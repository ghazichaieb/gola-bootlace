// What a start-up disk (initramfs) is made of. The systems that Bootlace knows by name are started the way their start-up disk needs, and a start-up disk of the same make
// needs the same: casper (initramfs-tools, Ubuntu), live-boot (initramfs-tools, Debian), archiso (mkinitcpio), dracut. Looking into the start-up disk tells which one an ISO
// that is not known by name has, and what it already has of the network side.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import "github.com/ghazichaieb/gola-bootlace/internal/initread"

// flavor is the make of a start-up disk.
type flavor string

const (
	flavorUnknown  flavor = ""
	flavorCasper   flavor = "casper"    // initramfs-tools with Ubuntu's casper scripts
	flavorLiveBoot flavor = "live-boot" // initramfs-tools with Debian's live-boot scripts
	flavorArchiso  flavor = "archiso"   // mkinitcpio with archiso's hooks
	flavorDracut   flavor = "dracut"    // dracut with its live system module (dmsquash-live)
)

// facts are what is known of a start-up disk.
type facts struct {
	flavor  flavor
	version string // the kernel version of its modules

	nicDrivers bool // it carries network card drivers
	nbdDriver  bool // it carries the kernel's nbd driver (the network block disk)
	archisoNet bool // archiso: it has the hooks for a start over the network
	netroot    bool // dracut: it has the network root handler (what netroot=nbd: is passed to)
	iscsi      bool // dracut: it has an iSCSI client
	livenet    bool // dracut: it can fetch a live image over HTTP
}

// identify says what a start-up disk is, from the list of what is in it.
func identify(l *initread.Listing) facts {
	f := facts{version: l.Version}
	nics := 0
	for _, n := range initread.NICModules {
		if l.Have[n] {
			nics++
		}
	}
	f.nicDrivers = nics >= 3
	f.nbdDriver = l.Have["nbd"]
	switch {
	case l.Has("scripts/casper"):
		f.flavor = flavorCasper
	case l.Has("scripts/live"):
		f.flavor = flavorLiveBoot
	case l.Has("hooks/archiso"):
		f.flavor = flavorArchiso
		f.archisoNet = l.Has("hooks/archiso_pxe_nbd")
	case l.HasSuffix("/dmsquash-live-root") || l.HasSuffix("/dmsquash-live-root.sh"):
		f.flavor = flavorDracut
		f.netroot = l.HasSuffix("/netroot")
		f.iscsi = l.HasSuffix("/iscsiroot") || l.HasSuffix("/iscsistart")
		f.livenet = l.HasSuffix("/livenetroot")
	}
	return f
}
