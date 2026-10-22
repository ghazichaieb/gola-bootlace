// Package alpineinit brings network card drivers to Alpine's start-up disk. Alpine's initramfs only carries the few modules a disc or a disk needs (a virtual network card
// among them); the rest are in the modloop, which can only be fetched once the network is up. So the common wired card drivers are read out of the modloop and put in an
// extra archive, together with a small script that the kernel runs in place of the start-up script's first step (rdinit=): it loads them with Bootlace's helper and then
// goes on with Alpine's own start-up script, which now finds the card. (The initramfs's modprobe reads a binary index of its modules, which cannot be extended.)
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package alpineinit

import (
	"fmt"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// Is says whether the ISO is Alpine's and has what is needed; flavor is the kernel's ("lts").
func Is(t interface{ Exists(string) bool }, flavor string) bool {
	return t.Exists("/.alpine-release") && t.Exists("/boot/initramfs-"+flavor) && t.Exists("/boot/modloop-"+flavor)
}

// startScript is what rdinit= names: the drivers are loaded, then Alpine's own start-up script runs.
const startScript = `#!/bin/sh
# Gola Bootlace: the network card drivers that this start-up disk lacks
/bootlace/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
exec /init
`

// Extra builds the archive for the flavor.
func Extra(iso disc.FS, flavor string, helper []byte) ([]byte, error) {
	f, err := iso.Open("/boot/initramfs-" + flavor)
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader())
	if err != nil {
		return nil, fmt.Errorf("the start-up disk of Alpine: %w", err)
	}
	loop, err := iso.Open("/boot/modloop-" + flavor)
	if err != nil {
		return nil, err
	}
	// (the drivers' own needs are brought too, also those that the initramfs has: it has them but does not load them, and the helper loads from plain files)
	mods, err := initread.Modules(loop, in.Version, map[string]bool{}, initread.NICModules)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	files := []initrd.File{
		{Name: "bootlace-init", Mode: 0o755, Data: []byte(startScript)},
		{Name: "bootlace", Mode: 0o755, Dir: true},
		{Name: "bootlace/bootlace-nbd", Mode: 0o755, Data: helper},
		{Name: "bootlace/modules", Mode: 0o755, Dir: true},
	}
	for i, m := range mods {
		files = append(files, initrd.File{Name: fmt.Sprintf("bootlace/modules/%02d-%s.ko", i, strings.SplitN(path.Base(m.Path), ".ko", 2)[0]), Mode: 0o644, Data: m.Data})
	}
	return initrd.Archive(files), nil
}
