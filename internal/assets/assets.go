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

//go:embed ipxe/*
var files embed.FS

// IPXE is the folder TFTP serves: ipxe.efi (UEFI, 64 bit), snponly.efi (UEFI, uses the firmware's network driver) and undionly.kpxe (BIOS).
func IPXE() fs.FS {
	sub, err := fs.Sub(files, "ipxe")
	if err != nil {
		panic(err) // the folder is part of the program: this cannot happen
	}
	return sub
}
