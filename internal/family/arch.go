// Arch Linux and what is built with archiso (CachyOS, SystemRescue, ChimeraOS ...). The ISO's start-up disk (made by mkinitcpio) usually has hooks for a start over the
// network: archiso_pxe_nbd attaches the ISO as /dev/nbd0 and reads the system image from it. Where the kernel is in the ISO's own files that is assumed; where it sits in the
// EFI image inside the ISO (ChimeraOS) the start-up disk is the one for a disc, and the extra archive adds a hook of its own (see hookExtra).
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

// archDir is where archiso keeps the boot files.
const archDir = "/arch/boot/x86_64"

// Arch Linux and its relatives: the boot files and the squashfs come from the web server. The kernel is the one named vmlinuz-linux or, in a relative,
// vmlinuz-linux-<name> (the long-term-support one only when it is the only one).
func archLinux(c *Context) (Boot, bool) {
	vol := Volume{c.FS, c.URLs.FS}
	if !c.FS.Exists(archDir) {
		// ChimeraOS keeps its kernel only in the EFI image inside the ISO: the system image in the ISO says it is an archiso
		var ok bool
		if vol, ok = c.volumeWith(archDir); !ok || !c.FS.Exists("/arch/x86_64/airootfs.sfs") {
			return Boot{}, false
		}
	}
	names, err := vol.FS.List(archDir)
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
	if !vol.FS.Exists(archDir + "/" + initrd) {
		return Boot{}, false
	}
	f := files{vol.URL, archDir + "/" + kernel, archDir + "/" + initrd}
	if vol.URL != c.URLs.FS { // the kernel is in the EFI image: the start-up disk is the one for a disc
		return c.archisoHook("Arch Linux (kernel in the EFI image)", vol, f, "arch", "/arch/x86_64/airootfs.sfs", "")
	}
	return c.archiso("Arch Linux", f, "ip=:::::eth0:dhcp", "arch", ""), true // (CachyOS wants the long form)
}

// SystemRescue: archiso with its own folder name.
func systemRescue(c *Context) (Boot, bool) {
	const dir = "/sysresccd/boot/x86_64"
	if !c.FS.Exists(dir+"/vmlinuz") || !c.FS.Exists(dir+"/sysresccd.img") {
		return Boot{}, false
	}
	return c.archiso("SystemRescue", c.in(dir+"/vmlinuz", dir+"/sysresccd.img"), "ip=dhcp", "sysresccd", ""), true // (its archiso hook builds the long form itself from ip=dhcp)
}

// archiso is how the ISOs built with archiso are started when their start-up disk has the hooks for the network. The network disk client of the hook takes the server's
// address only, so the network disk is used when the server listens on the standard port; otherwise the system image is downloaded over HTTP.
func (c *Context) archiso(family string, f files, ip, basedir, args string) Boot {
	u := c.URLs
	if u.has(NBD) && u.NBDPort == 10809 {
		// (the extra archive replaces the start-up disk's nbd-client by one that connects again when the connection is lost: the live system changes its address)
		return newBoot(family, f, ip+" archisobasedir="+basedir+" archiso_nbd_srv="+u.NBDHost+" archiso_nbd_name="+u.NBDName+" copytoram=n"+" console=tty0"+args).withExtra(u, nil)
	}
	return newBoot(family, f, ip+" archisobasedir="+basedir+" archiso_http_srv="+u.FS+"/ copytoram=y"+" console=tty0"+args)
}

// archisoHook is for an ISO whose start-up disk has no hook for the network: the extra archive makes one, and the arguments name the device it attaches. basedir is the
// folder of the live system (arch); image is the system image that has the kernel's modules, or empty to look for it. args are the system's own arguments that go with these.
func (c *Context) archisoHook(family string, vol Volume, f files, basedir, image, args string) (Boot, bool) {
	if !c.URLs.has(NBD) {
		return Boot{}, false
	}
	b := newBoot(family, f, "archisobasedir="+basedir+" archisodevice=/dev/nbd0 copytoram=n"+" console=tty0"+args)
	return b.withExtra(c.URLs, func(env ExtraEnv) ([]byte, error) { return c.hookExtra(vol, f.initrd, image, env) }), true
}

// mkinitcpioHook is the hook that goes in front of archiso's own. mkinitcpio only runs hooks that are executable.
func mkinitcpioHook(net initrd.NetworkScript) string {
	return "#!/usr/bin/ash\nrun_hook() {\n" + net.String() + "}\n"
}

// hookExtra is the archive with the hook, the drivers it loads and the DHCP script; the initramfs's list of hooks (/config) is changed to run it after udev starts.
func (c *Context) hookExtra(vol Volume, initramfsPath, image string, env ExtraEnv) ([]byte, error) {
	f, err := vol.FS.Open(initramfsPath)
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader(), "config")
	if err != nil {
		return nil, fmt.Errorf("the start-up disk: %w", err)
	}
	config := string(in.Files["config"])
	if !strings.Contains(config, "HOOKS=\"") || !strings.Contains(config, " archiso ") {
		return nil, errors.New("the start-up disk has a form that is not known here")
	}
	mods, err := c.nicDrivers(image, in.Version)
	if err != nil {
		return nil, err
	}
	s := env.Settings
	var b initrd.Builder
	b.File("config", 0o644, []byte(strings.Replace(config, " archiso ", " bootlace archiso ", 1)))
	b.Script("hooks/bootlace", mkinitcpioHook(initrd.NetworkScript{Helper: "/usr/bin/bootlace-nbd", Dir: "/bootlace", Host: s.Host, Port: s.Port, Name: s.Name, Block: s.Block, Say: "msg", Fail: "err"}))
	b.Dir("bootlace")
	b.Script("bootlace/udhcpc.sh", initrd.UdhcpcScript)
	b.Modules("bootlace/modules", mods)
	b.File("usr/bin/bootlace-nbd", 0o755, env.Helper)
	return b.Bytes(), nil
}
