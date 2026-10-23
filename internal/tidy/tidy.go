// Package tidy removes what the server left behind: partial files in the library's incoming folder that no job owns, cache entries (unpacked images, copies of boot.wim)
// of ISOs that are not in the library any more, and index records of files that are gone. It runs at start and once a day, only inside the library's own .bootlace
// folder and the cache folders, and never touches an ISO or any other file; what it removed is written to the log.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package tidy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// PartAge is how long a partial file must have been left alone before it is removed: an upload or a download that is going on keeps writing to its file.
const PartAge = 24 * time.Hour

// Tidier does the clean-up; its fields are what it needs to know about the server.
type Tidier struct {
	Library *library.Library
	Index   func() *library.Index           // the library's index, nil when the library is not available
	ISOIDs  func() (map[string]bool, error) // the ids of every ISO and image in the library, hidden ones included
	Caches  func() []string                 // the cache folders (the container's own and the library's)
	Owned   func() map[string]bool          // names of partial files that a job still owns (it can resume them)
	Logf    func(format string, args ...any)
	Now     func() time.Time // (a test sets it)
}

// Report says what a run removed.
type Report struct {
	Parts, Cache int
	Index        int
	Bytes        int64
}

// cacheName matches the names the server gives its cache entries: boot-<id>-<hash>.wim and disk-<id>-<hash>.img / .iso (the id is 10 hex digits).
var cacheName = regexp.MustCompile(`^(?:boot|disk)-([0-9a-f]{10})-[0-9a-f]+\.(?:wim|img|iso)$`)

func (t *Tidier) logf(format string, args ...any) {
	if t.Logf != nil {
		t.Logf(format, args...)
	}
}

func (t *Tidier) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Run does one clean-up. Nothing is done while the library is not available (a network folder that is not mounted looks empty, and an empty library would make
// every cache entry look left over).
func (t *Tidier) Run() Report {
	var r Report
	if t.Library == nil || t.Library.Check() != nil {
		return r
	}
	t.parts(&r)
	t.cache(&r)
	if t.Index != nil {
		if idx := t.Index(); idx != nil {
			if n := idx.Prune(); n > 0 {
				r.Index = n
				t.logf("clean-up: %d record(s) of the checksum index for files that are gone removed", n)
			}
		}
	}
	return r
}

func (t *Tidier) parts(r *Report) {
	dir := filepath.Join(t.Library.Path(), library.MetaDir, "incoming")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var owned map[string]bool
	if t.Owned != nil {
		owned = t.Owned()
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".part") || owned[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || t.now().Sub(info.ModTime()) < PartAge {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			r.Parts++
			r.Bytes += info.Size()
			t.logf("clean-up: partial file %s (%s old) removed", e.Name(), t.now().Sub(info.ModTime()).Round(time.Hour))
		}
	}
}

func (t *Tidier) cache(r *Report) {
	if t.Caches == nil || t.ISOIDs == nil {
		return
	}
	ids, err := t.ISOIDs()
	if err != nil {
		return
	}
	for _, base := range t.Caches() {
		for _, dir := range []string{base, filepath.Join(base, "images")} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				m := cacheName.FindStringSubmatch(e.Name())
				if e.IsDir() || m == nil || ids[m[1]] {
					continue
				}
				info, err := e.Info()
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				if os.Remove(filepath.Join(dir, e.Name())) == nil {
					r.Cache++
					r.Bytes += info.Size()
					t.logf("clean-up: cache entry %s removed (its ISO is not in the library any more)", filepath.Join(dir, e.Name()))
				}
			}
		}
	}
}

// Loop runs a clean-up a minute after the start (the library is chosen and indexed by then) and every day, until stop is closed.
func (t *Tidier) Loop(stop <-chan struct{}) {
	wait := time.Minute
	for {
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
		if r := t.Run(); r.Parts+r.Cache+r.Index > 0 {
			t.logf("clean-up done: %d partial file(s), %d cache entrie(s), %d index record(s); %d MB freed", r.Parts, r.Cache, r.Index, r.Bytes>>20)
		}
		wait = 24 * time.Hour
	}
}
