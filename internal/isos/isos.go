// Package isos finds the files the server offers: every .iso under the ISO folder, and the disk images (.img, .raw), in a stable order. Either may also be packed (.gz, .xz,
// .zst, .bz2) or be inside a .zip archive: such a file is offered once it has been unpacked into the cache folder (see package diskimg).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package isos

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ISO is one file of the folder. Name is its path under the folder with forward slashes (the name used in URLs); Title is what the menu shows.
type ISO struct {
	Name    string
	ID      string // short and stable, for web addresses that name the ISO without its folders
	Title   string
	Path    string
	Size    int64
	ModTime time.Time
	Disk    bool   // a disk image (shown to a PC as a hard disk), not an ISO
	Packed  string // for a file that is compressed: "gz", "xz", "zst" or "bz2", or "zip" (an archive with the ISO or image inside)
	Inner   string // for "zip": the name of the file inside that is offered
}

// packers are the compression suffixes of disk images that are unpacked once into the cache folder.
var packers = map[string]string{".gz": "gz", ".xz": "xz", ".zst": "zst", ".bz2": "bz2"}

// classify says what a file name is: an ISO or a disk image (with the compression it has), or not offered. (A zip archive is looked into, see peekZip.)
func classify(name string) (title string, disk bool, packed string, ok bool) {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	if p, found := packers[filepath.Ext(lower)]; found {
		base, lower, packed = base[:len(base)-len(filepath.Ext(base))], strings.TrimSuffix(lower, filepath.Ext(lower)), p
	}
	if strings.HasSuffix(lower, ".iso") {
		return base[:len(base)-4], false, packed, true
	}
	for _, ext := range []string{".img", ".raw"} {
		if strings.HasSuffix(lower, ext) {
			return base[:len(base)-len(ext)], true, packed, true
		}
	}
	return "", false, "", false
}

type zipKey struct {
	path string
	size int64
	mod  int64
}

type zipInfo struct {
	inner string
	disk  bool
	ok    bool
}

var (
	zipMu    sync.Mutex
	zipCache = map[zipKey]zipInfo{}
)

// peekZip looks at the file list of a zip archive for the ISO or disk image inside (the largest one, when there are several). The answer is kept: the folder is read for every request.
func peekZip(path string, size int64, mod time.Time) (inner string, disk, ok bool) {
	k := zipKey{path, size, mod.UnixNano()}
	zipMu.Lock()
	if z, found := zipCache[k]; found {
		zipMu.Unlock()
		return z.inner, z.disk, z.ok
	}
	zipMu.Unlock()
	var z zipInfo
	if r, err := zip.OpenReader(path); err == nil {
		var best uint64
		for _, f := range r.File {
			if f.FileInfo().IsDir() {
				continue
			}
			lower := strings.ToLower(f.Name)
			isISO := strings.HasSuffix(lower, ".iso")
			isImg := strings.HasSuffix(lower, ".img") || strings.HasSuffix(lower, ".raw")
			if (isISO || isImg) && f.UncompressedSize64 > best {
				best, z = f.UncompressedSize64, zipInfo{inner: f.Name, disk: isImg, ok: true}
			}
		}
		r.Close()
	}
	zipMu.Lock()
	zipCache[k] = z
	zipMu.Unlock()
	return z.inner, z.disk, z.ok
}

// Scan lists the ISO files under dir (at most four folders deep), sorted by name. A folder that does not exist is an empty list, not an error:
// the server can start before the first ISO is copied in.
func Scan(dir string) ([]ISO, error) {
	if dir == "" { // no library chosen: not the current folder, which is what an empty name would mean
		return nil, nil
	}
	var found []ISO
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if path != root && strings.HasPrefix(d.Name(), ".") { // the library's own folder (.bootlace) and hidden files, such as the "._name" copies a Mac leaves
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(filepath.ToSlash(rel), "/") >= 4 {
				return fs.SkipDir
			}
			return nil
		}
		title, disk, packed, ok := classify(path)
		inner := ""
		var info fs.FileInfo
		if !ok && strings.HasSuffix(strings.ToLower(path), ".zip") {
			if info, err = d.Info(); err != nil {
				return nil
			}
			if inner, disk, ok = peekZip(path, info.Size(), info.ModTime()); ok {
				packed = "zip"
				title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				for _, e := range []string{".img", ".iso", ".raw"} {
					title = strings.TrimSuffix(title, e)
				}
			}
		}
		if !ok {
			return nil
		}
		if info == nil {
			if info, err = d.Info(); err != nil {
				return nil
			}
		}
		name := filepath.ToSlash(rel)
		found = append(found, ISO{Name: name, ID: IDOf(name), ModTime: info.ModTime(), Title: title, Path: path, Size: info.Size(), Disk: disk, Packed: packed, Inner: inner})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return strings.ToLower(found[i].Name) < strings.ToLower(found[j].Name) })
	return found, nil
}

// Find returns the ISO called name (as Scan names it), or false. A name that tries to leave the folder is never found.
func Find(dir, name string) (ISO, bool) {
	list, err := Scan(dir)
	if err != nil {
		return ISO{}, false
	}
	for _, iso := range list {
		if iso.Name == name {
			return iso, true
		}
	}
	return ISO{}, false
}

// IDOf is the short id of an ISO by its name (its path in the library with "/"): what the settings and the web addresses call it.
func IDOf(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:5])
}
