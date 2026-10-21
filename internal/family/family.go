// Package family recognises which kind of Linux an ISO holds and says how to start it over the network: the kernel and the start-up disk come
// out of the ISO, and the kernel is told where the rest of the ISO is on the web server. The distributions' own network boot support does the work
// after that, so the ISO is never changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"sort"
	"strings"
)

// Tree is what Detect needs to look at: the files of an ISO.
type Tree interface {
	Exists(name string) bool
	List(name string) ([]string, error)
}

// URLs says where the web server offers this ISO: the whole file, and its contents file by file (both without a trailing slash).
type URLs struct {
	ISO string
	FS  string
}

// Boot is how to start one ISO.
type Boot struct {
	Family   string   // "Alpine", "Debian live" ...
	Commands []string // iPXE commands: kernel, initrd, boot
}

// Detect returns the way to boot the ISO, or false when it is not one of the kinds known here.
func Detect(t Tree, u URLs) (Boot, bool) {
	for _, d := range detectors {
		if b, ok := d(t, u); ok {
			return b, true
		}
	}
	return Boot{}, false
}

var detectors = []func(Tree, URLs) (Boot, bool){proxmox, alpine, archLinux, debianLive, ubuntu, fedoraLike}

// firstMatch is the first file in dir whose name starts with prefix (sorted, so the choice is the same every time).
func firstMatch(t Tree, dir, prefix string) string {
	names, err := t.List(dir)
	if err != nil {
		return ""
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.HasPrefix(n, prefix) && !strings.HasSuffix(n, "/") {
			return strings.TrimRight(dir, "/") + "/" + n
		}
	}
	return ""
}

func boot(family string, u URLs, kernel, initrd, args string) Boot {
	return Boot{Family: family, Commands: []string{
		"kernel " + u.FS + kernel + " " + args,
		"initrd " + u.FS + initrd,
		"boot",
	}}
}

// Alpine: the kernel and initramfs of its flavour, the modules disk and the packages from the ISO.
func alpine(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/.alpine-release") {
		return Boot{}, false
	}
	kernel := firstMatch(t, "/boot", "vmlinuz-")
	if kernel == "" {
		return Boot{}, false
	}
	flavor := strings.TrimPrefix(kernel, "/boot/vmlinuz-")
	initrd, modloop := "/boot/initramfs-"+flavor, "/boot/modloop-"+flavor
	if !t.Exists(initrd) {
		return Boot{}, false
	}
	args := "ip=dhcp modules=loop,squashfs,sd-mod,usb-storage,hv_netvsc,virtio_net alpine_repo=" + u.FS + "/apks console=tty0"
	if t.Exists(modloop) {
		args += " modloop=" + u.FS + modloop
	}
	return boot("Alpine Linux", u, kernel, initrd, args), true
}

// Arch Linux and its relatives (archiso, CachyOS ...): the boot files and the squashfs come from the web server. The kernel is the one named
// vmlinuz-linux or, in a relative, vmlinuz-linux-<name> (the long-term-support one only when it is the only one).
func archLinux(t Tree, u URLs) (Boot, bool) {
	dir := "/arch/boot/x86_64"
	names, err := t.List(dir)
	if err != nil {
		return Boot{}, false
	}
	sort.Strings(names)
	kernel := ""
	for _, n := range names {
		if strings.HasPrefix(n, "vmlinuz-") && (kernel == "" || (strings.Contains(kernel, "lts") && !strings.Contains(n, "lts"))) {
			kernel = n
		}
	}
	if kernel == "" {
		return Boot{}, false
	}
	initrd := "initramfs-" + strings.TrimPrefix(kernel, "vmlinuz-") + ".img"
	if !t.Exists(dir + "/" + initrd) {
		return Boot{}, false
	}
	return boot("Arch Linux", u, dir+"/"+kernel, dir+"/"+initrd, "ip=:::::eth0:dhcp archisobasedir=arch archiso_http_srv="+u.FS+"/ copytoram=y console=tty0"), true
}

// Debian live and the distributions built the same way (TrueNAS keeps its kernel at the top of the ISO): the squashfs comes from the web server.
func debianLive(t Tree, u URLs) (Boot, bool) {
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
	return boot("Debian live", u, kernel, initrd, "boot=live components ip=dhcp fetch="+u.FS+"/live/filesystem.squashfs console=tty0"), true
}

// Proxmox VE: its installer finds the ISO as a file in the start-up disk, so the ISO is given to the kernel as a second one.
func proxmox(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/.pve-cd-id.txt") || !t.Exists("/boot/linux26") || !t.Exists("/boot/initrd.img") {
		return Boot{}, false
	}
	return Boot{Family: "Proxmox VE", Commands: []string{
		"kernel " + u.FS + "/boot/linux26 ro ramdisk_size=16777216 rw quiet splash=silent console=tty0",
		"initrd " + u.FS + "/boot/initrd.img",
		"initrd --name proxmox.iso " + u.ISO,
		"boot",
	}}, true
}

// Ubuntu and its flavours (casper): the initramfs downloads the ISO into memory.
func ubuntu(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/casper/vmlinuz") {
		return Boot{}, false
	}
	initrd := firstMatch(t, "/casper", "initrd")
	if initrd == "" {
		return Boot{}, false
	}
	return boot("Ubuntu (casper)", u, "/casper/vmlinuz", initrd, "ip=dhcp url="+u.ISO+" boot=casper console=tty0"), true
}

// Fedora, CentOS, Rocky, Alma and other Red Hat family ISOs: an installer reads its packages from the web server, a live ISO is fetched whole.
func fedoraLike(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/images/pxeboot/vmlinuz") || !t.Exists("/images/pxeboot/initrd.img") {
		return Boot{}, false
	}
	if t.Exists("/LiveOS") {
		return boot("Fedora (live)", u, "/images/pxeboot/vmlinuz", "/images/pxeboot/initrd.img", "ip=dhcp root=live:"+u.ISO+" rd.live.image console=tty0"), true
	}
	return boot("Red Hat family (installer)", u, "/images/pxeboot/vmlinuz", "/images/pxeboot/initrd.img", "ip=dhcp inst.repo="+u.FS+" console=tty0"), true
}
