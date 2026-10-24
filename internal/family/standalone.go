// Systems that need nothing from the network side: the kernel and the start-up disk are fetched and the system carries on by itself, or the ISO is handed over whole. And
// the three that are started in a way of their own: LibreELEC, Porteus and Proxmox VE.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"fmt"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// Tiny Core (Core, without the graphical parts): a 20 MB kernel and a small initramfs that holds the whole system, so nothing is read from the ISO afterwards.
func tinyCore(c *Context) (Boot, bool) {
	if !c.FS.Exists("/boot/vmlinuz") || !c.FS.Exists("/boot/core.gz") {
		return Boot{}, false
	}
	return newBoot("Tiny Core", c.in("/boot/vmlinuz", "/boot/core.gz"), "loglevel=3"), true
}

// ShredOS (nwipe, for wiping drives): one kernel with everything inside it.
func shredOS(c *Context) (Boot, bool) {
	if !c.FS.Exists("/boot/bzImage") || !c.FS.Exists("/boot/shredos.id") {
		return Boot{}, false
	}
	return newBoot("ShredOS", c.in("/boot/bzImage", ""), "console=tty3 loglevel=3"), true
}

// LibreELEC and the systems built from it (Lakka) come as a disk image: a boot partition with a kernel (KERNEL, its initramfs inside) and the system as a file (SYSTEM, a
// squashfs). The initramfs mounts the partition named by "boot=", and "boot=NBD=host:port" makes it read the partition over the network; without "disk=" its storage is in
// memory. It has no nbd-client program, and its shell would take the name for BusyBox's own applet (an old client that knows no export names) even if a file of that name
// were on the path: an rdinit script changes that one call to name the helper, which reads the export's name from a list in the archive. Only the network disk works (the
// kernel cannot see a disk that the firmware reads over iSCSI).
func libreELEC(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/KERNEL") || !t.Exists("/SYSTEM") || t.Exists("/live") || !u.has(NBD) {
		return Boot{}, false
	}
	args := "boot=NBD=" + u.NBDHost + ":" + u.nbdPort() + " ip=dhcp rdinit=/bootlace-init quiet"
	return newBoot("Lakka / LibreELEC", c.in("/KERNEL", ""), args).withExtra(u, libreELECExtra), true // (no initramfs of its own: the extra archive is the only one)
}

// libreELECExtra is the archive: the helper under the name of the client, the list that says which export is /dev/nbd0 (the boot partition), and the rdinit script.
func libreELECExtra(env ExtraEnv) ([]byte, error) {
	var b initrd.Builder
	b.Script("bootlace-init", initrd.RdinitWrapper("/usr/bin/busybox sed -i 's#nbd-client $NBD_SERVER#/usr/bin/nbd-client $NBD_SERVER#' /init"))
	b.Dir("etc")
	b.File("etc/bootlace-nbd", 0o644, []byte(fmt.Sprintf("nbd0 %s %d\n", env.Settings.Name, env.Settings.BlockSize())))
	b.Dir("usr")
	b.Dir("usr/bin")
	b.File("usr/bin/nbd-client", 0o755, env.Helper)
	return b.Bytes(), nil
}

// Porteus (and Slax, built the same way) is a Slackware live system whose start-up script finds its files on a disk, over NFS (a share of its own layout) or in an ISO file
// that "from=" names. The ISO is handed over as a file in the initramfs, in a folder under /mnt where the script looks for disks: the PC needs memory for the ISO, twice over
// (the script copies the initramfs once), but nothing else. The "always fresh" options are used: the ISO cannot hold changes.
func porteus(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/boot/syslinux/vmlinuz") || !t.Exists("/boot/syslinux/initrd.xz") || !t.Exists("/porteus/base/001-core.xzm") || u.Extra == "" {
		return Boot{}, false
	}
	b := newBoot("Porteus (whole ISO in memory)", c.in("/boot/syslinux/vmlinuz", "/boot/syslinux/initrd.xz"), "from=/porteus.iso nomagic base_only norootcopy")
	b.Commands = append([]string{"echo Loading Porteus ... the whole ISO is read into memory first, this takes a while"}, b.Commands...)
	// (the extra archive makes the folder: iPXE does not, and a file in a folder that is not there is dropped without a word)
	b = b.withExtra(u, func(ExtraEnv) ([]byte, error) {
		var x initrd.Builder
		x.Dir("mnt")
		x.Dir("mnt/bootlace")
		return x.Bytes(), nil
	})
	return b.before("initrd --name /mnt/bootlace/porteus.iso " + u.ISO + " /mnt/bootlace/porteus.iso mode=0644"), true
}

