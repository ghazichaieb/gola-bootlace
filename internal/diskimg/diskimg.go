// Package diskimg prepares packed files for being offered: a disk image or an ISO that is compressed (.img.gz, .iso.bz2 ...), or that is inside a zip archive. Such a file cannot be read at any place,
// so it is unpacked once into the cache folder, in the background and one image at a time (unpacking is heavy on the disk and the memory of a small server). That starts when
// someone starts the image (Start), not when the list is read, because an image can be many times its packed size. The menu shows how far it is. An image that is not compressed
// is used where it is.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package diskimg

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// State is how far an image is.
type State struct {
	Ready    bool
	Path     string // the file that holds the raw disk, once ready
	Size     int64  // its size in bytes
	Started  bool   // unpacking has been asked for (and is waiting its turn or running)
	Progress int    // percent of the packed file read so far, while unpacking
	Err      error  // why unpacking failed
}

// Manager unpacks images into Dir, one at a time per image, and remembers how far each is.
type Manager struct {
	Dir  string
	Logf func(format string, args ...any)
	// Place, when set, says which folder a copy that is about to be made goes to (it is given a guess of the copy's size), and Also lists the other folders where
	// copies made earlier may be. Without them everything is in Dir. They let the cache spill over from a small disk to a bigger one.
	Place func(need int64) string
	Also  func() []string
	// Used, when set, is told about every use of a finished copy (the cache removes the least recently used ones when it is full).
	Used func(path string)

	mu        sync.Mutex
	jobs      map[string]*job                      // by the name of the cache file
	turn      chan struct{}                        // one unpacking at a time
	diskSpace func(string) (uint64, uint64, error) // nil uses the destination disk; tests simulate low space
}

type job struct {
	read, total atomic.Int64
	err         error
	done        bool
	dst         string // where the copy is being made
}

// fileName is the name of the cache file of an image: from the image's id and the file's size and time, so that a replaced file is unpacked again.
func fileName(iso isos.ISO) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", iso.Path, iso.Size, iso.ModTime.UnixNano())))
	ext := ".img"
	if !iso.Disk {
		ext = ".iso"
	}
	return fmt.Sprintf("disk-%s-%s%s", iso.ID, hex.EncodeToString(sum[:])[:10], ext)
}

// dirs are all the folders a copy may be in.
func (m *Manager) dirs() []string {
	d := []string{m.Dir}
	if m.Also != nil {
		d = append(d, m.Also()...)
	}
	return d
}

// existing finds a finished copy of the image, in whichever folder it was put.
func (m *Manager) existing(iso isos.ISO) (path string, size int64, ok bool) {
	name := fileName(iso)
	for _, dir := range m.dirs() {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil {
			if m.Used != nil {
				m.Used(p)
			}
			return p, st.Size(), true
		}
	}
	return "", 0, false
}

// target is where a new copy of the image goes. An unpacked image is not smaller than its packed file, and is often a few times bigger.
func (m *Manager) target(iso isos.ISO) string {
	dir := m.Dir
	if m.Place != nil {
		dir = m.Place(iso.Size * 3)
	}
	return filepath.Join(dir, fileName(iso))
}

// Status says whether the image can be read now and how far unpacking is; it never starts it.
func (m *Manager) Status(iso isos.ISO) State { return m.status(iso, false) }

// Start is Status, and starts unpacking the image when it must be unpacked and is not yet (it waits for its turn behind the images that are being unpacked).
func (m *Manager) Start(iso isos.ISO) State { return m.status(iso, true) }

