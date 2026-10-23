// Package family recognises which kind of system an ISO (or a partition of a disk image) holds and says how to start it over the network: the kernel and the start-up disk
// come out of the files, and the kernel is told where the rest is. The ISO is never changed.
//
// Every system is described the same way. A *locator* finds the kernel and the start-up disk (in the ISO, in a partition, in the EFI image inside the ISO); a *strategy* says
// how that start-up disk is made to find the ISO, by one of the ways in Transport; and, when the start-up disk lacks something (the network disk client, network card
// drivers), an *extra archive* adds it (package initrd). The files of one family (debian.go, arch.go ...) hold the locators and strategies of that kind of system.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// Tree is the files of one ISO, or of one partition of a disk image.
type Tree interface {
	Exists(name string) bool
	List(name string) ([]string, error)
	Open(name string) (disc.File, error)
}

// URLs says where the web server offers this ISO: the whole file, and its contents file by file (both without a trailing slash).
type URLs struct {
	ISO  string
	FS   string
	Win  string // generated files for Windows set-up (see package winpe)
	Base string // the server

	// NBD says that the ISO can be offered as a network disk, with Extra the address of the extra initramfs archive that makes use of it; both are empty when the server has
	// no network disk server (the whole ISO is then downloaded into the PC's memory, a PC needing RAM for all of it, twice over).
	NBD, Extra string
	// The network disk's address and the ISO's name on it, for the start-up disks that attach it themselves (archiso has its own client).
	NBDHost, NBDName string
	NBDPort          int

	ISCSI string // the ISO as a network CD-ROM (the address iPXE's sanboot takes), when the iSCSI server is on
	// The same network CD-ROM for a start-up disk that logs in to it itself (dracut's iSCSI support): server, port and the target's name. Empty when the iSCSI server is off.
	ISCSIHost, ISCSIIQN string
	ISCSIPort           int
}

// Volume is a set of files besides the ISO's own that a family may need: an EFI image inside the ISO, or a partition of a disk image. URL is where the server offers them.
type Volume struct {
	FS  Tree
	URL string
}

// Context is what is known when an ISO is looked at.
type Context struct {
	FS      Tree            // the ISO's files (or those of the partition being looked at)
	URLs    URLs            // where the server offers them
	Volumes func() []Volume // the other volumes of the same file, found when asked for; may be nil
}

// volumeWith finds a path in the ISO's files or, failing that, in another volume, and says where the server offers the files of the volume it was found in.
func (c *Context) volumeWith(path string) (Volume, bool) {
	if c.FS.Exists(path) {
		return Volume{c.FS, c.URLs.FS}, true
	}
	if c.Volumes != nil {
		for _, v := range c.Volumes() {
			if v.FS.Exists(path) {
				return v, true
			}
		}
	}
	return Volume{}, false
}

// Boot is how to start one ISO.
type Boot struct {
	Family   string   // "Alpine Linux", "Debian live" ...
	Commands []string // iPXE commands: kernel, initrd, boot

	// For a kernel that GRUB (signed by the distribution, so that UEFI Secure Boot can stay on) starts instead of iPXE: the files inside the ISO and the kernel's
	// arguments. Empty for the kinds that cannot be started that way.
	Kernel, Initrd, Args string

	ExtraInitrd string       // an address: a second initramfs archive to load after the ISO's own
	extra       ExtraBuilder // how that archive is made; nil for the usual one
}

// ExtraEnv is what an extra archive is made from besides the files: where the network disk is, and the helper program that attaches it.
type ExtraEnv struct {
	Settings initrd.Settings
	Helper   []byte
}

// ExtraBuilder makes the extra initramfs archive of one ISO. It reads the ISO's files, so it is made while they are still open (see Boot.BuildExtra).
type ExtraBuilder func(ExtraEnv) ([]byte, error)

// BuildExtra makes the extra archive that the boot's ExtraInitrd address serves.
func (b Boot) BuildExtra(env ExtraEnv) ([]byte, error) {
	if b.extra != nil {
		return b.extra(env)
	}
	return initrd.Extra(env.Settings, env.Helper), nil
}

// HasOwnExtra says whether the extra archive is made specially for this ISO (and so takes time to make), not the usual one.
func (b Boot) HasOwnExtra() bool { return b.extra != nil }

// Detect returns the way to boot the ISO, or false when it is not one of the kinds known here.
func Detect(t Tree, u URLs) (Boot, bool) { return DetectIn(&Context{FS: t, URLs: u}) }

// DetectIn is Detect with the context, which can lead to other volumes of the same file.
func DetectIn(c *Context) (Boot, bool) {
	for _, d := range detectors {
		if b, ok := d(c); ok {
			return b, true
		}
	}
	return Boot{}, false
}

// detectors are tried in order, the most specific first (Tails is a Debian live system, Pop!_OS an Ubuntu).
var detectors = []func(*Context) (Boot, bool){
	windows, proxmox, alpine, archLinux, systemRescue, shredOS, openSUSE, fedoraLiveISO, gentooLive, tinyCore, porteus, tails, libreELEC, debianInstaller, debianLive, ubuntu, freeBSD, fedoraLike,
}

// files are the kernel and the start-up disk to load, and the address the server offers the files they are in at.
type files struct {
	base, kernel, initrd string // initrd is empty for a kernel that holds its own
}

// in is the files in a path of the ISO's own volume.
func (c *Context) in(kernel, initrd string) files { return files{c.URLs.FS, kernel, initrd} }

// newBoot is the usual start: load the kernel with the arguments and the start-up disk, and boot.
func newBoot(family string, f files, args string) Boot {
	b := Boot{Family: family, Kernel: f.kernel, Initrd: f.initrd, Args: args, Commands: []string{"kernel " + f.base + f.kernel + " " + args}}
	if f.initrd != "" {
		b.Commands = append(b.Commands, "initrd "+f.base+f.initrd)
	}
	b.Commands = append(b.Commands, "boot")
	return b
}

// withExtra adds the extra initramfs archive after the ISO's own. build says how it is made when it is not the usual one.
func (b Boot) withExtra(u URLs, build ExtraBuilder) Boot {
	b.ExtraInitrd, b.extra = u.Extra, build
	return b.before("initrd " + u.Extra)
}

// before adds an iPXE command just before the final "boot".
func (b Boot) before(cmd string) Boot {
	n := len(b.Commands) - 1
	b.Commands = append(append(append([]string(nil), b.Commands[:n]...), cmd), b.Commands[n:]...)
	return b
}

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
