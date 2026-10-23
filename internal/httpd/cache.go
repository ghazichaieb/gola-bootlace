// The cache: what the server makes once and keeps (the unpacked copies of packed disk images, the copies of Windows' boot.wim with Bootlace's files in them). It has
// two places. The container's own folder is small and fast and is used while it has room; when it is full, new entries go to <library>/.bootlace/cache, so that the
// container's disk can stay small. An entry is looked for in both places. How much the first place may hold is a setting, and the web interface says that it
// bounds how many different installs can be prepared at once.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// DefaultLocalCache is how much the container's own cache folder may hold when nothing else is set.
const DefaultLocalCache = 2 << 30

// localCache is the container's own cache folder.
func (s *Server) localCache() string {
	if s.CacheDir != "" {
		return s.CacheDir
	}
	return filepath.Join(os.TempDir(), "bootlace-cache")
}

// libraryCache is the cache folder inside the library, or "" when the library is not available (nothing is written to a folder that is not the library). The
// answer is kept for a couple of seconds: it is asked for on every menu, and looking at a network drive is not free.
func (s *Server) libraryCache() string {
	if s.Library == nil {
		return ""
	}
	s.libCacheMu.Lock()
	defer s.libCacheMu.Unlock()
	if time.Since(s.libCacheAt) < 2*time.Second && s.libCachePath == s.Library.Path() {
		return s.libCacheDir
	}
	dir := ""
	if s.Library.Check() == nil {
		dir = filepath.Join(s.Library.Path(), library.MetaDir, "cache")
	}
	s.libCacheDir, s.libCachePath, s.libCacheAt = dir, s.Library.Path(), time.Now()
	return dir
}

// cacheDirs are the places an entry may be in.
func (s *Server) cacheDirs() []string {
	dirs := []string{s.localCache()}
	if d := s.libraryCache(); d != "" {
		dirs = append(dirs, d)
	}
	return dirs
}

// CacheDirs are the cache folders: the container's own and, while the library is available, the one inside it.
func (s *Server) CacheDirs() []string { return s.cacheDirs() }

// cacheSettings are where new entries go ("auto", "local" or "library") and how much the container's own folder may hold.
func (s *Server) cacheSettings() (where string, limit int64) {
	where, limit = "auto", DefaultLocalCache
	if s.CacheSettings != nil {
		w, l := s.CacheSettings()
		if w != "" {
			where = w
		}
		if l > 0 {
			limit = l
		}
	}
	return where, limit
}

// cachePlace is the folder a new entry of about this many bytes goes to.
func (s *Server) cachePlace(need int64) string {
	local, lib := s.localCache(), s.libraryCache()
	where, limit := s.cacheSettings()
	switch {
	case lib == "" || where == "local":
		return local
	case where == "library":
		return lib
	case dirSize(local)+need <= limit:
		return local
	}
	return lib
}

// cacheFind looks for a cache file by name in both places.
func (s *Server) cacheFind(name string) (string, bool) {
	for _, d := range s.cacheDirs() {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// CacheUsage is what the web interface shows.
type CacheUsage struct {
	Where        string `json:"where"`
	LocalLimit   int64  `json:"localLimit"`
	LocalDir     string `json:"localDir"`
	LocalBytes   int64  `json:"localBytes"`
	LibraryDir   string `json:"libraryDir,omitempty"`
	LibraryBytes int64  `json:"libraryBytes"`
}

// CacheUsage tells where the cache is and how much each place holds.
func (s *Server) CacheUsage() CacheUsage {
	where, limit := s.cacheSettings()
	u := CacheUsage{Where: where, LocalLimit: limit, LocalDir: s.localCache()}
	u.LocalBytes = dirSize(u.LocalDir)
	if d := s.libraryCache(); d != "" {
		u.LibraryDir, u.LibraryBytes = d, dirSize(d)
	}
	return u
}

// dirSize is the total size of the files under a folder (0 when it is not there).
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