func (m *Manager) status(iso isos.ISO, start bool) State {
	if iso.Packed == "" {
		return State{Ready: true, Path: iso.Path, Size: iso.Size}
	}
	if p, size, ok := m.existing(iso); ok {
		return State{Ready: true, Path: p, Size: size}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobs == nil {
		m.jobs = map[string]*job{}
		m.turn = make(chan struct{}, 1)
	}
	name := fileName(iso)
	j := m.jobs[name]
	if j == nil && start {
		j = &job{dst: m.target(iso)}
		j.total.Store(iso.Size)
		m.jobs[name] = j
		go m.unpack(iso, j.dst, j)
	}
	if j == nil {
		return State{}
	}
	if j.err != nil {
		return State{Err: j.err, Started: true}
	}
	pct := 0
	if t := j.total.Load(); t > 0 {
		pct = int(j.read.Load() * 100 / t)
	}
	if pct > 99 {
		pct = 99
	}
	return State{Started: true, Progress: pct}
}

type counter struct {
	r io.Reader
	n *atomic.Int64
}

func (c counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func (m *Manager) unpack(iso isos.ISO, dst string, j *job) {
	fail := func(err error) {
		m.mu.Lock()
		j.err, j.done = err, true
		m.mu.Unlock()
		if m.Logf != nil {
			m.Logf("disk image %s: cannot unpack: %v", iso.Name, err)
		}
	}
	m.turn <- struct{}{} // wait for the image that is being unpacked
	defer func() { <-m.turn }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		fail(err)
		return
	}
	src, err := os.Open(iso.Path)
	if err != nil {
		fail(err)
		return
	}
	defer src.Close()
	cr := counter{src, &j.read}
	var r io.Reader
	switch iso.Packed {
	case "zip":
		zr, err := zip.OpenReader(iso.Path)
		if err != nil {
			fail(err)
			return
		}
		defer zr.Close()
		var inner *zip.File
		for _, f := range zr.File {
			if f.Name == iso.Inner {
				inner = f
			}
		}
		if inner == nil {
			fail(fmt.Errorf("%s is not in the archive any more", iso.Inner))
			return
		}
		rc, err := inner.Open()
		if err != nil {
			fail(err)
			return
		}
		defer rc.Close()
		j.total.Store(int64(inner.UncompressedSize64)) // progress counts what is unpacked, not what is read of the archive
		r = counter{rc, &j.read}
	case "gz":
		zr, err := gzip.NewReader(cr)
		if err != nil {
			fail(err)
			return
		}
		r = zr
	case "xz":
		xr, err := xz.NewReader(cr)
		if err != nil {
			fail(err)
			return
		}
		r = xr
	case "zst":
		zr, err := zstd.NewReader(cr)
		if err != nil {
			fail(err)
			return
		}
		defer zr.Close()
		r = zr
	case "bz2":
		r = bzip2.NewReader(cr)
	default:
		fail(fmt.Errorf("unknown packing %q", iso.Packed))
		return
	}
	if m.Logf != nil {
		m.Logf("disk image %s: unpacking into the cache folder", iso.Name)
	}
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		fail(err)
		return
	}
	if err := copySynced(out, r, m.diskSpace); err != nil {
		out.Close()
		os.Remove(part)
		fail(err)
		return
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		fail(err)
		return
	}
	if err := os.Rename(part, dst); err != nil {
		fail(err)
		return
	}
	m.mu.Lock()
	j.done = true
	m.mu.Unlock()
	if m.Logf != nil {
		m.Logf("disk image %s: ready", iso.Name)
	}
	m.removeOld(iso, dst)
}

// copySynced copies and writes the data out to the disk every 32 MB, so that the pages waiting to be written do not pile up in the memory of a small server (a container with
// a memory limit counts them, and is killed for it).
func copySynced(out *os.File, r io.Reader, diskSpace func(string) (uint64, uint64, error)) error {
	if diskSpace == nil {
		diskSpace = library.DiskSpace
	}
	buf := make([]byte, 1<<20)
	var since int
	for {
		n, err := r.Read(buf)
		if n > 0 {
			// Packed sizes (and ZIP size fields) do not bound the output. Check the
			// actual destination before every write, including spillover caches.
			free, total, serr := diskSpace(filepath.Dir(out.Name()))
			if serr != nil {
				return fmt.Errorf("cannot check free space in the cache: %w", serr)
			}
			reserve := library.Reserve(total)
			if free < reserve || free-reserve < uint64(n) {
				return fmt.Errorf("not enough room to unpack into the cache: %s free, %s is kept in reserve", library.Size(free), library.Size(reserve))
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			since += n
			if since >= 32<<20 {
				if serr := out.Sync(); serr != nil {
					return serr
				}
				since = 0
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// removeOld deletes the unpacked copies of earlier versions of the same image.
func (m *Manager) removeOld(iso isos.ISO, keep string) {
	var old []string
	for _, dir := range m.dirs() {
		found, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("disk-%s-*", iso.ID)))
		old = append(old, found...)
	}
	for _, f := range old {
		if f != keep && !strings.HasSuffix(f, ".part") {
			os.Remove(f)
		}
	}
}
