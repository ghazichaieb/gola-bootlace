// What the library's drivers folder holds, for the page that switches driver injection on (issue #18).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/ghazichaieb/gola-bootlace/internal/winpe"
)

// driverFolder is one folder of the drivers folder: "all" (every Windows ISO) or the name of an ISO file without its extension.
type driverFolder struct {
	Name     string `json:"name"`
	Packages int    `json:"packages"` // the .inf files
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
	All      bool   `json:"all"` // for every ISO
}

// driverFolders lists the folders under <library>/drivers with what they hold; none when there is no such folder.
func driverFolders(library string) []driverFolder {
	out := []driverFolder{}
	if library == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Join(library, winpe.DriversFolder))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		inv, err := winpe.Scan(winpe.Set{Dir: filepath.Join(library, winpe.DriversFolder, e.Name())})
		if err != nil {
			continue
		}
		out = append(out, driverFolder{Name: e.Name(), Packages: inv.Packages, Files: inv.Files, Bytes: inv.Bytes, All: e.Name() == winpe.DriversAll})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
