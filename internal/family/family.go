// Package family recognises which kind of Linux an ISO holds and says how to start it over the network: the kernel and the start-up disk come
// out of the ISO, and the kernel is told where the rest of the ISO is on the web server. The distributions' own network boot support does the work
// after that, so the ISO is never changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"path"
	"sort"
	"strconv"
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
	Win string // generated files for Windows set-up (see package winpe)
	// NBD says that the ISO can be offered to a live Linux as a network disk, with Extra the address of the extra initramfs archive that makes use of it; empty: the whole
	// ISO is downloaded into the PC's memory (a PC then needs RAM for all of it, twice over).
	NBD, Extra string
	// The network disk's address and the ISO's name on it, for the ones that attach it themselves (archiso has its own NBD client).
	NBDHost, NBDName string
	ISCSI            string // the ISO as a network CD-ROM (the address iPXE's sanboot takes), when the iSCSI server is on
	// The same network CD-ROM for a Linux that logs in to it itself (dracut's iSCSI support): server, port and the target's name. Empty when the iSCSI server is off.
	ISCSIHost, ISCSIIQN string
	ISCSIPort           int
	NBDPort             int
	Base                string // the server
}

// Boot is how to start one ISO.
type Boot struct {
	Family   string   // "Alpine", "Debian live" ...
	Commands []string // iPXE commands: kernel, initrd, boot

	// For a kernel that GRUB (signed by the distribution, so that UEFI Secure Boot can stay on) starts instead of iPXE: the files inside the ISO and the kernel's
	// arguments. Empty for the kinds that cannot be started that way.
	Kernel, Initrd, Args string
	ExtraInitrd          string // an address: a second initramfs archive to load after the ISO's own
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

var detectors = []func(Tree, URLs) (Boot, bool){windows, proxmox, alpine, archLinux, systemRescue, shredOS, openSUSE, fedoraLiveISO, debianInstaller, debianLive, ubuntu, freeBSD, fedoraLike}

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

// withExtra adds the extra initramfs archive (the network disk's helper and nfsmount stand-in) after the ISO's own.
func withExtra(b Boot, u URLs) Boot {
	b.ExtraInitrd = u.Extra
	b.Commands = []string{b.Commands[0], b.Commands[1], "initrd " + u.Extra, "boot"}
	return b
}

func boot(family string, u URLs, kernel, initrd, args string) Boot {
	return Boot{Family: family, Kernel: kernel, Initrd: initrd, Args: args, Commands: []string{
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
	return archiso("Arch Linux", u, "ip=:::::eth0:dhcp", "arch", dir+"/"+kernel, dir+"/"+initrd), true // (CachyOS wants the long form)
}

// archiso is how Arch Linux ISOs and the ones built with it (SystemRescue, CachyOS ...) are started. Their initramfs has an NBD client of its own (the archiso_pxe_nbd hook):
// the ISO is attached as /dev/nbd0 and the squashfs is read from it a piece at a time, with copytoram=n so that it is not copied into memory. That client takes the server's
// address only, so it is used when the NBD server listens on the standard port; otherwise the squashfs is downloaded into memory over HTTP.
func archiso(family string, u URLs, ip, basedir, kernel, initrd string) Boot {
	if u.NBDHost != "" && u.NBDPort == 10809 {
		return boot(family, u, kernel, initrd, ip+" archisobasedir="+basedir+" archiso_nbd_srv="+u.NBDHost+" archiso_nbd_name="+u.NBDName+" copytoram=n console=tty0")
	}
	return boot(family, u, kernel, initrd, ip+" archisobasedir="+basedir+" archiso_http_srv="+u.FS+"/ copytoram=y console=tty0")
}

// SystemRescue: archiso with its own folder name.
func systemRescue(t Tree, u URLs) (Boot, bool) {
	const dir = "/sysresccd/boot/x86_64"
	if !t.Exists(dir+"/vmlinuz") || !t.Exists(dir+"/sysresccd.img") {
		return Boot{}, false
	}
	return archiso("SystemRescue", u, "ip=dhcp", "sysresccd", dir+"/vmlinuz", dir+"/sysresccd.img"), true // (its archiso hook builds the long form itself from ip=dhcp)
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
	if u.NBD != "" && u.Extra != "" && strings.HasPrefix(kernel, "/live/") { // the usual layout: live-boot mounts the ISO through nfsmount, which Bootlace replaces (package initrd)
		b := boot("Debian live", u, kernel, initrd, "boot=live components netboot=nfs nfsroot="+u.NBD+" console=tty0")
		return withExtra(b, u), true
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
		"initrd --name /proxmox.iso " + u.ISO + " /proxmox.iso mode=0644",
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
	if u.NBD != "" && u.Extra != "" { // the ISO is read over the network a piece at a time: any amount of RAM will do (see package initrd)
		return withExtra(boot("Ubuntu (casper)", u, "/casper/vmlinuz", initrd, "ip=dhcp boot=casper netboot=nfs nfsroot="+u.NBD+" cloud-init=disabled console=tty0"), u), true
	}
	return boot("Ubuntu (casper)", u, "/casper/vmlinuz", initrd, "ip=dhcp url="+u.ISO+" boot=casper cloud-init=disabled console=tty0"), true
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

// Windows installation media: Windows' own set-up environment is loaded with wimboot, and the installation files come from a share (see package winpe).
func windows(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/sources/boot.wim") || !t.Exists("/boot/boot.sdi") || !t.Exists("/efi/boot/bootx64.efi") || !t.Exists("/efi/microsoft/boot/bcd") {
		return Boot{}, false
	}
	if !t.Exists("/sources/install.wim") && !t.Exists("/sources/install.esd") && !t.Exists("/sources/install.swm") {
		// a Windows PE with no installation image (Hiren's BootCD PE, a recovery or rescue disc): it starts its own programs, so Bootlace adds nothing to it
		// With iSCSI the PC reads the ISO as a CD-ROM a piece at a time. Through wimboot the boot image (1.8 GB for Hiren's) would be held in memory twice
		// (wimboot's copy and Windows' RAM disk), which needs 6 GB or more.
		if u.ISCSI != "" {
			return Boot{Family: "Windows PE", Commands: []string{"sanboot --no-describe --drive 0x80 " + u.ISCSI}}, true
		}
		return Boot{Family: "Windows PE", Commands: []string{"chain " + u.Win + "/boot.ipxe"}}, true
	}
	return Boot{Family: "Windows setup", Commands: []string{"chain " + u.Win + "/boot.ipxe"}}, true
}

// Fedora's live ISOs since version 44 (built with KIWI): kernel and initramfs in /boot/x86_64/loader, the system in /LiveOS/squashfs.img. The initramfs (dracut) has an iSCSI
// client: it logs in to the ISO as a network CD-ROM and reads the system from it a piece at a time, so the PC does not need RAM for the whole ISO.
func fedoraLiveISO(t Tree, u URLs) (Boot, bool) {
	const dir = "/boot/x86_64/loader"
	if !t.Exists(dir+"/linux") || !t.Exists(dir+"/initrd") || !t.Exists("/LiveOS/squashfs.img") || t.Exists("/.treeinfo") {
		return Boot{}, false
	}
	if u.ISCSIHost != "" {
		target := u.ISCSIHost + ":" + strconv.Itoa(u.ISCSIPort) + "-iscsi-" + u.ISCSIIQN + "-lun-0"
		return boot("Fedora (live)", u, dir+"/linux", dir+"/initrd", "ip=dhcp rd.neednet=1 netroot=iscsi:"+u.ISCSIHost+"::"+strconv.Itoa(u.ISCSIPort)+"::"+u.ISCSIIQN+
			" root=live:/dev/disk/by-path/ip-"+target+" rd.live.image console=tty0"), true
	}
	return boot("Fedora (live)", u, dir+"/linux", dir+"/initrd", "ip=dhcp root=live:"+u.ISO+" rd.live.image console=tty0"), true
}

// ShredOS (nwipe, for wiping drives): one kernel with everything inside it.
func shredOS(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/boot/bzImage") || !t.Exists("/boot/shredos.id") {
		return Boot{}, false
	}
	return Boot{Family: "ShredOS", Kernel: "/boot/bzImage", Args: "console=tty3 loglevel=3", Commands: []string{"kernel " + u.FS + "/boot/bzImage console=tty3 loglevel=3", "boot"}}, true
}

// openSUSE (Leap, Tumbleweed): the installer reads the packages from the web server, file by file, when the ISO has them (the DVD); the network ISO asks the person.
func openSUSE(t Tree, u URLs) (Boot, bool) {
	const dir = "/boot/x86_64/loader"
	if !t.Exists(dir+"/linux") || !t.Exists(dir+"/initrd") || !t.Exists("/.treeinfo") {
		return Boot{}, false
	}
	args := "netsetup=dhcp"
	if t.Exists("/repodata") {
		args += " install=" + u.FS + "/"
	}
	return boot("openSUSE", u, dir+"/linux", dir+"/initrd", args), true
}

// Debian's installer and the ones built on it (Kali's): kernel and initrd come out of the ISO, the packages from a mirror or from the ISO's own files over the web server.
func debianInstaller(t Tree, u URLs) (Boot, bool) {
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
	return boot("Debian installer", u, "/install.amd/vmlinuz", initrd, "vga=788 --- quiet"), true
}

// FreeBSD and what is built on it (pfSense, OPNsense, TrueNAS CORE): FreeBSD's own loader is started from the ISO and then reads everything else,
// the kernel and the ISO itself as a memory disk, by TFTP from this server (see package httpd, which chooses the files by who asks).
func freeBSD(t Tree, u URLs) (Boot, bool) {
	if !t.Exists("/boot/kernel/kernel") || !t.Exists("/boot/loader.efi") {
		return Boot{}, false
	}
	// FreeBSD's loader cannot use the network card while iPXE holds it, so iPXE says which PC chose this ISO and restarts it: the next network boot
	// of that PC gets the loader as boot file straight from the firmware (see package handoff).
	return Boot{Family: "FreeBSD, experimental", Commands: []string{
		// (the hand-off has no boot file for a BIOS PC, which would restart for ever)
		"iseq ${platform} pcbios && echo FreeBSD-based ISOs (pfSense, OPNsense) start on UEFI PCs only, not in BIOS mode. && sleep 8 && exit",
		"echo Restarting this PC to start the FreeBSD loader from the firmware. It reads the kernel and the ISO by TFTP: this takes a while",
		"imgfetch --name handoff " + u.Base + "/next?iso=" + path.Base(u.FS) + "&mac=${net0/mac}&ip=${net0/ip}",
		"reboot",
	}}, true
}
