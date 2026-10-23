// Which files PCs are using: a file that a PC reads right now (as a network disk, over iSCSI or by a download) is marked, so that the web interface can warn
// before it is renamed or deleted.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"strings"
	"sync"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// hold marks an ISO (by its name in the library) as in use until the returned function is called.
func (s *Server) hold(name string) func() {
	s.useMu.Lock()
	if s.using == nil {
		s.using = map[string]int{}
	}
	s.using[name]++
	s.useMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.useMu.Lock()
			if s.using[name]--; s.using[name] <= 0 {
				delete(s.using, name)
			}
			s.useMu.Unlock()
		})
	}
}

// InUse says whether a PC is using the file at this path of the library, or any file under it (when it is a folder).
func (s *Server) InUse(path string) bool {
	s.useMu.Lock()
	defer s.useMu.Unlock()
	for name := range s.using {
		if name == path || strings.HasPrefix(name, strings.TrimSuffix(path, "/")+"/") {
			return true
		}
	}
	return false
}

// open opens an ISO of the list (part 0), or one of the partitions in it, and counts it as in use until it is closed.
func (s *Server) open(iso isos.ISO, part int) (source, error) {
	src, err := s.openRaw(iso, part)
	if err != nil {
		return src, err
	}
	release := s.hold(iso.Name)
	inner := src.Close
	src.Close = func() {
		if inner != nil {
			inner()
		}
		release()
	}
	return src, nil
}
