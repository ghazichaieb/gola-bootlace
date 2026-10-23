// Package bootcfg reads the boot menu of an ISO: the kernel, the start-up disk and the kernel's arguments that its own isolinux, GRUB or systemd-boot menu start it with. For
// an ISO that is not known by name this says where its kernel is and what the distribution itself passes to it; the arguments tell what the start-up disk is made for.
// The parsers take text, not files, so they work on whatever tree the ISO is read from.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package bootcfg

import (
	"path"
	"strings"
)

// Entry is one choice of a boot menu that starts a Linux kernel.
type Entry struct {
	Title   string
	Kernel  string   // the path in the ISO, with a leading slash
	Initrds []string // the start-up disk (more than one for a microcode file in front)
	Args    string   // the kernel's arguments, with the variables the menu could resolve put in
	Source  string   // the menu file the entry is from
}

// Files are the places a menu is looked for, best first. A folder "/loader/entries/" holds one file per entry.
var Files = []string{
	"/isolinux/isolinux.cfg", "/boot/isolinux/isolinux.cfg", "/syslinux/syslinux.cfg", "/boot/syslinux/syslinux.cfg", "/boot/syslinux/porteus.cfg",
	"/boot/grub/grub.cfg", "/boot/grub2/grub.cfg", "/EFI/BOOT/grub.cfg", "/EFI/boot/grub.cfg", "/grub/grub.cfg",
}

// EntriesFolder is where systemd-boot keeps its entries.
const EntriesFolder = "/loader/entries/"

// Include gives the text of a menu file that another one includes (syslinux's INCLUDE, GRUB's source), or false.
type Include func(name string) (string, bool)

// Parse reads the menu in text. The kind of menu is told by the file's name: ".conf" is a systemd-boot entry, "grub.cfg" or "*.cfg" under a grub folder is GRUB's, anything
// else syslinux's. Entries that start no kernel (a menu, a boot loader module, a chain to another disk) are left out.
func Parse(file, text string, inc Include) []Entry {
	var es []Entry
	switch {
	case strings.HasSuffix(file, ".conf"):
		es = parseLoaderEntry(text)
	case strings.Contains(file, "grub"):
		es = parseGrub(text, inc, 0, map[string]string{})
	default:
		es = parseSyslinux(text, inc, 0)
	}
	var out []Entry
	for _, e := range es {
		e.Source = file
		e.Kernel = clean(e.Kernel)
		bad := hasControl(e.Kernel)
		for i, in := range e.Initrds {
			e.Initrds[i] = clean(in)
			bad = bad || hasControl(e.Initrds[i])
		}
		// A path with a control character (a carriage return inside a menu line) could end up in a line of an iPXE script: such an entry is not offered. The arguments lose them.
		e.Args = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, e.Args)
		if !bad && isKernel(e.Kernel) {
			out = append(out, e)
		}
	}
	return out
}

// hasControl says whether a path holds a control character.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// isKernel is false for the loader modules and chain files that menus also start.
func isKernel(p string) bool {
	if p == "" || strings.Contains(p, "$") {
		return false
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".c32", ".0", ".bin", ".com", ".sys":
		return false
	}
	return true
}

// clean makes a path of a menu into a path in the ISO: no device in front ("(hd0,1)", "($root)"), no doubled slashes, one slash at the start.
func clean(p string) string {
	if strings.HasPrefix(p, "(") {
		if i := strings.Index(p, ")"); i >= 0 {
			p = p[i+1:]
		}
	}
	if p == "" {
		return ""
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// splitKey cuts a line at its first space or tab: the keyword, and the rest.
func splitKey(line string) (key, rest string) {
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return line[:i], strings.TrimSpace(line[i+1:])
	}
	return line, ""
}

// Tokens splits the arguments at spaces.
func Tokens(args string) []string { return strings.Fields(args) }

// Without is the arguments with the tokens for which drop is true taken out. A token that still has a variable in it that the menu could not resolve goes too.
func Without(args string, drop func(token string) bool) string {
	var keep []string
	for _, t := range Tokens(args) {
		if strings.Contains(t, "$") || drop(t) {
			continue
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, " ")
}
