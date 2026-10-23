// Package initrd makes the extra initramfs archive that Bootlace appends to a live Linux's own: a few files that overwrite the ones of the initramfs of the same name. The
// kernel unpacks every archive of the initrd in turn, and later files replace earlier ones, so the ISO's initramfs is used as it is except for those files.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initrd

import (
	"bytes"
	"fmt"
	"path"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// File is one entry of an archive.
type File struct {
	Name string // without a leading slash
	Mode uint32 // permission bits; directories get 040000 added
	Data []byte
	Dir  bool
}

// Builder collects the files of an archive. A folder is only made when asked for: where the initramfs has a link (Fedora's /usr/sbin points to bin), a folder entry would
// replace it with an empty folder. A file whose folder does not exist is dropped by the kernel without a word, so a folder is made before the files in it.
type Builder struct {
	files []File
	dirs  map[string]bool
}

// Dir adds a folder (once).
func (b *Builder) Dir(name string) {
	if b.dirs == nil {
		b.dirs = map[string]bool{}
	}
	if !b.dirs[name] {
		b.dirs[name] = true
		b.files = append(b.files, File{Name: name, Mode: 0o755, Dir: true})
	}
}

// File adds a file.
func (b *Builder) File(name string, mode uint32, data []byte) {
	b.files = append(b.files, File{Name: name, Mode: mode, Data: data})
}

// Script adds an executable text file.
func (b *Builder) Script(name, text string) { b.File(name, 0o755, []byte(text)) }

// Modules adds kernel modules as plain files in a folder, numbered in the order they must be loaded (a driver's own needs come first): the helper's "insmod" is given
// dir/* and loads them in that order.
func (b *Builder) Modules(dir string, mods []initread.Module) {
	b.Dir(dir)
	for i, m := range mods {
		b.File(fmt.Sprintf("%s/%02d-%s.ko", dir, i, initread.ModuleName(path.Base(m.Path))), 0o644, m.Data)
	}
}

// Bytes is the archive.
func (b *Builder) Bytes() []byte { return Archive(b.files) }

// Archive writes the files in the "newc" cpio format the kernel reads. Two archives one after the other are one initramfs to the kernel.
func Archive(files []File) []byte {
	var out bytes.Buffer
	ino := 1
	put := func(name string, mode uint32, data []byte) {
		fmt.Fprintf(&out, "070701%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X", ino, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, len(name)+1, 0)
		out.WriteString(name)
		out.WriteByte(0)
		for out.Len()%4 != 0 {
			out.WriteByte(0)
		}
		out.Write(data)
		for out.Len()%4 != 0 {
			out.WriteByte(0)
		}
		ino++
	}
	for _, f := range files {
		if f.Dir {
			put(f.Name, 0o040000|f.Mode, nil)
		} else {
			put(f.Name, 0o100000|f.Mode, f.Data)
		}
	}
	put("TRAILER!!!", 0, nil)
	return out.Bytes()
}
