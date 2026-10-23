// Package library is the folder the ISOs and disk images live in. It is chosen in the web interface and must be readable and writable. Bootlace only ever writes
// to a folder that was added as a library: adding one puts a small marker in <folder>/.bootlace, and the marker has to be there, with the same id, for the library to
// count as available. A network mount that is not mounted is only an empty folder on the container's own disk; with no marker it is not the library, and nothing
// is written to it.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// MetaDir is the folder inside a library where Bootlace keeps what belongs to the library itself (the marker, files being received, the index).
const MetaDir = ".bootlace"

const (
	markerFile  = "library.json"
	probeFile   = "write-test"
	checkWithin = 5 * time.Second // a mount that does not answer in this time counts as not available
)

// Marker is the content of <library>/.bootlace/library.json.
type Marker struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Format  int       `json:"format"`
}

// Library is the library the server uses. It is safe to use from several goroutines.
type Library struct {
	mu   sync.RWMutex
	path string
	id   string

	checking sync.Mutex // guards call
	call     *lookup    // the look at the folder that is under way, if any
}

// New is a library with the saved path and id (both empty: none chosen yet).
func New(path, id string) *Library { return &Library{path: path, id: id} }

// Path is the folder, or "" when none is chosen.
func (l *Library) Path() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.path
}

// ID is the id in the marker the library was added with.
func (l *Library) ID() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.id
}

// Set changes the library to a folder that Prepare has made.
func (l *Library) Set(path, id string) {
	l.mu.Lock()
	l.path, l.id = path, id
	l.mu.Unlock()
}

// ErrNone is the answer of Check when no library is chosen.
var ErrNone = errors.New("no library is chosen yet")

// Check says whether the library can be used now: the folder is there, it has the marker it was added with, and it can be written to. The error is for the person.
func (l *Library) Check() error {
	path, id := l.Path(), l.ID()
	if path == "" {
		return ErrNone
	}
	return within(l, func() error { return verify(path, id) })
}

// within runs fn and waits for its answer, but gives up waiting after checkWithin (a mount that hangs). Callers that come while a look is under way share its
// answer instead of starting another one, so a hung mount does not pile up goroutines and a busy moment is not mistaken for a hung mount.
func within(l *Library, fn func() error) error {
	l.checking.Lock()
	c := l.call
	if c == nil {
		c = &lookup{done: make(chan struct{})}
		l.call = c
		go func() {
			c.err = fn()
			l.checking.Lock()
			l.call = nil
			l.checking.Unlock()
			close(c.done)
		}()
	}
	l.checking.Unlock()
	select {
	case <-c.done:
		return c.err
	case <-time.After(checkWithin):
		return errors.New("the library folder does not answer")
	}
}

// lookup is a look at the folder that is under way.
type lookup struct {
	done chan struct{}
	err  error
}

func verify(path, id string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the library folder %s is not available (%s)", path, reason(err))
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", path)
	}
	m, err := ReadMarker(path)
	if err != nil {
		return fmt.Errorf("the folder %s is not the library any more: its %s/%s is missing (is the network drive mounted?)", path, MetaDir, markerFile)
	}
	if m.ID != id {
		return fmt.Errorf("the folder %s is another library than the one that was added", path)
	}
	if err := writable(filepath.Join(path, MetaDir)); err != nil {
		return fmt.Errorf("the library folder %s cannot be written to (%s)", path, reason(err))
	}
	return nil
}

// reason is the part of a file error that means something to a person.
func reason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return err.Error()
}

// ReadMarker reads the marker of the library at path.
func ReadMarker(path string) (Marker, error) {
	var m Marker
	data, err := os.ReadFile(filepath.Join(path, MetaDir, markerFile))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.ID == "" {
		return m, errors.New("the marker has no id")
	}
	return m, nil
}

// writable proves a folder can be written to by making and removing a small file in it.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, probeFile+"-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	rerr := os.Remove(name)
	return errors.Join(werr, cerr, rerr)
}

