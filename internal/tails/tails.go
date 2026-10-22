// Package tails makes Tails start over the network. Tails comes as a disk image for a USB stick, and its initramfs has no network card drivers at all (a PC starts it from the
// stick). The drivers are in the system image next to it, so the extra archive brings the common ones along, loaded by a script that the initramfs runs first (init-top), and
// the usual network disk files that let live-boot take the system from Bootlace (see package initrd).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package tails

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// Is says whether the file system is Tails's.
func Is(t interface{ Exists(string) bool }) bool {
	return t.Exists("/live/Tails.module") && t.Exists("/live/initrd.img") && t.Exists("/live/filesystem.squashfs")
}

// loadDrivers is the initramfs script that loads the drivers: initramfs-tools runs the scripts that /scripts/init-top/ORDER names, before the devices are looked at.
const loadDrivers = `#!/bin/sh
# Gola Bootlace: the network card drivers that this start-up disk lacks
PREREQ=""
prereqs() { echo "$PREREQ"; }
case "$1" in
prereqs) prereqs; exit 0;;
esac
/usr/bin/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
exit 0
`

// Extra builds the archive for Tails on a FAT partition (img): the generic files of a network disk and the drivers.
func Extra(img disc.FS, s initrd.Settings, helper []byte) ([]byte, error) {
	f, err := img.Open("/live/initrd.img")
	if err != nil {
		return nil, err
	}
	const order = "scripts/init-top/ORDER"
	in, err := initread.Read(f.Reader(), order)
	if err != nil {
		return nil, fmt.Errorf("the start-up disk of Tails: %w", err)
	}
	if !strings.Contains(string(in.Files[order]), "/scripts/init-top/") {
		return nil, errors.New("the start-up disk of Tails has a form that is not known here")
	}
	sq, err := img.Open("/live/filesystem.squashfs")
	if err != nil {
		return nil, err
	}
	mods, err := initread.Modules(sq, in.Version, in.Have, initread.NICModules)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	files := []initrd.File{
		{Name: "scripts/init-top/bootlace-nic", Mode: 0o755, Data: []byte(loadDrivers)},
		{Name: order, Mode: 0o644, Data: []byte("/scripts/init-top/bootlace-nic \"$@\"\n[ -e /conf/param.conf ] && . /conf/param.conf\n" + string(in.Files[order]))},
		{Name: "bootlace", Mode: 0o755, Dir: true},
		{Name: "bootlace/modules", Mode: 0o755, Dir: true},
	}
	for i, m := range mods {
		files = append(files, initrd.File{Name: fmt.Sprintf("bootlace/modules/%02d-%s", i, strings.TrimSuffix(path.Base(m.Path), path.Ext(path.Base(m.Path)))+".ko"), Mode: 0o644, Data: m.Data})
	}
	// two archives one after the other are one initramfs to the kernel
	return append(initrd.Extra(s, helper), initrd.Archive(files)...), nil
}
