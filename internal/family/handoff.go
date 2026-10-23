// Systems that are not started by loading a kernel: Windows installation media (Windows' own set-up environment, loaded with wimboot) and FreeBSD (its own loader, which
// has to be started by the PC's firmware).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import "path"

// Windows installation media: Windows' own set-up environment is loaded with wimboot, and the installation files come from a share (see package winpe).
func windows(c *Context) (Boot, bool) {
	t, u := c.FS, c.URLs
	if !t.Exists("/sources/boot.wim") || !t.Exists("/boot/boot.sdi") || !t.Exists("/efi/boot/bootx64.efi") || !t.Exists("/efi/microsoft/boot/bcd") {
		return Boot{}, false
	}
	if !t.Exists("/sources/install.wim") && !t.Exists("/sources/install.esd") && !t.Exists("/sources/install.swm") {
		// A Windows PE with no installation image (Hiren's BootCD PE, a recovery or rescue disc): it starts its own programs, so Bootlace adds nothing to it.
		// With iSCSI the PC reads the ISO as a CD-ROM a piece at a time. Through wimboot the boot image (1.8 GB for Hiren's) would be held in memory twice
		// (wimboot's copy and Windows' RAM disk), which needs 6 GB or more.
		if u.has(ISCSI) {
			return Boot{Family: "Windows PE", Commands: []string{"sanboot --no-describe --drive 0x80 " + u.ISCSI}}, true
		}
		return Boot{Family: "Windows PE", Commands: []string{"chain " + u.Win + "/boot.ipxe"}}, true
	}
	return Boot{Family: "Windows setup", Commands: []string{"chain " + u.Win + "/boot.ipxe"}}, true
}

// FreeBSD and what is built on it (pfSense, OPNsense, TrueNAS CORE): FreeBSD's own loader is started from the ISO and then reads everything else, the kernel and the ISO itself
// as a memory disk, by TFTP from this server (see package httpd, which chooses the files by who asks).
func freeBSD(c *Context) (Boot, bool) {
	if !c.FS.Exists("/boot/kernel/kernel") || !c.FS.Exists("/boot/loader.efi") {
		return Boot{}, false
	}
	// FreeBSD's loader cannot use the network card while iPXE holds it, so iPXE says which PC chose this ISO and restarts it: the next network boot of that PC gets the loader
	// as boot file straight from the firmware (see package handoff).
	return Boot{Family: "FreeBSD, experimental", Commands: []string{
		// (the hand-off has no boot file for a BIOS PC, which would restart for ever. The menu adds "|| goto failed" to every line: on a UEFI PC iseq is false, and the
		// final echo makes the line succeed)
		"iseq ${platform} pcbios && echo FreeBSD-based ISOs (pfSense, OPNsense) start on UEFI PCs only, not in BIOS mode. && sleep 8 && goto again || echo",
		"echo Restarting this PC to start the FreeBSD loader from the firmware. It reads the kernel and the ISO by TFTP: this takes a while",
		"imgfetch --name handoff " + c.URLs.Base + "/next?iso=" + path.Base(c.URLs.FS) + "&mac=${mac}&ip=${ip}",
		"reboot",
	}}, true
}
