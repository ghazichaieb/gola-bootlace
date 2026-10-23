// What the server reads an ISO from. A file of the ISO folder is an ISO, or a disk image whose partitions hold the files, and an ISO can have partitions too (the EFI image
// with the kernel, in ChimeraOS's). A partition is named "<id>@<number>" in the addresses of its files (/fs/), of the network block disk and of the extra archive (/x/); a
// plain ISO is just "<id>". This file opens them, and says what is inside to the package family.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// splitSpec takes "<id>@<number>" apart; a plain id has the number 0.
func splitSpec(spec string) (id string, part int) {
	id, n, ok := strings.Cut(spec, "@")
	if !ok {
		return spec, 0
	}
	part, err := strconv.Atoi(n)
	if err != nil || part < 1 {
		return spec, 0
	}
	return id, part
}

func specOf(iso isos.ISO, part int) string {
	if part == 0 {
		return iso.ID
	}
	return fmt.Sprintf("%s@%d", iso.ID, part)
}

var errNoSuchISO = errors.New("no such ISO")

// source is an ISO, or a partition of a file, opened for reading.
type source struct {
	ISO   isos.ISO
	R     io.ReaderAt // the bytes that make up the disk shown over the network
	Size  int64
	FS    disc.FS // the files inside; nil when the file system cannot be read
	Close func()
}

// openSource opens what a spec names.
func (s *Server) openSource(spec string) (source, error) {
	id, part := splitSpec(spec)
	list, err := s.scan()
	if err != nil {
		return source{}, err
	}
	for _, iso := range list {
		if iso.ID == id {
			return s.open(iso, part)
		}
	}
	return source{}, errNoSuchISO
}

// open opens an ISO of the list (part 0), or one of the partitions in it.
func (s *Server) open(iso isos.ISO, part int) (source, error) {
	if iso.Packed != "" {
		return source{}, errors.New("the file is not unpacked yet")
	}
	path, size := iso.Path, iso.Size
	if iso.Disk {
		if part == 0 {
			return source{}, errors.New("a disk image is not offered whole this way")
		}
		st := s.diskState(iso)
		if !st.Ready {
			return source{}, errors.New("the disk image is not unpacked yet")
		}
		path, size = st.Path, st.Size
	}
	f, err := os.Open(path)
	if err != nil {
		return source{}, err
	}
	src := source{ISO: iso, R: f, Size: size, Close: func() { f.Close() }}
	if part == 0 {
		if img, err := disc.Open(f, size); err == nil {
			src.FS = img
		}
		return src, nil
	}
	parts, err := disc.OpenDisk(f, size)
	if err != nil {
		f.Close()
		return source{}, err
	}
	for _, p := range parts {
		if p.Number == part {
			src.R, src.Size, src.FS = p.Section(f), p.Size, p.FS
			return src, nil
		}
	}
	f.Close()
	return source{}, fmt.Errorf("the file has no readable partition %d", part)
}

// context is what the package family looks at for a source: its files, where the server offers them, and, for a whole ISO, the files of its partitions (found when asked for).
func (s *Server) context(src source, spec string) *family.Context {
	c := &family.Context{FS: src.FS, URLs: s.familyURLsFor(src.ISO, spec)}
	if _, part := splitSpec(spec); part == 0 {
		var once sync.Once
		var vols []family.Volume
		c.Volumes = func() []family.Volume {
			once.Do(func() {
				parts, err := disc.OpenDisk(src.R, src.Size)
				if err != nil {
					return
				}
				for _, p := range parts {
					vols = append(vols, family.Volume{FS: p.FS, URL: s.Base + "/fs/" + specOf(src.ISO, p.Number)})
				}
			})
			return vols
		}
	}
	return c
}

// recognise looks inside the ISO (once) and says whether it is a system this server knows how to start over the network.
func (s *Server) recognise(iso isos.ISO) (family.Boot, bool) {
	if iso.Disk {
		return family.Boot{}, false
	}
	return s.remember("iso", iso.Path, iso.Size, iso.ModTime, func() (family.Boot, bool) {
		src, err := s.open(iso, 0)
		if err != nil {
			return family.Boot{}, false
		}
		defer src.Close()
		if src.FS == nil {
			return family.Boot{}, false
		}
		return family.DetectIn(s.context(src, iso.ID))
	})
}

// recogniseDisk looks at the partitions of a disk image (once) for a system this server knows how to start, and gives the way to start it.
func (s *Server) recogniseDisk(iso isos.ISO) (family.Boot, bool) {
	st := s.diskState(iso)
	if !st.Ready {
		return family.Boot{}, false
	}
	return s.remember("disk", st.Path, st.Size, iso.ModTime, func() (family.Boot, bool) {
		f, err := os.Open(st.Path)
		if err != nil {
			return family.Boot{}, false
		}
		defer f.Close()
		parts, err := disc.OpenDisk(f, st.Size)
		if err != nil {
			return family.Boot{}, false
		}
		for _, p := range parts {
			if b, ok := family.DetectIn(&family.Context{FS: p.FS, URLs: s.familyURLsFor(iso, specOf(iso, p.Number))}); ok {
				return b, true
			}
		}
		return family.Boot{}, false
	})
}

// remember runs a look inside a file once, and keeps the answer until the file changes (reading an ISO's folders takes a moment).
func (s *Server) remember(kind, path string, size int64, mod time.Time, look func() (family.Boot, bool)) (family.Boot, bool) {
	key := fmt.Sprintf("%s|%s|%d|%d", kind, path, size, mod.UnixNano())
	s.mu.Lock()
	if r, ok := s.known[key]; ok {
		s.mu.Unlock()
		return r.boot, r.ok
	}
	s.mu.Unlock()
	var r recognised
	r.boot, r.ok = look()
	s.mu.Lock()
	if s.known == nil {
		s.known = map[string]recognised{}
	}
	s.known[key] = r
	s.mu.Unlock()
	return r.boot, r.ok
}
