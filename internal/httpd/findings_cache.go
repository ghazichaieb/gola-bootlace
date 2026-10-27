// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

const maxFindingsFile = (1 << 20) + (16 << 10)

type findingsFile struct {
	Format               int
	Program, Base, Stamp string
	Findings             json.RawMessage
}

func (s *Server) findingsPath(iso isos.ISO) string {
	if s.CacheDir == "" {
		return ""
	} // embedders can keep recognition entirely in memory
	sum := sha256.Sum256([]byte(iso.Path))
	return filepath.Join(s.CacheDir, "boot-findings", fmt.Sprintf("%x.json", sum[:16]))
}

// loadFindings reads at most one bounded local file per ISO version/address.
// Changed runtime addresses are a cache miss; generated commands are never loaded.
func (s *Server) loadFindings(iso isos.ISO) bool {
	s.findingsCacheMu.Lock()
	defer s.findingsCacheMu.Unlock()
	if s.findingsEpoch != 0 {
		return false
	} // a rescan never reloads files it tried to invalidate
	key := stamp(iso) + "|" + s.Base
	if hit, ok := s.findingsLoaded[key]; ok {
		return hit
	}
	if s.findingsLoaded == nil {
		s.findingsLoaded = map[string]bool{}
	}
	s.findingsLoaded[key] = false
	path := s.findingsPath(iso)
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFindingsFile+1))
	if err != nil || len(data) > maxFindingsFile {
		return false
	}
	var saved findingsFile
	if json.Unmarshal(data, &saved) != nil || saved.Format != 1 || saved.Program != version.Version || saved.Base != s.Base || saved.Stamp != stamp(iso) {
		return false
	}
	if s.findings.RestoreStamp(saved.Stamp, saved.Findings) != nil {
		return false
	}
	s.findingsLoaded[key] = true
	return true
}

func (s *Server) findingsGeneration() uint64 {
	s.findingsCacheMu.Lock()
	defer s.findingsCacheMu.Unlock()
	return s.findingsEpoch
}

func (s *Server) saveFindings(iso isos.ISO, epoch uint64) {
	s.findingsCacheMu.Lock()
	defer s.findingsCacheMu.Unlock()
	if epoch != s.findingsEpoch {
		return
	}
	path := s.findingsPath(iso)
	if path == "" {
		return
	}
	payload, err := s.findings.MarshalStamp(stamp(iso))
	if err != nil {
		return
	}
	data, err := json.Marshal(findingsFile{1, version.Version, s.Base, stamp(iso), payload})
	if err != nil || len(data) > maxFindingsFile {
		return
	}
	if err := writeFileAtomic(path, data); err != nil {
		s.logf("cannot save boot recognition cache: %v", err)
	}
}

func (s *Server) clearFindings() {
	s.findingsCacheMu.Lock()
	defer s.findingsCacheMu.Unlock()
	s.findingsEpoch++
	s.findingsLoaded = nil
	s.findings.Forget()
	if s.CacheDir == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(s.CacheDir, "boot-findings", "*.json"))
	for _, path := range files {
		_ = os.Remove(path)
	}
}

func (s *Server) cachedGeneric(iso isos.ISO) (family.Boot, bool) {
	if !s.loadFindings(iso) {
		return family.Boot{}, false
	}
	facts := s.findings.CachedOnly()
	contexts, done := s.contexts(iso)
	defer done()
	for _, c := range contexts {
		c.Findings = facts
		if b, ok := family.DetectGeneric(c); ok && !facts.Missing() {
			return b, true
		}
	}
	return family.Boot{}, false
}

func (s *Server) cachedWays(iso isos.ISO, primary family.Boot) ([]family.Way, bool) {
	if !s.loadFindings(iso) {
		return nil, false
	}
	facts := s.findings.CachedOnly()
	contexts, done := s.contexts(iso)
	defer done()
	var ways []family.Way
	for _, c := range contexts {
		c.Findings = facts
		if ways = family.Ways(c, primary); len(ways) > 0 {
			break
		}
	}
	return ways, !facts.Missing()
}

// publishRecognition keeps an older in-flight look from undoing a rescan.
// Lock order is cache state, then recognition maps, as for invalidation.
func (s *Server) publishRecognition(epoch uint64, publish func()) bool {
	s.findingsCacheMu.Lock()
	defer s.findingsCacheMu.Unlock()
	if epoch != s.findingsEpoch {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	publish()
	return true
}
