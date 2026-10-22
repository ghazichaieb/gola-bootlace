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

	mu   sync.Mutex
	jobs map[string]*job
	turn chan struct{} // one unpacking at a time
}

type job struct {
	read, total atomic.Int64
	err         error
	done        bool
}

// target is the cache file of an image: named by the image's id and by the file's size and time, so that a replaced file is unpacked again.
func (m *Manager) target(iso isos.ISO) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", iso.Path, iso.Size, iso.ModTime.UnixNano())))
	ext := ".img"
	if !iso.Disk {
		ext = ".iso"
	}
	return filepath.Join(m.Dir, fmt.Sprintf("disk-%s-%s%s", iso.ID, hex.EncodeToString(sum[:])[:10], ext))
}

// Status says whether the image can be read now and how far unpacking is; it never starts it.
func (m *Manager) Status(iso isos.ISO) State { return m.status(iso, false) }

// Start is Status, and starts unpacking the image when it must be unpacked and is not yet (it waits for its turn behind the images that are being unpacked).
func (m *Manager) Start(iso isos.ISO) State { return m.status(iso, true) }

func (m *Manager) status(iso isos.ISO, start bool) State {
	if iso.Packed == "" {
		return State{Ready: true, Path: iso.Path, Size: iso.Size}
	}
	dst := m.target(iso)
	if st, err := os.Stat(dst); err == nil {
		return State{Ready: true, Path: dst, Size: st.Size()}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobs == nil {
		m.jobs = map[string]*job{}
		m.turn = make(chan struct{}, 1)
	}
	j := m.jobs[dst]
	if j == nil && start {
		j = &job{}
		j.total.Store(iso.Size)
		m.jobs[dst] = j
		go m.unpack(iso, dst, j)
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
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
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
	if err := copySynced(out, r); err != nil {
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
func copySynced(out *os.File, r io.Reader) error {
	buf := make([]byte, 1<<20)
	var since int
	for {
		n, err := r.Read(buf)
		if n > 0 {
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
	old, _ := filepath.Glob(filepath.Join(m.Dir, fmt.Sprintf("disk-%s-*", iso.ID)))
	for _, f := range old {
		if f != keep && !strings.HasSuffix(f, ".part") {
			os.Remove(f)
		}
	}
}
