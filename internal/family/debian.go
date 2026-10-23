// Debian and Ubuntu: live systems made with initramfs-tools (Ubuntu's casper, Debian's live-boot, and what is built on them: Mint, Pop!_OS, Tails, VyOS, TrueNAS SCALE) and
// the Debian installer.
//
// casper and live-boot both mount the ISO with the nfsmount command when told "netboot=nfs", and the extra archive replaces that command by one that attaches the network
// disk (package initrd): the ISO is read a piece at a time. Tails and VyOS need more (see below).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// Ubuntu and its flavours (casper).
func ubuntu(c *Context) (Boot, bool) {
	t := c.FS
	dir, kernel := casperDir(t)
	if dir == "" {
		return Boot{}, false
	}
	initrd := firstMatch(t, dir, "initrd")
	if initrd == "" {
		return Boot{}, false
	}
	return c.casper("Ubuntu (casper)", c.in(kernel, initrd), dir, firstMatch(t, dir, "ubuntu-server") != "", "", nil), true
}

// casper is how a start-up disk with Ubuntu's casper scripts is told to find its ISO: through the network disk if there is one, otherwise the whole ISO is downloaded.
// dir is the folder of the live system (casper looks in /casper unless told); args are the system's own arguments that go with these.
func (c *Context) casper(family string, f files, dir string, server bool, args string, extra ExtraBuilder) Boot {
	u := c.URLs
	livePath := ""
	if dir != "/casper" { // Pop!_OS and others keep the live system in a folder of their own name, which casper is told about
		livePath = " live-media-path=" + dir
	}
	if u.pick(NBD, Memory) == NBD {
		// cloud-init stays on for the server ISOs: casper hands them the network settings of the start-up (which the network disk needs kept: see package initrd), and with
		// cloud-init off they get a plain "DHCP on every card" instead, take another address and lose the connection to the ISO. The desktop ISOs do not need it.
		cloudInit := " cloud-init=disabled"
		if server {
			cloudInit = ""
		}
		return newBoot(family, f, "ip=dhcp boot=casper netboot=nfs nfsroot="+u.NBD+livePath+cloudInit+args+" console=tty0").withExtra(u, extra)
	}
	return newBoot(family, f, "ip=dhcp url="+u.ISO+" boot=casper"+livePath+" cloud-init=disabled"+args+" console=tty0")
}

// casperDir finds the folder of a casper live system and its kernel: /casper, or a folder whose name starts with casper (Pop!_OS: casper_pop-os_24.04_..., kernel vmlinuz.efi).
func casperDir(t Tree) (dir, kernel string) {
	if t.Exists("/casper/vmlinuz") {
		return "/casper", "/casper/vmlinuz"
	}
	names, _ := t.List("/")
	sort.Strings(names)
	for _, n := range names {
		if strings.HasPrefix(n, "casper") && strings.HasSuffix(n, "/") {
			d := "/" + strings.TrimSuffix(n, "/")
			if k := firstMatch(t, d, "vmlinuz"); k != "" {
				return d, k
			}
		}
	}
	return "", ""
}

// Debian live and the distributions built the same way (TrueNAS keeps its kernel at the top of the ISO).
func debianLive(c *Context) (Boot, bool) {
	t := c.FS
	if !t.Exists("/live/filesystem.squashfs") {
		return Boot{}, false
	}
	kernel, initrd := firstMatch(t, "/live", "vmlinuz"), firstMatch(t, "/live", "initrd")
	if kernel == "" && t.Exists("/vmlinuz") {
		kernel = "/vmlinuz"
	}
	if initrd == "" && t.Exists("/initrd.img") {
		initrd = "/initrd.img"
	}
	if kernel == "" || initrd == "" {
		return Boot{}, false
	}
	f := c.in(kernel, initrd)
	if isVyOS(t) {
		// VyOS's start-up disk has no network block disk driver, so only its system image (a few hundred MB, not the whole ISO) is downloaded. Its own boot menu passes these
		// options: without noautologin the console logs in as a user that does not exist, over and over.
		return newBoot("Debian live", f, "boot=live components hostname=vyos username=live nopersistence noautologin union=overlay net.ifnames=0 biosdevname=0 ip=dhcp fetch="+c.URLs.FS+"/live/filesystem.squashfs console=tty0"), true
	}
	// (the usual layout has the kernel in /live: the other, TrueNAS's, was only ever started by fetching its system image)
	return c.liveBoot("Debian live", f, "/live/filesystem.squashfs", "", strings.HasPrefix(kernel, "/live/"), nil), true
}

