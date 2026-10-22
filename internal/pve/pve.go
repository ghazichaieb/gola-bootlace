// Package pve makes the Proxmox VE installer read its ISO over the network a piece at a time. The installer's start-up disk (its initramfs) looks for the ISO on a local
// drive, or takes it whole from a file inside itself (what PXE booting did until now, which needs RAM for the whole ISO twice over). This package builds an extra initramfs
// archive that goes after the installer's own: a copy of its start-up script with a few lines added that bring up the network, attach the ISO as /dev/nbd0 (see package nbd and
// cmd/bootlace-nbd) and let the script's own search for the ISO find it there, the network card drivers that the start-up disk lacks (taken from the system image on the ISO,
// which carries the full set of kernel modules), and the little scripts they need. The installer's files are not changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package pve

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// Settings say where the ISO is offered as a network block disk.
type Settings struct {
	Host string
	Port int
	Name string
}

// Is says whether the ISO is a Proxmox installer of the kind handled here.
func Is(t interface{ Exists(string) bool }) bool {
	return t.Exists("/.pve-cd-id.txt") && t.Exists("/boot/initrd.img") && t.Exists("/pve-installer.squashfs")
}

// Extra builds the archive for one ISO. img is the ISO's file system; the installer's start-up disk and its system image are read from it.
func Extra(img disc.FS, s Settings, helper []byte) ([]byte, error) {
	f, err := img.Open("/boot/initrd.img")
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader(), "init")
	if err != nil {
		return nil, fmt.Errorf("the installer's start-up disk: %w", err)
	}
	patched, ok := Patch(string(in.Files["init"]), s)
	if !ok {
		return nil, errors.New("the installer's start-up script has a form that is not known here")
	}
	sq, err := img.Open("/pve-installer.squashfs")
	if err != nil {
		return nil, err
	}
	mods, err := initread.Modules(sq, in.Version, in.Have, initread.NICModules)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	files := []initrd.File{
		{Name: "init", Mode: 0o755, Data: []byte(patched)},
		{Name: "bootlace", Mode: 0o755, Dir: true},
		{Name: "bootlace/bootlace-nbd", Mode: 0o755, Data: helper},
		{Name: "bootlace/udhcpc.sh", Mode: 0o755, Data: []byte(udhcpcScript)},
		{Name: "bootlace/modules", Mode: 0o755, Dir: true},
	}
	for i, m := range mods {
		files = append(files, initrd.File{Name: fmt.Sprintf("bootlace/modules/%02d-%s", i, path.Base(m.Path)), Mode: 0o644, Data: m.Data})
	}
	return initrd.Archive(files), nil
}

const udhcpcScript = `#!/bin/sh
# Gola Bootlace: what busybox's udhcpc runs when it has an address.
case "$1" in
bound|renew)
    ifconfig "$interface" "$ip" netmask "${subnet:-255.255.255.0}" up
    for r in $router; do
        route add default gw "$r" dev "$interface" 2>/dev/null
    done
    ;;
esac
exit 0
`

// Patch adds Bootlace's lines to the installer's start-up script, or says that the script does not look as expected (then the old way, the whole ISO inside the
// initramfs, is still there).
func Patch(script string, s Settings) (string, bool) {
	const anchor = "# we have no iscsi daemon"
	const scan = "/sys/block/nvme*; do"
	if strings.Count(script, anchor) != 1 || strings.Count(script, scan) != 1 {
		return "", false
	}
	block := fmt.Sprintf(netBlock, shellWord(s.Host), s.Port, shellWord(s.Name))
	script = strings.Replace(script, anchor, block+anchor, 1)
	script = strings.Replace(script, scan, "/sys/block/nvme* /sys/block/nbd0; do", 1)
	return script, true
}

// netBlock is added before the script looks for the ISO. Everything it needs is in the archive; without the network the script goes on as before and does not find an ISO.
const netBlock = `# Gola Bootlace: the ISO is read over the network a piece at a time (a network block disk), so the PC does not need memory for all of it.
if [ -x /bootlace/bootlace-nbd ]; then
    echo "Gola Bootlace: starting the network"
    /bootlace/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
    modprobe -q nbd
    netok=
    for try in 1 2 3 4 5 6 7 8 9 10 11 12; do
        for n in /sys/class/net/*; do
            dev="${n##*/}"
            [ "$dev" = lo ] && continue
            [ -d "$n" ] || continue
            ip link set "$dev" up
            if udhcpc -i "$dev" -n -q -t 3 -T 2 -s /bootlace/udhcpc.sh >/dev/null 2>&1; then
                netok=$dev
                break
            fi
        done
        [ -n "$netok" ] && break
        sleep 2
    done
    if [ -n "$netok" ]; then
        echo "Gola Bootlace: reading the ISO over the network ($netok)"
        /bootlace/bootlace-nbd -host %s -port %d -name %s -dev /dev/nbd0 &
        for try in 1 2 3 4 5 6 7 8 9 10; do
            [ "$(cat /sys/block/nbd0/size 2>/dev/null)" != 0 ] && break
            sleep 1
        done
    else
        echo "Gola Bootlace: no network card answered, the ISO cannot be read over the network"
    fi
fi

`

// shellWord keeps a value from being read as anything but one word.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
