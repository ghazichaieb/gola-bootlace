// Package libreelec makes LibreELEC and the systems built from it (Lakka) start over the network. They come as a disk image: a FAT partition with a kernel (KERNEL, with
// its initramfs inside) and the system as a file (SYSTEM, a squashfs); the initramfs mounts the partition with "boot=" and can take a network block disk for it
// ("boot=NBD=host:port"), but has no nbd-client program. The extra archive brings one (Bootlace's own helper, which takes the name of the disk to read from a list in the
// archive, since the start-up script does not give one).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package libreelec

import (
	"fmt"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// Is says whether the file system is a LibreELEC-style boot partition.
func Is(t interface{ Exists(string) bool }) bool {
	return t.Exists("/KERNEL") && t.Exists("/SYSTEM") && !t.Exists("/live")
}

// startScript runs instead of the start-up script's first step (rdinit=): the start-up script calls "nbd-client", which its shell takes for BusyBox's own applet of that name
// (an old client that knows no export names) even when a file of that name is on the path, so the call is changed to name the file. Then the start-up script goes on.
const startScript = `#!/bin/sh
# Gola Bootlace
/usr/bin/busybox sed -i 's#nbd-client $NBD_SERVER#/usr/bin/nbd-client $NBD_SERVER#' /init
exec /init
`

// Extra builds the archive: the helper under the name of the client program and the list that says which export is /dev/nbd0 (the boot partition).
func Extra(s initrd.Settings, helper []byte) []byte {
	return initrd.Archive([]initrd.File{
		{Name: "bootlace-init", Mode: 0o755, Data: []byte(startScript)},
		{Name: "etc", Mode: 0o755, Dir: true},
		{Name: "etc/bootlace-nbd", Mode: 0o644, Data: []byte(fmt.Sprintf("nbd0 %s %d\n", s.Name, s.Block))},
		{Name: "usr", Mode: 0o755, Dir: true},
		{Name: "usr/bin", Mode: 0o755, Dir: true},
		{Name: "usr/bin/nbd-client", Mode: 0o755, Data: helper},
	})
}
