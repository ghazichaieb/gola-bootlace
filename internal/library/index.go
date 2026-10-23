// The library's index: for every ISO and image, its SHA-256, where it came from and when it was added, kept in <library>/.bootlace/index.json. It is how a file that is
// added twice is noticed (same name, same link, same size, same content) without reading the files again. Files that were copied in by hand are hashed in the
// background, one at a time, and not while PCs are using the server.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record is what is known about one file.
type Record struct {
	Size   int64     `json:"size"`
	Mod    int64     `json:"mod"` // modification time, Unix nanoseconds: a file that changed is hashed again
	SHA256 string    `json:"sha256,omitempty"`
	Source string    `json:"source,omitempty"` // the link it was imported from
	Added  time.Time `json:"added,omitempty"`
}

// Index is the list of records of one library.
type Index struct {
	root string
	id   string

	mu   sync.Mutex
	recs map[string]Record // by "/" path

	// Idle, when set, says whether the server is quiet enough for the hasher to read files (nothing is installing).
	Idle func() bool

	status HashStatus
}

// HashStatus is how far the background hashing is.
type HashStatus struct {
	Running bool   `json:"running"`
	File    string `json:"file,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Waiting int    `json:"waiting"` // files still to do
	Paused  bool   `json:"paused"`  // waiting for the PCs to finish
}

type indexFile struct {
	Version int               `json:"version"`
	Files   map[string]Record `json:"files"`
}

// OpenIndex reads the index of the library (none yet is an empty one).
func OpenIndex(l *Library) *Index {
	x := &Index{root: l.Path(), id: l.ID(), recs: map[string]Record{}}
	if data, err := os.ReadFile(filepath.Join(x.root, MetaDir, "index.json")); err == nil {
		var f indexFile
		if json.Unmarshal(data, &f) == nil && f.Files != nil {
			x.recs = f.Files
		}
	}
	return x
}

// Root is the library folder this index belongs to.
func (x *Index) Root() string { return x.root }

// Get is the record of a file, when it is still true: the file has the size and time it had when the record was made.
func (x *Index) Get(rel string) (Record, bool) {
	x.mu.Lock()
	r, ok := x.recs[rel]
	x.mu.Unlock()
	if !ok {
		return Record{}, false
	}
	st, err := os.Stat(filepath.Join(x.root, filepath.FromSlash(rel)))
	if err != nil || st.Size() != r.Size || st.ModTime().UnixNano() != r.Mod {
		return Record{}, false
	}
	return r, true
}

// Set keeps a record. The size and time are taken from the file as it is now.
func (x *Index) Set(rel string, r Record) error {
	st, err := os.Stat(filepath.Join(x.root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	r.Size, r.Mod = st.Size(), st.ModTime().UnixNano()
	x.mu.Lock()
	x.recs[rel] = r
	x.mu.Unlock()
	return x.save()
}

// Move follows a rename: one file, or everything under a renamed folder.
func (x *Index) Move(from, to string) {
	x.mu.Lock()
	for p, r := range x.recs {
		if p == from || strings.HasPrefix(p, from+"/") {
			delete(x.recs, p)
			x.recs[to+strings.TrimPrefix(p, from)] = r
		}
	}
	x.mu.Unlock()
	_ = x.save()
}

// Forget drops the record of a file that was deleted.
func (x *Index) Forget(rel string) {
	x.mu.Lock()
	delete(x.recs, rel)
	x.mu.Unlock()
	_ = x.save()
}

// Match is a file that looks like one that is being added.
type Match struct {
	Path   string `json:"path"`
	Reason string `json:"reason"` // "name", "source", "size", "content"
}

// Duplicates finds the files that a new one would repeat: the same name, the same link, the same size under the same file name, or the same content. Any of the
// arguments can be empty or zero when it is not known. Only current records count for the content and the link.
func (x *Index) Duplicates(destRel, source string, size int64, sha string) []Match {
	out := []Match{} // (never nil: the web page reads it as a list)
	seen := map[string]bool{}
	add := func(p, why string) {
		if !seen[p+why] {
			seen[p+why] = true
			out = append(out, Match{p, why})
		}
	}
	if destRel != "" {
		if _, err := os.Lstat(filepath.Join(x.root, filepath.FromSlash(destRel))); err == nil {
			add(destRel, "name")
		}
	}
	x.mu.Lock()
	paths := make([]string, 0, len(x.recs))
	for p := range x.recs {
		paths = append(paths, p)
	}
	x.mu.Unlock()
	sort.Strings(paths)
	base := filepath.Base(destRel)
	for _, p := range paths {
		r, ok := x.Get(p)
		if !ok {
			continue
		}
		switch {
		case sha != "" && r.SHA256 == sha:
			add(p, "content")
		case source != "" && r.Source == source:
			add(p, "source")
		case size > 0 && r.Size == size && destRel != "" && strings.EqualFold(filepath.Base(p), base):
			add(p, "size")
		}
	}
	return out
}

func (x *Index) save() error {
	x.mu.Lock()
	data, err := json.Marshal(indexFile{Version: 1, Files: x.recs})
	x.mu.Unlock()
	if err != nil {
		return err
	}
	dir := filepath.Join(x.root, MetaDir)
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		return errors.New("the library is not available") // (never writes into a folder that is not the library)
	}
	tmp, err := os.CreateTemp(dir, "index-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "index.json"))
}

// ---- hashing

// HashFile is the SHA-256 of a file, read with the given pace check.
func HashFile(path string, pause func()) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if pause != nil {
			pause()
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Status is how far the hashing is.
func (x *Index) Status() HashStatus {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.status
}

func (x *Index) setStatus(f func(*HashStatus)) {
	x.mu.Lock()
	f(&x.status)
	x.mu.Unlock()
}

// stableFor is how long a file must have been left alone before it is read: one that is still being copied in is not hashed half way.
const stableFor = 15 * time.Second

// pending lists the files that need a record: managed files that have none, or whose record no longer fits.
func (x *Index) pending() []string {
	var out []string
	_ = filepath.WalkDir(x.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(x.root, p)
		if rel != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !Managed(d.Name()) {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, ok := x.Get(rel); ok {
			return nil
		}
		if info, err := d.Info(); err == nil && time.Since(info.ModTime()) >= stableFor && info.Mode().IsRegular() {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// HashPending hashes the files that have no record yet, one at a time, and returns when there are none left or stop is closed. It waits while Idle says that PCs
// are using the server.
func (x *Index) HashPending(stop <-chan struct{}) {
	for {
		todo := x.pending()
		x.setStatus(func(s *HashStatus) { s.Waiting = len(todo) })
		if len(todo) == 0 {
			x.setStatus(func(s *HashStatus) { *s = HashStatus{} })
			return
		}
		rel := todo[0]
		abs := filepath.Join(x.root, filepath.FromSlash(rel))
		st, err := os.Stat(abs)
		if err != nil {
			continue
		}
		x.setStatus(func(s *HashStatus) { s.Running, s.File, s.Done, s.Total = true, rel, 0, st.Size() })
		var done int64
		pause := func() {
			for x.Idle != nil && !x.Idle() {
				x.setStatus(func(s *HashStatus) { s.Paused = true })
				select {
				case <-stop:
					return
				case <-time.After(2 * time.Second):
				}
			}
			x.setStatus(func(s *HashStatus) { s.Paused = false; s.Done = done })
			done += 1 << 20
		}
		sum, err := HashFile(abs, pause)
		select {
		case <-stop:
			return
		default:
		}
		if err != nil {
			if _, serr := os.Stat(abs); serr == nil {
				_ = x.Set(rel, Record{Added: time.Now()}) // unreadable: noted without a hash, so that it is not tried again and again
			} else {
				x.Forget(rel)
			}
			continue
		}
		if st2, err := os.Stat(abs); err == nil && st2.Size() == st.Size() && st2.ModTime().Equal(st.ModTime()) {
			x.mu.Lock()
			prev := x.recs[rel]
			x.mu.Unlock()
			prev.SHA256 = sum
			if prev.Added.IsZero() {
				prev.Added = time.Now()
			}
			_ = x.Set(rel, prev)
		}
	}
}

// Indexer keeps the index of whichever library is current, and does the hashing in the background.
type Indexer struct {
	Lib *Library
	// Idle says whether the server is quiet enough for files to be read (nothing is installing).
	Idle func() bool

	mu  sync.Mutex
	cur *Index
	id  string
}

// Get is the index of the current library, or nil when the library is not available.
func (x *Indexer) Get() *Index {
	if x.Lib.Check() != nil {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if key := x.Lib.Path() + "|" + x.Lib.ID(); x.cur == nil || x.id != key {
		x.cur, x.id = OpenIndex(x.Lib), key
		x.cur.Idle = x.Idle
	}
	return x.cur
}

// Run hashes the files that have no record, now and every little while, until stop is closed.
func (x *Indexer) Run(stop <-chan struct{}) {
	for {
		if idx := x.Get(); idx != nil {
			idx.HashPending(stop)
		}
		select {
		case <-stop:
			return
		case <-time.After(20 * time.Second):
		}
	}
}