// Info is what the web interface shows about a library.
type Info struct {
	Path       string `json:"path"`
	ID         string `json:"id,omitempty"`
	Available  bool   `json:"available"`
	Problem    string `json:"problem,omitempty"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

// Info looks at the library now.
func (l *Library) Info() Info {
	i := Info{Path: l.Path(), ID: l.ID()}
	if err := l.Check(); err != nil {
		i.Problem = err.Error()
	} else {
		i.Available = true
		i.FreeBytes, i.TotalBytes, _ = DiskSpace(i.Path)
	}
	return i
}

// Inspection is what is known about a folder before it is added as a library. Nothing is written to find it out.
type Inspection struct {
	Path      string `json:"path"`
	IsLibrary bool   `json:"isLibrary"` // it already has a marker (it was a library before: it is taken back as it is)
	FreeBytes uint64 `json:"freeBytes"`
	Problem   string `json:"problem,omitempty"`
}

// forbidden are system folders that are never a library (and what is inside them).
func forbidden() []string {
	if runtime.GOOS == "windows" {
		var out []string
		for _, v := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
			if p := os.Getenv(v); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{"/etc", "/bin", "/sbin", "/usr", "/boot", "/proc", "/sys", "/dev", "/lib", "/lib32", "/lib64", "/run", "/root", "/var/lib/docker"}
}

// Clean makes the folder name absolute and refuses what can never be a library: the top of a disk and system folders.
func Clean(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("choose a folder")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Dir(abs) == abs {
		return "", errors.New("the top of a disk cannot be the library: make a folder for it")
	}
	for _, f := range forbidden() {
		if inside(f, abs) {
			return "", fmt.Errorf("%s is a system folder and cannot be the library", abs)
		}
	}
	return abs, nil
}

func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		rel = strings.ToLower(rel)
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Inspect looks at a folder before it is chosen.
func Inspect(path string) Inspection {
	in := Inspection{Path: path}
	abs, err := Clean(path)
	if err != nil {
		in.Problem = err.Error()
		return in
	}
	in.Path = abs
	res := make(chan Inspection, 1) // (a goroutine stuck on a hung mount writes to its own copy, not to the one returned)
	go func() {
		r := Inspection{Path: abs}
		st, err := os.Stat(abs)
		switch {
		case err != nil:
			r.Problem = "this folder cannot be opened (" + reason(err) + ")"
		case !st.IsDir():
			r.Problem = "this is a file, not a folder"
		default:
			_, merr := ReadMarker(abs)
			r.IsLibrary = merr == nil
			r.FreeBytes, _, _ = DiskSpace(abs)
		}
		res <- r
	}()
	select {
	case in = <-res:
	case <-time.After(checkWithin):
		in.Problem = "the folder does not answer"
	}
	return in
}

// Prepare makes a folder the library: it is checked, the marker is made when there is none (a folder that already has one is taken back with its id), and a
// file is written and removed to prove that it can be written to. It returns the id for the settings. Nothing is left behind when it fails.
func Prepare(path string) (id string, err error) {
	abs, err := Clean(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("this folder cannot be opened (%s)", reason(err))
	}
	if !st.IsDir() {
		return "", errors.New("this is a file, not a folder")
	}
	meta := filepath.Join(abs, MetaDir)
	madeMeta := false
	if _, err := os.Stat(meta); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(meta, 0o755); err != nil {
			return "", fmt.Errorf("the folder cannot be written to (%s): the library needs read and write access", reason(err))
		}
		madeMeta = true
	}
	undo := func() {
		if madeMeta {
			_ = os.RemoveAll(meta)
		}
	}
	if err := writable(meta); err != nil {
		undo()
		return "", fmt.Errorf("the folder cannot be written to (%s): the library needs read and write access", reason(err))
	}
	if m, err := ReadMarker(abs); err == nil {
		return m.ID, nil
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		undo()
		return "", err
	}
	m := Marker{ID: hex.EncodeToString(b), Created: time.Now().UTC(), Format: 1}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(meta, markerFile), append(data, '\n'), 0o644); err != nil {
		undo()
		return "", err
	}
	return m.ID, nil
}

// Dir is one folder in a listing.
type Dir struct {
	Name      string `json:"name"`
	IsLibrary bool   `json:"isLibrary"`
}

// Listing is the folders inside a folder, for the folder picker. Files are never listed.
type Listing struct {
	Path   string `json:"path"`
	Parent string `json:"parent"` // "" at the top
	Dirs   []Dir  `json:"dirs"`
	Cut    bool   `json:"cut"` // more folders than are shown
}

const maxDirs = 500

// Browse lists the folders in path ("" is the top: the drives on Windows, / elsewhere).
func Browse(path string) (Listing, error) {
	if path == "" {
		if runtime.GOOS == "windows" {
			return Listing{Path: "", Dirs: drives()}, nil
		}
		path = "/"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Listing{}, err
	}
	l := Listing{Path: abs, Dirs: []Dir{}}
	if parent := filepath.Dir(abs); parent != abs {
		l.Parent = parent
	} else if runtime.GOOS == "windows" {
		l.Parent = "" // from a drive back to the list of drives
	}
	var entries []os.DirEntry
	done := make(chan error, 1)
	go func() {
		var err error
		entries, err = os.ReadDir(abs)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return Listing{}, fmt.Errorf("this folder cannot be read (%s)", reason(err))
		}
	case <-time.After(checkWithin):
		return Listing{}, errors.New("the folder does not answer")
	}
	for _, e := range entries {
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(abs, e.Name())); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if !isDir || e.Name() == MetaDir {
			continue
		}
		if len(l.Dirs) >= maxDirs {
			l.Cut = true
			break
		}
		_, merr := os.Stat(filepath.Join(abs, e.Name(), MetaDir, markerFile))
		l.Dirs = append(l.Dirs, Dir{Name: e.Name(), IsLibrary: merr == nil})
	}
	sort.Slice(l.Dirs, func(i, j int) bool { return strings.ToLower(l.Dirs[i].Name) < strings.ToLower(l.Dirs[j].Name) })
	return l, nil
}

func drives() []Dir {
	var out []Dir
	for c := 'A'; c <= 'Z'; c++ {
		if _, err := os.Stat(string(c) + `:\`); err == nil {
			out = append(out, Dir{Name: string(c) + `:\`})
		}
	}
	return out
}
