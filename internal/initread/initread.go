// Package initread takes an initramfs apart as far as is needed to add to it, and collects network card drivers out of the system image of a live ISO. Some live systems
// (Proxmox's installer, Tails) start from an initramfs made for a USB stick or a disc: it has no network card drivers at all, which a start over the network needs. The full
// set of kernel modules is in the system image (a squashfs), so the drivers worth bringing along are read from there and put in an extra initramfs archive (see package initrd).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initread

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/CalebQ42/squashfs"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Initrd is what was learnt from reading an initramfs.
type Initrd struct {
	Version string            // the kernel version of the modules it carries
	Have    map[string]bool   // the modules it carries, by name
	Files   map[string][]byte // the files that were asked for (by name without a leading slash), when it has them
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

// Read goes through an initramfs: one or more cpio archives (the first often only microcode), the last one compressed. It stops when it has the kernel version and every file in
// want, or at the end.
func Read(r io.Reader, want ...string) (*Initrd, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	res := &Initrd{Have: map[string]bool{}, Files: map[string][]byte{}}
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w] = true
	}
	// a wanted name that starts with "*/" matches any file of that name below some folder (the modules' index files sit under a folder named for the kernel version)
	matchWanted := func(name string) (string, bool) {
		if wanted[name] {
			return name, true
		}
		for w := range wanted {
			if strings.HasPrefix(w, "*/") && strings.HasSuffix(name, w[1:]) {
				return w, true
			}
		}
		return "", false
	}
	for {
		// skip padding between archives
		for {
			b, e := br.Peek(1)
			if e != nil {
				return nil, errors.New("no compressed archive found")
			}
			if b[0] != 0 {
				break
			}
			_, _ = br.ReadByte()
		}
		peek, e := br.Peek(6)
		if e != nil {
			return nil, e
		}
		magic := append([]byte(nil), peek...) // (Peek's bytes are overwritten by the next read)
		var cr io.Reader = br
		switch {
		case string(magic) == "070701":
		case magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
			zr, e := zstd.NewReader(br)
			if e != nil {
				return nil, e
			}
			defer zr.Close()
			cr = zr
		case magic[0] == 0x1f && magic[1] == 0x8b:
			zr, e := gzip.NewReader(br)
			if e != nil {
				return nil, e
			}
			cr = zr
		case magic[0] == 0xfd && string(magic[1:6]) == "7zXZ\x00":
			zr, e := xz.NewReader(br)
			if e != nil {
				return nil, e
			}
			cr = zr
		default:
			return nil, fmt.Errorf("unknown archive start %x", magic)
		}
		e = walkCPIO(cr, func(name string, size int64, body io.Reader) error {
			key, isWanted := matchWanted(name)
			switch {
			case isWanted:
				b, e := io.ReadAll(body)
				res.Files[key] = b
				return e
			default:
				for _, prefix := range []string{"lib/modules/", "usr/lib/modules/"} {
					if !strings.HasPrefix(name, prefix) {
						continue
					}
					if n, ok := moduleName(name); ok {
						res.Version = strings.SplitN(strings.TrimPrefix(name, prefix), "/", 2)[0]
						res.Have[n] = true
					}
				}
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
		if res.Version != "" && len(res.Files) == len(wanted) {
			return res, nil
		}
		if string(magic) != "070701" && res.Version == "" {
			return nil, errors.New("the kernel modules were not found in the start-up disk")
		}
		if string(magic) != "070701" { // the last (compressed) archive was read: that is all there is
			for w := range wanted {
				if _, ok := res.Files[w]; !ok {
					return nil, fmt.Errorf("%s was not found in the start-up disk", w)
				}
			}
			return res, nil
		}
	}
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

// NICModules are the network card drivers worth bringing along, by module name: the common wired cards of PCs and servers, USB network adapters and the virtual cards of the
// usual hypervisors. Those the start-up disk already has are skipped; a driver whose card is not there does nothing.
var NICModules = []string{
	"e1000", "e1000e", "igb", "igc", "ixgbe", "i40e", "ice", "r8169", "realtek", "tg3", "bnx2", "bnxt_en", "vmxnet3", "hv_netvsc", "8139too", "8139cp",
	"forcedeth", "atlantic", "sky2", "r8152", "cdc_ether", "ax88179_178a", "virtio_net", "alx", "atl1c", "igbvf", "ixgbevf", "mlx4_en", "mlx5_core", "qede", "be2net",
	"sfc", "e100", "via-rhine", "r8125", "cdc_ncm", "asix", "ax88772", "lan78xx", "smsc95xx", "rtl8150", "pegasus",
}

// Module is a kernel module ready to be loaded: its file, uncompressed.
type Module struct {
	Path string // in the system image, relative to the folder of the kernel's modules
	Data []byte // the module itself, uncompressed
}

// Modules reads the wanted drivers, and what they need, out of the system image, in the order they must be loaded. What the start-up disk has of them is brought again: it
// keeps its modules compressed or unloaded, and a driver can only be loaded from a plain file. The kernel's modules are looked for under usr/lib/modules/<version>,
// lib/modules/<version> or modules/<version> (a modloop), compressed or not.
func Modules(sq io.ReaderAt, version string, names []string) ([]Module, error) {
	rd, err := squashfs.NewReader(sq)
	if err != nil {
		return nil, err
	}
	var base string
	var dep []byte
	for _, b := range []string{"usr/lib/modules/" + version + "/", "lib/modules/" + version + "/", "modules/" + version + "/"} {
		if dep, err = sqRead(rd, b+"modules.dep"); err == nil {
			base = b
			break
		}
	}
	if base == "" {
		return nil, fmt.Errorf("the system image has no modules for the kernel %s", version)
	}
	var out []Module
	for _, p := range loadOrder(string(dep), names) {
		b, err := sqRead(rd, base+p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if b, err = decompress(p, b); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, Module{Path: p, Data: b})
	}
	if len(out) == 0 {
		return nil, errors.New("no network driver found in the system image")
	}
	return out, nil
}

// loadOrder is the paths of the named modules and of everything they need, each after the modules it needs, from the text of a modules.dep ("path: needed paths").
func loadOrder(dep string, names []string) []string {
	needs := map[string][]string{}
	byName := map[string]string{}
	for _, line := range strings.Split(dep, "\n") {
		p, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		needs[p] = strings.Fields(rest)
		if n, ok := moduleName(p); ok {
			byName[n] = p
		}
	}
	var order []string
	seen := map[string]bool{}
	var add func(p string)
	add = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		for _, d := range needs[p] {
			add(d)
		}
		order = append(order, p)
	}
	for _, n := range names {
		if p, ok := byName[n]; ok {
			add(p)
		}
	}
	return order
}

// decompress gives the module's own bytes (the kernel loads a module from memory, not from a compressed file).
func decompress(name string, b []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, ".xz"):
		r, err := xz.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(r)
	case strings.HasSuffix(name, ".gz"):
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(r)
	case strings.HasSuffix(name, ".zst"):
		r, err := zstd.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return b, nil
}

// sqRead reads a file out of the squashfs, one path component at a time (the reader's own path handling turns the slashes into backslashes on Windows).
func sqRead(rd squashfs.Reader, p string) ([]byte, error) {
	cur := rd.FS
	parts := strings.Split(p, "/")
	for i, part := range parts {
		f, err := cur.OpenFile(part)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if i == len(parts)-1 {
			defer f.Close()
			return io.ReadAll(f)
		}
		if cur, err = f.FS(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil, errors.New("empty path")
}
