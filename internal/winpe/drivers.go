// Drivers for Windows Setup (issue #18): driver packages kept in the library's drivers folder are added to the copy of boot.wim that Bootlace serves and are loaded in the set-up
// environment with drvload, so that Setup sees a disk or a network card that Windows does not know; Setup is also told to put them into the Windows it installs. The ISO itself is
// never changed: only the copy of boot.wim that the server hands out holds them.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package winpe

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DriversFolder is the folder of the library that holds the drivers: drivers/all/ for every Windows ISO and drivers/<file name of an ISO without its extension>/ for one ISO.
const DriversFolder = "drivers"

// DriversAll is the name of the folder for every ISO.
const DriversAll = "all"

// MaxDriverBytes is the most that is added to the boot.wim copy: the whole set-up environment is held in the PC's memory.
const MaxDriverBytes = 1 << 30

// Set is one folder of drivers and where it goes inside the boot.wim.
type Set struct {
	Dir string // the folder on disk
	WIM string // the folder inside the WIM, for example /$WinPEDriver$/all
}

// SetsFor are the folders of drivers that apply to the ISO with this file name (path under the library, with or without folders), those that exist.
func SetsFor(library, isoName string) []Set {
	base := strings.TrimSuffix(filepath.Base(filepath.FromSlash(isoName)), filepath.Ext(isoName))
	var out []Set
	for _, n := range []struct{ folder, wim string }{{DriversAll, "/$WinPEDriver$/all"}, {base, "/$WinPEDriver$/iso"}} {
		if n.folder == "" || n.folder == "." || strings.ContainsAny(n.folder, `/\`) || n.folder == ".." {
			continue
		}
		if n.wim == "/$WinPEDriver$/iso" && base == DriversAll {
			continue
		}
		dir := filepath.Join(library, DriversFolder, n.folder)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, Set{Dir: dir, WIM: n.wim})
		}
	}
	return out
}

// Inventory is what a set holds.
type Inventory struct {
	Packages int   // the .inf files
	Files    int   // every file
	Bytes    int64 // their size
	Print    string
}

// Scan reads a set: the number of driver packages (.inf files), the files, their size and a fingerprint of their names, sizes and dates (what the cached boot.wim copy is keyed by).
// Links and files that are not regular are skipped (they are not copied either).
func Scan(set Set) (Inventory, error) {
	var inv Inventory
	h := sha1.New()
	var rows []string
	err := filepath.WalkDir(set.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(set.Dir, path)
		inv.Files++
		inv.Bytes += info.Size()
		if strings.EqualFold(filepath.Ext(path), ".inf") {
			inv.Packages++
		}
		rows = append(rows, fmt.Sprintf("%s|%d|%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		return inv, err
	}
	sort.Strings(rows)
	fmt.Fprintf(h, "%s\n%s", set.WIM, strings.Join(rows, "\n"))
	inv.Print = hex.EncodeToString(h.Sum(nil))[:12]
	return inv, nil
}

// Plan is what is added for one ISO: the sets with something in them, the fingerprint of all of them, and the total size. An error says a set cannot be used (too big, unreadable).
func Plan(sets []Set) (use []Set, print string, bytes int64, err error) {
	h := sha1.New()
	for _, s := range sets {
		inv, e := Scan(s)
		if e != nil {
			return nil, "", 0, e
		}
		if inv.Packages == 0 {
			continue // (a folder without a .inf is not a driver)
		}
		use = append(use, s)
		bytes += inv.Bytes
		fmt.Fprint(h, inv.Print)
	}
	if bytes > MaxDriverBytes {
		return nil, "", bytes, fmt.Errorf("the drivers hold %d MB: at most %d MB can be added, because the whole set-up environment is held in the PC's memory", bytes>>20, MaxDriverBytes>>20)
	}
	if len(use) == 0 {
		return nil, "", 0, nil
	}
	return use, hex.EncodeToString(h.Sum(nil))[:12], bytes, nil
}
