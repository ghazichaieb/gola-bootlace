// Package assets holds the boot loaders Bootlace hands to PCs: iPXE builds, embedded so that one file is the whole server.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
//
// iPXE (https://ipxe.org) is licensed GPL-2.0-or-later with additional terms, and the files come unchanged from https://boot.ipxe.org
// (see NOTICE for the date and the checksums). Each name is the file a PC asks for over TFTP.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed ipxe/* ubuntu/* helper/*
var files embed.FS

// IPXE is the folder TFTP serves: ipxe.efi (UEFI, 64 bit), snponly.efi (UEFI, uses the firmware's network driver) and undionly.kpxe (BIOS).
func IPXE() fs.FS {
	sub, err := fs.Sub(files, "ipxe")
	if err != nil {
		panic(err) // the folder is part of the program: this cannot happen
	}
	return sub
}

// UbuntuGrub is Ubuntu's signed network-boot GRUB (grubnetx64.efi.signed from the noble-updates package grub-efi-amd64-signed 1.202.5+2.12-1ubuntu7.3, unchanged). The GRUB on an Ubuntu ISO is a media-only build that
// cannot read files over the network when Secure Boot is on, so the shim from the ISO starts this one instead: Canonical signed it, and the shim trusts Canonical.
func UbuntuGrub() []byte {
	b, err := files.ReadFile("ubuntu/grubnetx64.efi.signed")
	if err != nil {
		panic(err) // the file is part of the program: this cannot happen
	}
	return b
}

// NBDHelper is bootlace-nbd (cmd/bootlace-nbd) built for Linux on x86-64 with no dependencies: it runs in a PC's initramfs and attaches the ISO as a network block
// disk. Built by tools/build_helper.sh; it is part of this program's source, so there is nothing to ship beside it.
func NBDHelper() []byte {
	b, err := files.ReadFile("helper/bootlace-nbd-linux-amd64")
	if err != nil {
		panic(err) // the file is part of the program: this cannot happen
	}
	return b
}
