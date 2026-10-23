// Package initread takes an initramfs apart as far as is needed to add to it or to tell what it is made of, and collects network card drivers out of the system image of a
// live ISO. Some live systems (Proxmox's installer, Tails) start from an initramfs made for a USB stick or a disc: it has no network card drivers at all, which a start over the
// network needs. The full set of kernel modules is in the system image (a squashfs), so the drivers worth bringing along are read from there and put in an extra initramfs
// archive (see package initrd).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initread

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Stop is what a function given to Walk returns to end the walk early without an error.
var Stop = errors.New("stop")

// Walk goes through an initramfs: one or more cpio archives, the first often only microcode and the last one compressed (gzip, xz or zstd). fn is called with the name of
// every entry (without a leading "./") and a reader for its contents, which must be read before fn returns.
func Walk(r io.Reader, fn func(name string, size int64, body io.Reader) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		// skip the padding between archives
		for {
			b, err := br.Peek(1)
			if err != nil {
				return nil // the end
			}
			if b[0] != 0 {
				break
			}
			_, _ = br.ReadByte()
		}
		peek, err := br.Peek(6)
		if err != nil {
			return nil
		}
		magic := append([]byte(nil), peek...) // (Peek's bytes are overwritten by the next read)
		var cr io.Reader = br
		compressed := true
		switch {
		case string(magic) == "070701":
			compressed = false
		case magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
			zr, err := zstd.NewReader(br)
			if err != nil {
				return err
			}
			defer zr.Close()
			cr = zr
		case magic[0] == 0x1f && magic[1] == 0x8b:
			zr, err := gzip.NewReader(br)
			if err != nil {
				return err
			}
			cr = zr
		case magic[0] == 0xfd && string(magic[1:6]) == "7zXZ\x00":
			zr, err := xz.NewReader(br)
			if err != nil {
				return err
			}
			cr = zr
		default:
			return fmt.Errorf("unknown archive start %x", magic)
		}
		if err := walkCPIO(cr, fn); err != nil {
			if errors.Is(err, Stop) {
				return nil
			}
			return err
		}
		if compressed { // (a decompressor reads ahead: nothing after a compressed archive can be read, and nothing follows it)
			return nil
		}
	}
}

// moduleName is the name of a module from its file name (".ko" and a compression suffix taken off).
func moduleName(file string) (string, bool) {
	b := path.Base(file)
	for _, suffix := range []string{".ko.xz", ".ko.zst", ".ko.gz", ".ko"} {
		if strings.HasSuffix(b, suffix) {
			return strings.TrimSuffix(b, suffix), true
		}
	}
	return "", false
}

// ModuleName is the name of a module from its file name (without ".ko" and any compression suffix); empty for a file that is not a module.
func ModuleName(file string) string {
	n, _ := moduleName(file)
	return n
}

// moduleVersion is the kernel version a module's path belongs to ("usr/lib/modules/<version>/kernel/..."), when it is one.
func moduleVersion(name string) (string, bool) {
	for _, prefix := range []string{"usr/lib/modules/", "lib/modules/"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			if _, isModule := moduleName(name); isModule {
				return strings.SplitN(rest, "/", 2)[0], true
			}
		}
	}
	return "", false
}

// Initrd is what was learnt from reading an initramfs for a few files.
type Initrd struct {
	Version string            // the kernel version of the modules it carries
	Have    map[string]bool   // the modules it carries, by name
	Files   map[string][]byte // the files that were asked for (by name without a leading slash), when it has them
}

