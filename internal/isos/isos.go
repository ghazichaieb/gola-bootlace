// Package isos finds the files the server offers: every .iso under the ISO folder, and the disk images (.img, .raw, and those packed as .gz, .xz, .zst or .bz2), in a stable order.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package isos

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Packed  string // for a disk image that is compressed: "gz", "xz", "zst" or "bz2"
}

// packers are the compression suffixes of disk images that are unpacked once into the cache folder.
var packers = map[string]string{".gz": "gz", ".xz": "xz", ".zst": "zst", ".bz2": "bz2"}

// classify says what a file name is: an ISO, a disk image (with the compression it has), or not offered.
func classify(name string) (title string, disk bool, packed string, ok bool) {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".iso") {
		return base[:len(base)-4], false, "", true
	}
	if p, found := packers[filepath.Ext(lower)]; found {
		base, lower, packed = base[:len(base)-len(filepath.Ext(base))], strings.TrimSuffix(lower, filepath.Ext(lower)), p
	}
	for _, ext := range []string{".img", ".raw"} {
		if strings.HasSuffix(lower, ext) {
			return base[:len(base)-len(ext)], true, packed, true
		}
	}
	return "", false, "", false
}

// Scan lists the ISO files under dir (at most four folders deep), sorted by name. A folder that does not exist is an empty list, not an error:
// the server can start before the first ISO is copied in.
func Scan(dir string) ([]ISO, error) {
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
		if d.IsDir() {
			if strings.Count(filepath.ToSlash(rel), "/") >= 4 {
				return fs.SkipDir
			}
			return nil
		}
		title, disk, packed, ok := classify(path)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		sum := sha256.Sum256([]byte(name))
		found = append(found, ISO{Name: name, ID: hex.EncodeToString(sum[:5]), ModTime: info.ModTime(), Title: title, Path: path, Size: info.Size(), Disk: disk, Packed: packed})
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
