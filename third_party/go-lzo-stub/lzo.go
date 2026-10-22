// Package lzo stands in for github.com/rasky/go-lzo, a GPL-2.0 port of LZO that the squashfs reader would otherwise pull in: Bootlace does not read LZO-compressed
// squashfs files (Proxmox VE and the other images it reads use zstd, xz or gzip), and this keeps that code out of the program. Reading an LZO file gives an error.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package lzo

import (
	"errors"
	"io"
)

// Decompress1X is the one function the squashfs reader calls; it always fails here.
func Decompress1X(r io.Reader, inLen int, outLen int) ([]byte, error) {
	return nil, errors.New("LZO-compressed squashfs files are not supported")
}