// Read goes through an initramfs for the kernel version of its modules and for the files named in want, and stops when it has them all. A name that starts with "*/" matches a
// file of that name in any folder (the modules' index files sit in a folder named for the kernel version).
func Read(r io.Reader, want ...string) (*Initrd, error) {
	res := &Initrd{Have: map[string]bool{}, Files: map[string][]byte{}}
	match := func(name string) (string, bool) {
		for _, w := range want {
			if w == name || (strings.HasPrefix(w, "*/") && strings.HasSuffix(name, w[1:])) {
				return w, true
			}
		}
		return "", false
	}
	err := Walk(r, func(name string, size int64, body io.Reader) error {
		if key, ok := match(name); ok {
			b, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			res.Files[key] = b
		} else if v, ok := moduleVersion(name); ok {
			res.Version, res.Have[ModuleName(name)] = v, true
		}
		if res.Version != "" && len(res.Files) == len(want) {
			return Stop
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if res.Version == "" {
		return nil, errors.New("the kernel modules were not found in the start-up disk")
	}
	for _, w := range want {
		if _, ok := res.Files[w]; !ok {
			return nil, fmt.Errorf("%s was not found in the start-up disk", w)
		}
	}
	return res, nil
}

// Listing is what is in an initramfs: the names of its files, a few of the small ones (to read what a start-up script is made of), and the modules it carries.
type Listing struct {
	Names   map[string]bool   // every entry, without a leading "./"
	Texts   map[string][]byte // the files that were asked for, up to 64 KB of each
	Version string            // the kernel version of the modules it carries, when it has any
	Have    map[string]bool   // the modules it carries, by name
}

// Has says whether the initramfs has an entry of that name.
func (l *Listing) Has(name string) bool { return l.Names[name] }

// HasSuffix says whether it has an entry whose name ends with the text.
func (l *Listing) HasSuffix(suffix string) bool {
	for n := range l.Names {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// Inspect goes through the whole initramfs and lists it; texts are the names of the files whose beginning is to be kept.
func Inspect(r io.Reader, texts ...string) (*Listing, error) {
	l := &Listing{Names: map[string]bool{}, Texts: map[string][]byte{}, Have: map[string]bool{}}
	keep := map[string]bool{}
	for _, t := range texts {
		keep[t] = true
	}
	err := Walk(r, func(name string, size int64, body io.Reader) error {
		l.Names[name] = true
		if keep[name] {
			b, err := io.ReadAll(io.LimitReader(body, 64<<10))
			if err != nil {
				return err
			}
			l.Texts[name] = b
		} else if v, ok := moduleVersion(name); ok {
			l.Version, l.Have[ModuleName(name)] = v, true
		}
		return nil
	})
	return l, err
}

// walkCPIO reads a "newc" cpio archive up to its end marker, calling fn with each file's name and a reader for its contents.
func walkCPIO(r io.Reader, fn func(name string, size int64, body io.Reader) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var pos int64
	skip := func(n int64) error {
		m, err := io.CopyN(io.Discard, br, n)
		pos += m
		return err
	}
	for {
		hdr := make([]byte, 110)
		if _, err := io.ReadFull(br, hdr); err != nil {
			return err
		}
		pos += 110
		if string(hdr[:6]) != "070701" {
			return errors.New("not a cpio archive")
		}
		size := hexNum(hdr[54:62])
		nsize := hexNum(hdr[94:102])
		nb := make([]byte, nsize)
		if _, err := io.ReadFull(br, nb); err != nil {
			return err
		}
		pos += nsize
		name := strings.TrimSuffix(string(nb), "\x00")
		if err := skip((4 - pos%4) % 4); err != nil {
			return err
		}
		if name == "TRAILER!!!" {
			return nil
		}
		lr := &io.LimitedReader{R: br, N: size}
		if err := fn(strings.TrimPrefix(name, "./"), size, lr); err != nil {
			return err
		}
		if lr.N > 0 {
			if err := skip(lr.N); err != nil {
				return err
			}
		}
		pos += size - lr.N
		if err := skip((4 - pos%4) % 4); err != nil {
			return err
		}
	}
}

func hexNum(b []byte) int64 {
	var n int64
	for _, c := range b {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n |= int64(c - '0')
		case c >= 'a' && c <= 'f':
			n |= int64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n |= int64(c-'A') + 10
		}
	}
	return n
}
