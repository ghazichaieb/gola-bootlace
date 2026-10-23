// Kernel modules out of a system image (a squashfs): the drivers a start-up disk lacks, with what they need, in the order they must be loaded.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initread

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/CalebQ42/squashfs"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

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
