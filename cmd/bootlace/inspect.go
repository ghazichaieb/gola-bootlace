// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/iso9660"
)

// inspect opens each ISO named on the command line and says what the server finds: the top level of its file system and the way it would be started.
func inspect(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: bootlace inspect [-ls /folder]... <file.iso>...")
		return 2
	}
	code := 0
	var also []string // folders to list as well: bootlace inspect -ls /live -ls /boot file.iso
	for len(args) >= 2 && args[0] == "-ls" {
		also, args = append(also, args[1]), args[2:]
	}
	for _, name := range args {
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			code = 1
			continue
		}
		info, _ := f.Stat()
		fmt.Fprintf(stdout, "%s (%.2f GB)\n", name, float64(info.Size())/(1<<30))
		img, err := iso9660.Open(f, info.Size())
		if err != nil {
			fmt.Fprintf(stdout, "  no ISO 9660 file system: %v\n", err)
			f.Close()
			continue
		}
		if top, err := img.List("/"); err == nil {
			fmt.Fprintf(stdout, "  top level: %s\n", strings.Join(top, " "))
		}
		for _, dir := range also {
			if list, err := img.List(dir); err == nil {
				fmt.Fprintf(stdout, "  %s: %s\n", dir, strings.Join(list, " "))
			} else {
				fmt.Fprintf(stdout, "  %s: %v\n", dir, err)
			}
		}
		if b, ok := family.Detect(img, family.URLs{ISO: "http://SERVER/iso/NAME", FS: "http://SERVER/fs/ID"}); ok {
			fmt.Fprintf(stdout, "  starts as: %s\n", b.Family)
			for _, c := range b.Commands {
				fmt.Fprintf(stdout, "    %s\n", c)
			}
		} else {
			fmt.Fprintln(stdout, "  not a Linux this server knows: it would be handed to the PC as a disk")
		}
		f.Close()
	}
	return code
}