// liveBoot is how a start-up disk with Debian's live-boot scripts is told to find its ISO: through the network disk if there is one and network says it works, otherwise
// only the system image (image, a path in the ISO) is downloaded. args are the system's own arguments that go with these.
func (c *Context) liveBoot(family string, f files, image, args string, network bool, extra ExtraBuilder) Boot {
	u := c.URLs
	if network && u.has(NBD) {
		return newBoot(family, f, "boot=live components netboot=nfs nfsroot="+u.NBD+args+" console=tty0").withExtra(u, extra)
	}
	return newBoot(family, f, "boot=live components ip=dhcp fetch="+u.FS+image+args+" console=tty0")
}

// isVyOS says whether a live folder is VyOS's (its kernel and initramfs files carry "vyos" in their names).
func isVyOS(t Tree) bool {
	names, _ := t.List("/live")
	for _, n := range names {
		if strings.Contains(n, "vyos") {
			return true
		}
	}
	return false
}

// Tails comes as a disk image with one FAT partition (kernel and initramfs in /live, the module name in /live/Tails.module). Its arguments are its own, but for the ones that
// find the start-up medium: live-boot is told to take the system from the network disk instead. Its initramfs is made for a USB stick and has no network card drivers at all,
// so the common ones are read out of its system image and loaded by a script that the initramfs runs first (init-top).
func tails(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/live/Tails.module") || !t.Exists("/live/vmlinuz") || !t.Exists("/live/initrd.img") || !t.Exists("/live/filesystem.squashfs") {
		return Boot{}, false
	}
	const own = "boot=live config nopersistence noprompt timezone=Etc/UTC splash noautologin module=Tails"
	const hardening = "slab_nomerge slub_debug=FZ mce=0 vsyscall=none init_on_free=1 mds=full,nosmt page_alloc.shuffle=1 randomize_kstack_offset=on efi_pstore.pstore_disable=1 erst_disable spec_store_bypass_disable=on systemd.condition_needs_update=no proc_mem.force_override=ptrace"
	f := c.in("/live/vmlinuz", "/live/initrd.img")
	if u.has(NBD) {
		return newBoot("Tails", f, own+" netboot=nfs nfsroot="+u.NBD+" "+hardening+" console=tty0").withExtra(u, c.drivers(Volume{c.FS, u.FS}, "/live/initrd.img", "/live/filesystem.squashfs")), true
	}
	return newBoot("Tails", f, own+" ip=dhcp fetch="+u.FS+"/live/filesystem.squashfs "+hardening+" console=tty0"), true
}

// initTopLoader is the initramfs script that loads the drivers: initramfs-tools runs the scripts that /scripts/init-top/ORDER names, before the devices are looked at.
const initTopLoader = `#!/bin/sh
# Gola Bootlace: the network card drivers that this start-up disk lacks
PREREQ=""
prereqs() { echo "$PREREQ"; }
case "$1" in
prereqs) prereqs; exit 0;;
esac
/usr/bin/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
exit 0
`

// drivers makes the archive of a start-up disk made with initramfs-tools that has no network card drivers (Tails's, or an unknown ISO's): the usual archive, the drivers
// read from the system image (image, or one found in the ISO when it is empty) and the script that loads them, which the initramfs's ORDER file is made to run first.
func (c *Context) drivers(vol Volume, initramfs, image string) ExtraBuilder {
	return func(env ExtraEnv) ([]byte, error) {
		const order = "scripts/init-top/ORDER"
		f, err := vol.FS.Open(initramfs)
		if err != nil {
			return nil, err
		}
		in, err := initread.Read(f.Reader(), order)
		if err != nil {
			return nil, fmt.Errorf("the start-up disk: %w", err)
		}
		if !strings.Contains(string(in.Files[order]), "/scripts/init-top/") {
			return nil, errors.New("the start-up disk has a form that is not known here")
		}
		mods, err := c.nicDrivers(image, in.Version)
		if err != nil {
			return nil, err
		}
		var b initrd.Builder
		b.Archive(env.Settings, env.Helper)
		b.Script("scripts/init-top/bootlace-nic", initTopLoader)
		b.File(order, 0o644, []byte("/scripts/init-top/bootlace-nic \"$@\"\n[ -e /conf/param.conf ] && . /conf/param.conf\n"+string(in.Files[order])))
		b.Dir("bootlace")
		b.Modules("bootlace/modules", mods)
		return b.Bytes(), nil
	}
}

// Debian's installer and the ones built on it (Kali's): kernel and initrd come out of the ISO, the packages from a mirror or from the ISO's own files over the web server.
func debianInstaller(c *Context) (Boot, bool) {
	t := c.FS
	if !t.Exists("/install.amd/vmlinuz") {
		return Boot{}, false
	}
	initrd := "/install.amd/gtk/initrd.gz"
	if !t.Exists(initrd) {
		initrd = "/install.amd/initrd.gz"
	}
	if !t.Exists(initrd) {
		return Boot{}, false
	}
	return newBoot("Debian installer", c.in("/install.amd/vmlinuz", initrd), "vga=788 --- quiet"), true
}
