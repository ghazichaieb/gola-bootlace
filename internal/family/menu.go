// The boot menu of an ISO as the files of its volumes give it (package bootcfg parses the text): which kernels it starts, with which start-up disk, and where those files are.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"io"
	"path"
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/bootcfg"
)

// menuEntry is a choice of the ISO's boot menu whose kernel and start-up disk are in the files of a volume.
type menuEntry struct {
	bootcfg.Entry
	vol    Volume
	kernel string
	initrd string // the start-up disk proper (a microcode file in front of it is left out)
}

// maxMenuFile is how much of a menu file is read.
const maxMenuFile = 1 << 20

// readText reads a small file of a tree.
func readText(t Tree, name string) (string, bool) {
	f, err := t.Open(name)
	if err != nil {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(f.Reader(), maxMenuFile))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// menuEntries are the choices of the ISO's own boot menus (isolinux, GRUB, systemd-boot) in the ISO's files and its other volumes, in the order the menus give them, each once.
func (c *Context) menuEntries() []menuEntry {
	vols := []Volume{{c.FS, c.URLs.FS}}
	if c.Volumes != nil {
		vols = append(vols, c.Volumes()...)
	}
	var out []menuEntry
	seen := map[string]bool{}
	for _, vol := range vols {
		for _, e := range menuOf(vol.FS) {
			kernel, initrd := e.Kernel, startUpDisk(vol.FS, e)
			if kernel == "" || initrd == "" || !vol.FS.Exists(kernel) || seen[vol.URL+kernel+initrd] {
				continue
			}
			seen[vol.URL+kernel+initrd] = true
			out = append(out, menuEntry{Entry: e, vol: vol, kernel: kernel, initrd: initrd})
		}
	}
	// the entries that are the ordinary way to start the system come first; the special ones (safe graphics, a check of the disc ...) are only taken when nothing else is
	sort.SliceStable(out, func(i, j int) bool { return !special(out[i].Entry) && special(out[j].Entry) })
	return out
}

// special says whether a menu entry is a special way to start the system rather than the ordinary one.
func special(e bootcfg.Entry) bool {
	s := " " + strings.ToLower(e.Title+" "+e.Args) + " "
	for _, w := range []string{"safe", "nomodeset", "check", "test", "rescue", "recovery", "memtest", "debug", "verbose", "single", " text ", "emergency"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// menuOf reads every boot menu that a tree has.
func menuOf(t Tree) []bootcfg.Entry {
	var out []bootcfg.Entry
	for _, file := range bootcfg.Files {
		if text, ok := readText(t, file); ok {
			out = append(out, bootcfg.Parse(file, text, includer(t, file))...)
		}
	}
	if names, err := t.List("/EFI"); err == nil { // a boot loader of its own in /EFI/<name> (Debian's, Ubuntu's, Fedora's)
		for _, n := range names {
			if dir, ok := strings.CutSuffix(n, "/"); ok {
				file := "/EFI/" + dir + "/grub.cfg"
				if text, ok := readText(t, file); ok {
					out = append(out, bootcfg.Parse(file, text, includer(t, file))...)
				}
			}
		}
	}
	if names, err := t.List(bootcfg.EntriesFolder); err == nil {
		sort.Strings(names)
		for _, n := range names {
			if strings.HasSuffix(n, ".conf") {
				if text, ok := readText(t, bootcfg.EntriesFolder+n); ok {
					out = append(out, bootcfg.Parse(bootcfg.EntriesFolder+n, text, nil)...)
				}
			}
		}
	}
	return out
}

// includer finds a menu file that another includes: by its path, or next to the one that includes it.
func includer(t Tree, from string) bootcfg.Include {
	return func(name string) (string, bool) {
		if text, ok := readText(t, path.Join(path.Dir(from), name)); ok {
			return text, true
		}
		return readText(t, "/"+strings.TrimPrefix(name, "/"))
	}
}

// startUpDisk is the last start-up disk of the entry that exists, leaving out the microcode files that menus load first.
func startUpDisk(t Tree, e bootcfg.Entry) string {
	for i := len(e.Initrds) - 1; i >= 0; i-- {
		if in := e.Initrds[i]; !strings.Contains(strings.ToLower(in), "ucode") && t.Exists(in) {
			return in
		}
	}
	return ""
}
