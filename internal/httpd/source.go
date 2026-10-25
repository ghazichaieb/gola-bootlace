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

// openRaw opens an ISO of the list (part 0), or one of the partitions in it.
func (s *Server) openRaw(iso isos.ISO, part int) (source, error) {
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
	c := &family.Context{FS: src.FS, URLs: s.familyURLsFor(src.ISO, spec), Findings: &s.findings, Stamp: stamp(src.ISO)}
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

// readable is the file to look into for an ISO of the list: itself, or for a disk image the unpacked copy (false while that is not there).
func (s *Server) readable(iso isos.ISO) (path string, size int64, ok bool) {
	if !iso.Disk {
		return iso.Path, iso.Size, iso.Packed == ""
	}
	st := s.diskState(iso)
	return st.Path, st.Size, st.Ready
}

// contexts are what a file is looked at as: the ISO itself, or each readable partition of a disk image. The returned function closes the file.
func (s *Server) contexts(iso isos.ISO) ([]*family.Context, func()) {
	if !iso.Disk {
		src, err := s.open(iso, 0)
		if err != nil || src.FS == nil {
			if err == nil {
				src.Close()
			}
			return nil, func() {}
		}
		return []*family.Context{s.context(src, iso.ID)}, src.Close
	}
	path, size, ok := s.readable(iso)
	if !ok {
		return nil, func() {}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}
	}
	parts, err := disc.OpenDisk(f, size)
	if err != nil {
		f.Close()
		return nil, func() {}
	}
	var out []*family.Context
	for _, p := range parts {
		spec := specOf(iso, p.Number)
		out = append(out, &family.Context{FS: p.FS, URLs: s.familyURLsFor(iso, spec), Findings: &s.findings, Stamp: stamp(iso) + "|" + spec})
	}
	return out, func() { f.Close() }
}

// stamp names this version of an ISO file.
func stamp(iso isos.ISO) string {
	return fmt.Sprintf("%s|%d|%d", iso.Path, iso.Size, iso.ModTime.UnixNano())
}

// recognise looks inside an ISO or disk image (once) and says whether it is a system this server knows how to start over the network: first by the names of its files,
// which is quick, and then, in the background, by its boot menu and what its start-up disk is made of (see recogniseGeneric).
func (s *Server) recognise(iso isos.ISO) (family.Boot, bool) {
	path, size, ok := s.readable(iso)
	if !ok {
		return family.Boot{}, false
	}
	if !s.byNameSkipped(iso) {
		if b, ok := s.remember("named", path, size, iso.ModTime, func() (family.Boot, bool) { return s.detect(iso, family.DetectIn) }); ok {
			return b, true
		}
	}
	return s.recogniseGeneric(iso)
}

// detect runs a way of recognising over what the file holds, until one of them knows it.
func (s *Server) detect(iso isos.ISO, by func(*family.Context) (family.Boot, bool)) (family.Boot, bool) {
	cs, done := s.contexts(iso)
	defer done()
	for _, c := range cs {
		if b, ok := by(c); ok {
			return b, true
		}
	}
	return family.Boot{}, false
}

// byNameSkipped is a test aid: BOOTLACE_GENERIC_ONLY lists names (the start of an ISO's file name, separated by commas) whose recognition by name is skipped, so that the
// generic way is what starts them.
func (s *Server) byNameSkipped(iso isos.ISO) bool {
	for _, p := range strings.Split(os.Getenv("BOOTLACE_GENERIC_ONLY"), ",") {
		if p != "" && strings.HasPrefix(strings.ToLower(iso.Name), strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// recogniseGeneric is the answer for an ISO that no family is known by: from its boot menu and its start-up disk, which takes seconds. Until the answer is there it is "not
// recognised", and the look is made in the background (one at a time) so that the menu of a PC is not held up; the next menu has the answer.
func (s *Server) recogniseGeneric(iso isos.ISO) (family.Boot, bool) {
	key := "generic|" + stamp(iso)
	s.mu.Lock()
	if r, ok := s.known[key]; ok {
		s.mu.Unlock()
		return r.boot, r.ok
	}
	if s.looking[key] {
		s.mu.Unlock()
		return family.Boot{}, false
	}
	if s.looking == nil {
		s.looking = map[string]bool{}
	}
	s.looking[key] = true
	s.mu.Unlock()
	go func() {
		s.lookMu.Lock()
		defer s.lookMu.Unlock()
		var r recognised
		r.boot, r.ok = s.detect(iso, family.DetectGeneric)
		s.mu.Lock()
		if s.known == nil {
			s.known = map[string]recognised{}
		}
		s.known[key] = r
		delete(s.looking, key)
		s.mu.Unlock()
		if r.ok {
			s.logf("%s: not known by name, but it is made like one that is: %s", iso.Title, r.boot.Family)
		}
	}()
	return family.Boot{}, false
}

// waysOf are the other entries of an ISO's own boot menu that it can be started with, next to the usual start `primary` (issue #35). Like the generic recognition this looks into
// start-up disks, which takes seconds: the first call starts the look in the background and answers none, and the menu that follows has the answer.
func (s *Server) waysOf(iso isos.ISO, primary family.Boot) []family.Way {
	if iso.Disk || iso.Packed != "" {
		return nil
	}
	key := "ways|" + stamp(iso)
	s.mu.Lock()
	if w, ok := s.waysKnown[key]; ok {
		s.mu.Unlock()
		return w
	}
	if s.looking[key] {
		s.mu.Unlock()
		return nil
	}
	if s.looking == nil {
		s.looking = map[string]bool{}
	}
	s.looking[key] = true
	s.mu.Unlock()
	go func() {
		s.lookMu.Lock()
		defer s.lookMu.Unlock()
		var found []family.Way
		cs, done := s.contexts(iso)
		for _, c := range cs {
			if found = family.Ways(c, primary); len(found) > 0 {
				break
			}
		}
		done()
		s.mu.Lock()
		if s.waysKnown == nil {
			s.waysKnown = map[string][]family.Way{}
		}
		s.waysKnown[key] = found
		delete(s.looking, key)
		s.mu.Unlock()
		if len(found) > 0 {
			s.logf("%s: its own boot menu has %d other way(s) to start it", iso.Title, len(found))
		}
	}()
	return nil
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