// Proxmox VE's installer finds the ISO on a local drive or as a file in its own start-up disk. With the network disk the start-up script is given a few lines that bring up the
// network, attach the ISO as /dev/nbd0 and let the script's own search for the ISO find it there (and the drivers for the network cards, which the start-up disk lacks, are
// brought along). Without it the ISO is given to the kernel as a second initramfs file, which needs RAM for the whole ISO twice over.
func proxmox(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/.pve-cd-id.txt") || !t.Exists("/boot/linux26") || !t.Exists("/boot/initrd.img") {
		return Boot{}, false
	}
	const args = "rw quiet splash=silent console=tty0"
	f := c.in("/boot/linux26", "/boot/initrd.img")
	if u.has(NBD) && t.Exists("/pve-installer.squashfs") {
		return newBoot("Proxmox VE", f, "ro "+args).withVerbose("proxdebug").withExtra(u, c.proxmoxExtra), true
	}
	b := newBoot("Proxmox VE", f, "ro ramdisk_size=16777216 "+args).withVerbose("proxdebug")
	return b.before("initrd --name /proxmox.iso " + u.ISO + " /proxmox.iso mode=0644"), true
}

// proxmoxExtra is the archive: the installer's start-up script with the network lines added, the helper, the drivers and the DHCP script.
func (c *Context) proxmoxExtra(env ExtraEnv) ([]byte, error) {
	f, err := c.FS.Open("/boot/initrd.img")
	if err != nil {
		return nil, err
	}
	in, err := initread.Read(f.Reader(), "init")
	if err != nil {
		return nil, fmt.Errorf("the installer's start-up disk: %w", err)
	}
	patched, ok := patchProxmoxInit(string(in.Files["init"]), env.Settings)
	if !ok {
		return nil, fmt.Errorf("the installer's start-up script has a form that is not known here")
	}
	mods, err := c.nicDrivers("/pve-installer.squashfs", in.Version)
	if err != nil {
		return nil, err
	}
	var b initrd.Builder
	b.Script("init", patched)
	b.Dir("bootlace")
	b.File("bootlace/bootlace-nbd", 0o755, env.Helper)
	b.Script("bootlace/udhcpc.sh", initrd.UdhcpcScript)
	b.Modules("bootlace/modules", mods)
	return b.Bytes(), nil
}

// patchProxmoxInit adds the network lines to the installer's start-up script, before the script looks for the ISO; false when the script does not look as expected (the
// old way, the whole ISO inside the initramfs, is then still there to use).
func patchProxmoxInit(script string, s initrd.Settings) (string, bool) {
	const anchor = "# we have no iscsi daemon"
	const scan = "/sys/block/nvme*; do"
	if strings.Count(script, anchor) != 1 || strings.Count(script, scan) != 1 {
		return "", false
	}
	net := initrd.NetworkScript{Helper: "/bootlace/bootlace-nbd", Dir: "/bootlace", Host: s.Host, Port: s.Port, Name: s.Name, Block: s.Block, Say: "echo", Fail: "echo"}
	// (everything the lines need is in the archive; without the network the script goes on as before and does not find an ISO)
	block := "# Gola Bootlace: the ISO is read over the network a piece at a time (a network block disk), so the PC does not need memory for all of it.\n" +
		"if [ -x /bootlace/bootlace-nbd ]; then\n" + net.String() + "fi\n\n"
	script = strings.Replace(script, anchor, block+anchor, 1)
	return strings.Replace(script, scan, "/sys/block/nvme* /sys/block/nbd0; do", 1), true
}
