// A disk image that holds a Linux the server knows how to start (Tails, for one) is started from the files on its FAT partition, the way an ISO is: the kernel and
// initramfs are fetched from inside the image, and the partition is offered as a network block disk. Such a partition is named "<id>@<number>" in the addresses
// of the files (/fs/), of the network block disk and of the extra archive (/x/); a plain ISO is just "<id>".
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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

// source is an ISO, or a partition of a disk image, opened for reading.
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
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return source{}, err
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
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
		} else if part != 0 {
			return source{}, errors.New("an ISO has no partitions")
		}
		f, err := os.Open(path)
		if err != nil {
			return source{}, err
		}
		src := source{ISO: iso, R: f, Size: size, Close: func() { f.Close() }}
		if iso.Disk {
			parts, err := disc.OpenDisk(f, size)
			if err != nil {
				f.Close()
				return source{}, err
			}
			found := false
			for _, p := range parts {
				if p.Number == part {
					src.R, src.Size, src.FS, found = p.Section(f), p.Size, p.FS, true
				}
			}
			if !found {
				f.Close()
				return source{}, fmt.Errorf("the disk image has no readable partition %d", part)
			}
		} else if img, err := disc.Open(f, size); err == nil {
			src.FS = img
		}
		return src, nil
	}
	return source{}, errNoSuchISO
}

// recogniseDisk looks at the partitions of a disk image (once) for a Linux this server knows how to start, and gives the way to start it.
func (s *Server) recogniseDisk(iso isos.ISO) (family.Boot, bool) {
	st := s.diskState(iso)
	if !st.Ready {
		return family.Boot{}, false
	}
	key := fmt.Sprintf("disk|%s|%d|%d", st.Path, st.Size, iso.ModTime.UnixNano())
	s.mu.Lock()
	if r, ok := s.known[key]; ok {
		s.mu.Unlock()
		return r.boot, r.ok
	}
	s.mu.Unlock()
	var result recognised
	if f, err := os.Open(st.Path); err == nil {
		if parts, err := disc.OpenDisk(f, st.Size); err == nil {
			for _, p := range parts {
				if b, ok := family.Detect(p.FS, s.familyURLsFor(iso, specOf(iso, p.Number))); ok {
					result.boot, result.ok = b, true
					break
				}
			}
		}
		f.Close()
	}
	s.mu.Lock()
	if s.known == nil {
		s.known = map[string]recognised{}
	}
	s.known[key] = result
	s.mu.Unlock()
	return result.boot, result.ok
}
