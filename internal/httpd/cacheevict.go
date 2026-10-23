// Making room in the cache: when a place is full, the entries that were used least recently and that nothing is using are removed, oldest first, instead of failing.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// recentlyUsed is how long an entry is left alone after it was last used: a PC may be about to read it (a copy of boot.wim is read by the boot manager some time after the
// menu asked for it).
const recentlyUsed = 30 * time.Minute

// cacheFile matches the names the server gives its cache entries: boot-<id>-<hash>.wim and disk-<id>-<hash>.img or .iso (the id is 10 hex digits).
var cacheFile = regexp.MustCompile(`^(?:boot|disk)-([0-9a-f]{10})-[0-9a-f]+\.(?:wim|img|iso)$`)

// CacheEntry is one entry of the cache as the web interface lists it.
type CacheEntry struct {
	Name     string    `json:"name"`
	Where    string    `json:"where"` // "container" or "library"
	Bytes    int64     `json:"bytes"`
	LastUsed time.Time `json:"lastUsed"`
	InUse    bool      `json:"inUse"` // a PC is using the ISO it belongs to, or it was used a moment ago: it is not removed
	Title    string    `json:"title"` // the ISO it belongs to, when that is known
}

type cacheItem struct {
	path string
	CacheEntry
	id string
}

// idInUse says whether a PC is reading the ISO with this id.
func (s *Server) idInUse(id string) bool {
	s.useMu.Lock()
	defer s.useMu.Unlock()
	for name := range s.using {
		if isos.IDOf(name) == id {
			return true
		}
	}
	return false
}

// cacheItems lists the entries of one cache folder (and of its images folder), the least recently used first.
func (s *Server) cacheItems(dir, where string) []cacheItem {
	var out []cacheItem
	for _, d := range []string{dir, filepath.Join(dir, "images")} {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			m := cacheFile.FindStringSubmatch(e.Name())
			if e.IsDir() || m == nil {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			it := cacheItem{path: filepath.Join(d, e.Name()), id: m[1], CacheEntry: CacheEntry{Name: e.Name(), Where: where, Bytes: info.Size(), LastUsed: info.ModTime()}}
			it.InUse = s.idInUse(m[1]) || time.Since(info.ModTime()) < recentlyUsed
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.Before(out[j].LastUsed) })
	return out
}

// touchCache notes that an entry was just used (its modification time is the last use).
func touchCache(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// evict removes entries of the folder, oldest first, until done() says there is room. It never removes an entry that is in use. It says whether there is room now.
func (s *Server) evict(dir, where string, done func() bool) bool {
	if done() {
		return true
	}
	for _, it := range s.cacheItems(dir, where) {
		if it.InUse {
			continue
		}
		if os.Remove(it.path) == nil {
			s.logf("cache: removed %s (%s, last used %s) to make room", it.Name, library.Size(uint64(it.Bytes)), it.LastUsed.Local().Format("2006-01-02 15:04"))
			if done() {
				return true
			}
		}
	}
	return done()
}

// libraryRoom is how many bytes fit in the library's cache folder while the library's reserve is left alone (a very large number when the disk cannot say).
func libraryRoom(dir string) int64 {
	free, total, err := library.DiskSpace(dir)
	if err != nil {
		return 1 << 62
	}
	if r := library.Reserve(total); free > r {
		return int64(free - r)
	}
	return 0
}

// noteFull remembers that the cache could not make room, for the web page.
func (s *Server) noteFull(msg string) {
	s.cacheFullMu.Lock()
	s.cacheFull, s.cacheFullAt = msg, time.Now()
	s.cacheFullMu.Unlock()
}

// cacheFullNote is the last "could not make room" message, while it is recent.
func (s *Server) cacheFullNote() string {
	s.cacheFullMu.Lock()
	defer s.cacheFullMu.Unlock()
	if time.Since(s.cacheFullAt) > time.Hour {
		return ""
	}
	return s.cacheFull
}
