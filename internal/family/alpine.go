// Alpine Linux. Its start-up disk carries only the few modules a disc or a disk needs (a virtual network card among them); the rest are in the modloop, which can only be
// fetched once the network is up. So the common wired card drivers are read out of the modloop and put in an extra archive, with a script that the kernel runs in place of the
// start-up script's first step (rdinit=): it loads them with the helper and then goes on with Alpine's own start-up script, which now finds the card. (The initramfs's
// modprobe reads a binary index of its modules, which cannot be extended: the drivers are loaded directly.)
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"fmt"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// alpine: the kernel and initramfs of its flavour, the modules disk and the packages from the ISO.
func alpine(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/.alpine-release") {
		return Boot{}, false
	}
	kernel := firstMatch(t, "/boot", "vmlinuz-")
	if kernel == "" {
		return Boot{}, false
	}
	flavor := strings.TrimPrefix(kernel, "/boot/vmlinuz-")
	initramfs, modloop := "/boot/initramfs-"+flavor, "/boot/modloop-"+flavor
	if !t.Exists(initramfs) {
		return Boot{}, false
	}
	args := "ip=dhcp modules=loop,squashfs,sd-mod,usb-storage,hv_netvsc,virtio_net alpine_repo=" + u.FS + "/apks console=tty0"
	hasModloop := t.Exists(modloop)
	if hasModloop {
		args += " modloop=" + u.FS + modloop
	}
	withDrivers := u.has(NBD) && hasModloop
	if withDrivers {
		args += " rdinit=/bootlace-init"
	}
	b := newBoot("Alpine Linux", c.in(kernel, initramfs), args)
	if !withDrivers {
		return b, true
	}
	return b.withExtra(u, func(env ExtraEnv) ([]byte, error) { return c.alpineExtra(initramfs, modloop, env.Helper) }), true
}

// alpineExtra is the archive with the drivers and the script that loads them.
func (c *Context) alpineExtra(initramfs, modloop string, helper []byte) ([]byte, error) {
	f, err := c.FS.Open(initramfs)
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader())
	if err != nil {
		return nil, fmt.Errorf("the start-up disk of Alpine: %w", err)
	}
	mods, err := c.nicDrivers(modloop, in.Version)
	if err != nil {
		return nil, err
	}
	var b initrd.Builder
	b.Script("bootlace-init", initrd.RdinitWrapper("/bootlace/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1"))
	b.Dir("bootlace")
	b.File("bootlace/bootlace-nbd", 0o755, helper)
	b.Modules("bootlace/modules", mods)
	return b.Bytes(), nil
}
