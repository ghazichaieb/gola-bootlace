// Package diskimg prepares disk images for being shown to a PC as a hard disk. An image that is compressed (.img.gz, .img.xz, .img.zst, .img.bz2) cannot be read at any place,
// so it is unpacked once, in the background, into the cache folder; the menu shows how far that is until it is ready. An image that is not compressed is used where it is.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package diskimg

import (
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Progress int    // percent of the packed file read so far, while unpacking
	Err      error  // why unpacking failed
}

// Manager unpacks images into Dir, one at a time per image, and remembers how far each is.
type Manager struct {
	Dir  string
	Logf func(format string, args ...any)

	mu   sync.Mutex
	jobs map[string]*job
}

type job struct {
	read, total atomic.Int64
	err         error
	done        bool
}

// target is the cache file of an image: named by the image's id and by the file's size and time, so that a replaced file is unpacked again.
func (m *Manager) target(iso isos.ISO) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", iso.Path, iso.Size, iso.ModTime.UnixNano())))
	return filepath.Join(m.Dir, fmt.Sprintf("disk-%s-%s.img", iso.ID, hex.EncodeToString(sum[:])[:10]))
}

// Status says whether the image can be read now and starts unpacking it when it must be and is not yet.
func (m *Manager) Status(iso isos.ISO) State {
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
	}
	j := m.jobs[dst]
	if j == nil {
		j = &job{}
		j.total.Store(iso.Size)
		m.jobs[dst] = j
		go m.unpack(iso, dst, j)
	}
	if j.err != nil {
		return State{Err: j.err}
	}
	pct := 0
	if t := j.total.Load(); t > 0 {
		pct = int(j.read.Load() * 100 / t)
	}
	if pct > 99 {
		pct = 99
	}
	return State{Progress: pct}
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
	if _, err := io.Copy(out, r); err != nil {
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

// removeOld deletes the unpacked copies of earlier versions of the same image.
func (m *Manager) removeOld(iso isos.ISO, keep string) {
	old, _ := filepath.Glob(filepath.Join(m.Dir, fmt.Sprintf("disk-%s-*.img", iso.ID)))
	for _, f := range old {
		if f != keep {
			os.Remove(f)
		}
	}
}
