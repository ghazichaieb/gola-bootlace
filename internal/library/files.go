// Managing the files of the library: list, make a folder, rename or move, delete. Every path comes from the web interface as a "/" path relative to the library, is
// checked to stay inside it, and never reaches a hidden file or the library's own .bootlace folder. Deleting is never recursive: a file that is an ISO or an image,
// or a folder that is empty, and nothing else.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// managedExt are the kinds of file the library offers and manages: ISOs, disk images and the packed forms of them.
var managedExt = map[string]bool{".iso": true, ".img": true, ".raw": true, ".xz": true, ".gz": true, ".zst": true, ".bz2": true, ".zip": true}

// Managed says whether a file name is one the library offers (and so one that can be added, renamed and deleted here).
func Managed(name string) bool { return managedExt[strings.ToLower(filepath.Ext(name))] }

// Errors that the web interface shows as they are.
var (
	ErrExists   = errors.New("something with that name is already there")
	ErrNotEmpty = errors.New("the folder is not empty: only an empty folder can be deleted here")
	ErrNotFound = errors.New("there is no such file or folder in the library")
	ErrManaged  = errors.New("this file is not an ISO or a disk image: it is not managed here")
)

const maxName = 200

// ValidName checks a name that is about to be made (a new file, a folder, the target of a rename).
func ValidName(name string) error {
	switch {
	case name == "" || name == "." || name == "..":
		return errors.New("the name is empty")
	case len(name) > maxName || !utf8.ValidString(name):
		return fmt.Errorf("the name must be valid text of at most %d characters", maxName)
	case strings.HasPrefix(name, "."):
		return errors.New("a name cannot start with a dot (such files are hidden)")
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return errors.New("a name cannot end with a dot or a space")
	case strings.ContainsAny(name, `/\:*?"<>|`):
		return errors.New(`a name cannot contain / \ : * ? " < > |`)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("a name cannot contain control characters")
		}
	}
	return nil
}

// CleanRel turns a path from the web interface into a clean "/" path inside the library ("" is the library itself). Anything that could leave it, or reach
// the library's own files, is refused.
func CleanRel(rel string) (string, error) {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" {
		return "", nil
	}
	var parts []string
	for _, p := range strings.Split(rel, "/") {
		if p != "" { // (a doubled slash is the same as one)
			parts = append(parts, p)
		}
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") || strings.ContainsRune(p, 0) || strings.ContainsAny(p, `:*?"<>|`) || !utf8.ValidString(p) {
			return "", fmt.Errorf("%q is not a path inside the library", rel)
		}
	}
	return strings.Join(parts, "/"), nil
}

// Entry is one file or folder of the library.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // relative to the library, with "/"
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	Mod     time.Time `json:"mod"`
	Managed bool      `json:"managed"` // an ISO or an image: it can be renamed and deleted here
}

// abs is the path of rel inside the library, once it is known that the library is there and that nothing on the way leaves it through a link.
func (l *Library) abs(rel string) (root, path string, err error) {
	if err := l.Check(); err != nil {
		return "", "", err
	}
	rel, err = CleanRel(rel)
	if err != nil {
		return "", "", err
	}
	root = l.Path()
	path = filepath.Join(root, filepath.FromSlash(rel))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if rel != "" && !inside(realRoot, realDir) {
		return "", "", fmt.Errorf("%q is not inside the library", rel)
	}
	return root, path, nil
}

// Abs is the path on disk of a file of the library, for reading it.
func (l *Library) Abs(rel string) (string, error) {
	_, p, err := l.abs(rel)
	return p, err
}

// ListDir lists a folder: folders first, then files, each by name. Hidden files and the library's own folder are left out.
func (l *Library) ListDir(rel string) ([]Entry, error) {
	_, dir, err := l.abs(rel)
	if err != nil {
		return nil, err
	}
	rel, _ = CleanRel(rel)
	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(it.Name(), ".") {
			continue
		}
		info, err := it.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue // (a link could lead anywhere: it is not shown, not offered, not managed)
		}
		e := Entry{Name: it.Name(), Path: strings.TrimPrefix(rel+"/"+it.Name(), "/"), Dir: info.IsDir(), Mod: info.ModTime()}
		if !e.Dir {
			e.Size, e.Managed = info.Size(), Managed(it.Name())
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Mkdir makes one folder inside an existing one.
func (l *Library) Mkdir(rel string) error {
	_, p, err := l.abs(rel)
	if err != nil {
		return err
	}
	rel, _ = CleanRel(rel)
	if rel == "" {
		return errors.New("choose a name for the folder")
	}
	if err := ValidName(filepath.Base(p)); err != nil {
		return err
	}
	if _, err := os.Lstat(p); err == nil {
		return ErrExists
	}
	return os.Mkdir(p, 0o755)
}

// Rename renames or moves a file or a folder (a move is a rename into another folder). A file keeps a name the library offers; nothing is replaced.
func (l *Library) Rename(from, to string) error {
	_, src, err := l.abs(from)
	if err != nil {
		return err
	}
	_, dst, err := l.abs(to)
	if err != nil {
		return err
	}
	from, _ = CleanRel(from)
	to, _ = CleanRel(to)
	if from == "" || to == "" {
		return errors.New("the library itself cannot be renamed")
	}
	info, err := os.Lstat(src)
	if err != nil {
		return ErrNotFound
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("links are not managed here")
	}
	if err := ValidName(filepath.Base(dst)); err != nil {
		return err
	}
	if !info.IsDir() && (!Managed(info.Name()) || !Managed(filepath.Base(dst))) {
		return fmt.Errorf("a file keeps a name that ends in %s", managedList())
	}
	if info.IsDir() && (to == from || strings.HasPrefix(to+"/", from+"/")) {
		return errors.New("a folder cannot be moved into itself")
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrExists
	}
	return os.Rename(src, dst)
}

// Delete removes a file that the library manages, or an empty folder. It never removes anything else, and never a folder with anything in it.
func (l *Library) Delete(rel string) error {
	_, p, err := l.abs(rel)
	if err != nil {
		return err
	}
	rel, _ = CleanRel(rel)
	if rel == "" {
		return errors.New("the library itself cannot be deleted")
	}
	info, err := os.Lstat(p)
	if err != nil {
		return ErrNotFound
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return errors.New("links are not managed here")
	case info.IsDir():
		items, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			return ErrNotEmpty
		}
	case !Managed(info.Name()):
		return ErrManaged
	}
	return os.Remove(p) // (os.Remove, not RemoveAll: a folder with anything in it is refused by the system as well)
}

func managedList() string {
	var l []string
	for e := range managedExt {
		l = append(l, e)
	}
	sort.Strings(l)
	return strings.Join(l, " ")
}
