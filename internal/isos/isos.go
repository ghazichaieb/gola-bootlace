// Package isos finds the ISO files the server offers: every .iso under the ISO folder, in a stable order.
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
		if !strings.EqualFold(filepath.Ext(path), ".iso") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		sum := sha256.Sum256([]byte(name))
		found = append(found, ISO{Name: name, ID: hex.EncodeToString(sum[:5]), ModTime: info.ModTime(), Title: strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)), Path: path, Size: info.Size()})
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
