// An ISO that is not known by name. The systems that Bootlace knows are told apart by their files; one it has never seen may still be made like one of them, and its own boot
// menu says where its kernel is and what the distribution passes to it. So the menu is read (package bootcfg), the start-up disk is looked into (what it is made of: flavor.go)
// and, if it is one of the makes that are known, it is started the way the named systems of that make are, with the menu's own arguments in place of the ones that said where
// the ISO is. Looking into a start-up disk takes seconds, which is why this runs after the named families and not instead of them.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/bootcfg"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// maxLooked is how many different start-up disks of an ISO are looked into (a menu often starts the same one in several ways).
const maxLooked = 3

// DetectGeneric is Detect for an ISO that no family is known by: from the ISO's boot menu and from what its start-up disk is made of.
func DetectGeneric(c *Context) (Boot, bool) {
	seen := map[string]bool{}
	for _, m := range c.menuEntries() {
		key := m.vol.URL + m.initrd
		if seen[key] {
			continue
		}
		if len(seen) == maxLooked {
			break
		}
		seen[key] = true
		fa, ok := c.Findings.remember(c.Stamp+"|"+key, func() (facts, bool) { return inspectStartUpDisk(m) })
		if !ok {
			continue
		}
		if b, ok := c.fromFacts(m, fa); ok {
			return b, true
		}
	}
	return Boot{}, false
}

// inspectStartUpDisk looks into the start-up disk of a menu entry.
func inspectStartUpDisk(m menuEntry) (facts, bool) {
	f, err := m.vol.FS.Open(m.initrd)
	if err != nil {
		return facts{}, false
	}
	l, err := initread.Inspect(f.Reader())
	if err != nil {
		return facts{}, false
	}
	return identify(l), true
}

// fromFacts starts the entry the way the named systems of its make are started.
func (c *Context) fromFacts(m menuEntry, fa facts) (Boot, bool) {
	f := files{m.vol.URL, m.kernel, m.initrd}
	dir := path.Dir(m.kernel)
	var own ExtraBuilder // for a start-up disk that has no network card drivers
	if !fa.nicDrivers {
		own = c.drivers(m.vol, m.initrd, "")
	}
	switch fa.flavor {
	case flavorCasper:
		server := firstMatch(m.vol.FS, dir, "ubuntu-server") != ""
		return c.restricted(fa.nbdDriver, true).casper("Ubuntu-style (casper), by its boot menu", f, dir, server, menuArgs(m, dropForInitramfsTools), own), true
	case flavorLiveBoot:
		image := c.liveImage(dir)
		if image == "" {
			return Boot{}, false
		}
		return c.restricted(fa.nbdDriver, true).liveBoot("Debian-style live, by its boot menu", f, image, menuArgs(m, dropForInitramfsTools), strings.HasPrefix(m.kernel, "/live/"), own), true
	case flavorArchiso:
		basedir := argValue(m.Args, "archisobasedir", "arch")
		args := menuArgs(m, dropForArchiso)
		if fa.archisoNet {
			return c.archiso("Arch-style (archiso), by its boot menu", f, "ip=:::::eth0:dhcp", basedir, args), true
		}
		return c.archisoHook("Arch-style (archiso, no network hooks), by its boot menu", m.vol, f, basedir, "", args)
	case flavorDracut:
		cc := c.restricted(fa.netroot, fa.iscsi)
		if !cc.URLs.has(NBD) && !cc.URLs.has(ISCSI) && !fa.livenet {
			return Boot{}, false
		}
		return cc.dracutLive("dracut live, by its boot menu", f, menuArgs(m, dropForDracut)), true
	}
	return Boot{}, false
}

// restricted is the context with only the ways to reach the ISO that the start-up disk can use: the network disk and the iSCSI CD.
func (c *Context) restricted(nbd, iscsi bool) *Context {
	cc := *c
	if !nbd {
		cc.URLs.NBD = ""
	}
	if !iscsi {
		cc.URLs.ISCSIHost = ""
	}
	return &cc
}

// liveImage is the system image of a live-boot ISO: filesystem.squashfs next to the kernel or in /live, or else the biggest squashfs of the ISO.
func (c *Context) liveImage(dir string) string {
	for _, d := range []string{dir, "/live"} {
		if img := strings.TrimRight(d, "/") + "/filesystem.squashfs"; c.FS.Exists(img) {
			return img
		}
	}
	if imgs := c.systemImages(); len(imgs) > 0 {
		return imgs[0]
	}
	return ""
}

// menuArgs are the menu entry's arguments that go with the ones Bootlace adds: the ones that say where the ISO is, or that Bootlace sets itself, are taken out. The result
// starts with a space, or is empty.
func menuArgs(m menuEntry, drop func(token string) bool) string {
	args := bootcfg.Without(m.Args, func(t string) bool {
		return drop(t) || strings.HasPrefix(t, "BOOT_IMAGE=") || strings.HasPrefix(t, "initrd=") || strings.HasPrefix(t, "ip=") || strings.HasPrefix(t, "console=")
	})
	if args == "" {
		return ""
	}
	return " " + args
}

// dropForInitramfsTools: the arguments of casper and live-boot that look for the ISO on a disk or name the way it is fetched (the network disk takes their place).
func dropForInitramfsTools(t string) bool {
	return hasAnyPrefix(t, "boot=", "netboot=", "nfsroot=", "url=", "fetch=", "findiso=", "iso-scan/", "live-media=", "live-media-path=", "components", "toram", "persistent", "persistence")
}

// dropForArchiso: the arguments of archiso that look for the ISO by its label or name the device or the server.
func dropForArchiso(t string) bool {
	return hasAnyPrefix(t, "archisobasedir=", "archisodevice=", "archisolabel=", "archisosearchuuid=", "archiso_", "copytoram")
}

// dropForDracut: the arguments of a dracut live system that say where the ISO is.
func dropForDracut(t string) bool {
	return hasAnyPrefix(t, "root=", "rd.live.image", "rd.live.check", "rd.neednet", "netroot=", "iso-scan/", "inst.stage2=", "inst.repo=", "rd.live.ram")
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// argValue is the value of a "name=value" argument, or the default.
func argValue(args, name, def string) string {
	for _, t := range strings.Fields(args) {
		if v, ok := strings.CutPrefix(t, name+"="); ok && v != "" {
			return v
		}
	}
	return def
}
