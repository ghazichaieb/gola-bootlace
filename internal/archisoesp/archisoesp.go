// Package archisoesp makes an archiso that keeps its kernel in the ISO's EFI image start over the network (ChimeraOS). Its initramfs has the archiso hooks for a disc or a
// USB stick but not the ones for a network start, and no network card drivers. The extra archive adds a hook of its own (before archiso's) that loads the drivers, gets an
// address, and attaches the ISO as /dev/nbd0, which the kernel arguments name as the device archiso is to look at.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package archisoesp

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

const bootDir = "/arch/boot/x86_64"

// Is says whether the image (the files of an EFI partition) holds an archiso's kernel and initramfs.
func Is(esp interface {
	Exists(string) bool
	List(string) ([]string, error)
}) bool {
	names, err := esp.List(bootDir)
	if err != nil {
		return false
	}
	for _, n := range names {
		if strings.HasPrefix(n, "initramfs-") {
			return true
		}
	}
	return false
}

// hook is the initramfs hook, run before archiso's own.
const hook = `#!/usr/bin/ash
run_hook() {
    msg ":: Gola Bootlace: starting the network"
    /usr/bin/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
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
        msg ":: Gola Bootlace: reading the ISO over the network ($netok)"
        /usr/bin/bootlace-nbd -host %s -port %d -name %s -dev /dev/nbd0 &
        for try in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
            [ "$(cat /sys/block/nbd0/size 2>/dev/null)" != 0 ] && break
            sleep 1
        done
    else
        err "Gola Bootlace: no network card answered, the ISO cannot be read over the network"
    fi
}
`

func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Extra builds the archive. esp is the EFI image's files (it holds the initramfs), iso the ISO's (it holds the system image with the kernel's modules).
func Extra(esp, iso disc.FS, s initrd.Settings, helper []byte) ([]byte, error) {
	names, _ := esp.List(bootDir)
	var img string
	for _, n := range names {
		if strings.HasPrefix(n, "initramfs-") {
			img = bootDir + "/" + n
			break
		}
	}
	if img == "" {
		return nil, errors.New("no initramfs in the EFI image")
	}
	f, err := esp.Open(img)
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader(), "config")
	if err != nil {
		return nil, fmt.Errorf("the initramfs: %w", err)
	}
	config := string(in.Files["config"])
	if !strings.Contains(config, "HOOKS=\"") || !strings.Contains(config, " archiso ") {
		return nil, errors.New("the initramfs has a form that is not known here")
	}
	sq, err := iso.Open("/arch/x86_64/airootfs.sfs")
	if err != nil {
		return nil, err
	}
	// (the drivers' own needs are brought too, also those that the initramfs has: it has them compressed, and the helper loads from plain files)
	mods, err := initread.Modules(sq, in.Version, map[string]bool{}, initread.NICModules)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	// the new hook goes in front of archiso's: the udev hook, which starts the device discovery, stays first
	config = strings.Replace(config, " archiso ", " bootlace archiso ", 1)
	files := []initrd.File{
		{Name: "config", Mode: 0o644, Data: []byte(config)},
		{Name: "hooks/bootlace", Mode: 0o755, Data: []byte(fmt.Sprintf(hook, shellWord(s.Host), s.Port, shellWord(s.Name)))},
		{Name: "bootlace", Mode: 0o755, Dir: true},
		{Name: "bootlace/udhcpc.sh", Mode: 0o755, Data: []byte(initrd.UdhcpcScript)},
		{Name: "bootlace/modules", Mode: 0o755, Dir: true},
		{Name: "usr/bin/bootlace-nbd", Mode: 0o755, Data: helper},
	}
	for i, m := range mods {
		base := path.Base(m.Path)
		files = append(files, initrd.File{Name: fmt.Sprintf("bootlace/modules/%02d-%s.ko", i, strings.SplitN(base, ".ko", 2)[0]), Mode: 0o644, Data: m.Data})
	}
	return initrd.Archive(files), nil
}
