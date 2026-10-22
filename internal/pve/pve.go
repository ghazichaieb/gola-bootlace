// Package pve makes the Proxmox VE installer read its ISO over the network a piece at a time. The installer's start-up disk (its initramfs) looks for the ISO on a local
// drive, or takes it whole from a file inside itself (what PXE booting did until now, which needs RAM for the whole ISO twice over). This package builds an extra initramfs
// archive that goes after the installer's own: a copy of its start-up script with a few lines added that bring up the network, attach the ISO as /dev/nbd0 (see package nbd and
// cmd/bootlace-nbd) and let the script's own search for the ISO find it there, the network card drivers that the start-up disk lacks (taken from the system image on the ISO,
// which carries the full set of kernel modules), and the little scripts they need. The installer's files are not changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package pve

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/CalebQ42/squashfs"
	"github.com/klauspost/compress/zstd"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// Settings say where the ISO is offered as a network block disk.
type Settings struct {
	Host string
	Port int
	Name string
}

// Is says whether the ISO is a Proxmox installer of the kind handled here.
func Is(t interface{ Exists(string) bool }) bool {
	return t.Exists("/.pve-cd-id.txt") && t.Exists("/boot/initrd.img") && t.Exists("/pve-installer.squashfs")
}

// nicModules are the network card drivers worth bringing along, by file name: the common wired cards of PCs and servers and the virtual cards of the usual hypervisors.
// Those the installer's start-up disk already has are skipped; a driver whose card is not there does nothing.
var nicModules = []string{
	"e1000", "e1000e", "igb", "igc", "ixgbe", "i40e", "ice", "r8169", "realtek", "tg3", "bnx2", "bnxt_en", "vmxnet3", "hv_netvsc", "8139too", "8139cp",
	"forcedeth", "atlantic", "sky2", "r8152", "cdc_ether", "ax88179_178a", "virtio_net",
}

// Extra builds the archive for one ISO. img is the ISO's file system; the installer's start-up disk and its system image are read from it.
func Extra(img disc.FS, s Settings, helper []byte) ([]byte, error) {
	f, err := img.Open("/boot/initrd.img")
	if err != nil {
		return nil, err
	}
	init, version, have, err := readInitrd(f.Reader())
	if err != nil {
		return nil, fmt.Errorf("the installer's start-up disk: %w", err)
	}
	patched, ok := Patch(string(init), s)
	if !ok {
		return nil, errors.New("the installer's start-up script has a form that is not known here")
	}
	sq, err := img.Open("/pve-installer.squashfs")
	if err != nil {
		return nil, err
	}
	mods, err := modules(sq, version, have)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	files := []initrd.File{
		{Name: "init", Mode: 0o755, Data: []byte(patched)},
		{Name: "bootlace", Mode: 0o755, Dir: true},
		{Name: "bootlace/bootlace-nbd", Mode: 0o755, Data: helper},
		{Name: "bootlace/udhcpc.sh", Mode: 0o755, Data: []byte(udhcpcScript)},
		{Name: "bootlace/modules", Mode: 0o755, Dir: true},
	}
	for i, m := range mods {
		files = append(files, initrd.File{Name: fmt.Sprintf("bootlace/modules/%02d-%s", i, path.Base(m.path)), Mode: 0o644, Data: m.data})
	}
	return initrd.Archive(files), nil
}

const udhcpcScript = `#!/bin/sh
# Gola Bootlace: what busybox's udhcpc runs when it has an address.
case "$1" in
bound|renew)
    ifconfig "$interface" "$ip" netmask "${subnet:-255.255.255.0}" up
    for r in $router; do
        route add default gw "$r" dev "$interface" 2>/dev/null
    done
    ;;
esac
exit 0
`

// Patch adds Bootlace's lines to the installer's start-up script, or says that the script does not look as expected (then the old way, the whole ISO inside the
// initramfs, is still there).
func Patch(script string, s Settings) (string, bool) {
	const anchor = "# we have no iscsi daemon"
	const scan = "/sys/block/nvme*; do"
	if strings.Count(script, anchor) != 1 || strings.Count(script, scan) != 1 {
		return "", false
	}
	block := fmt.Sprintf(netBlock, shellWord(s.Host), s.Port, shellWord(s.Name))
	script = strings.Replace(script, anchor, block+anchor, 1)
	script = strings.Replace(script, scan, "/sys/block/nvme* /sys/block/nbd0; do", 1)
	return script, true
}

// netBlock is added before the script looks for the ISO. Everything it needs is in the archive; without the network the script goes on as before and does not find an ISO.
const netBlock = `# Gola Bootlace: the ISO is read over the network a piece at a time (a network block disk), so the PC does not need memory for all of it.
if [ -x /bootlace/bootlace-nbd ]; then
    echo "Gola Bootlace: starting the network"
    /bootlace/bootlace-nbd insmod /bootlace/modules/* >/dev/null 2>&1
    modprobe -q nbd
    netok=
    for try in 1 2 3 4 5 6 7 8 9 10 11 12; do
        for n in /sys/class/net/*; do
            dev="${n##*/}"
            [ "$dev" = lo ] && continue
            [ -d "$n" ] || continue
            ip link set "$dev" up
            if udhcpc -i "$dev" -n -q -t 3 -T 2 -s /bootlace/udhcpc.sh >/dev/null 2>&1; then
                netok=$dev
                break
            fi
        done
        [ -n "$netok" ] && break
        sleep 2
    done
    if [ -n "$netok" ]; then
        echo "Gola Bootlace: reading the ISO over the network ($netok)"
        /bootlace/bootlace-nbd -host %s -port %d -name %s -dev /dev/nbd0 &
        for try in 1 2 3 4 5 6 7 8 9 10; do
            [ "$(cat /sys/block/nbd0/size 2>/dev/null)" != 0 ] && break
            sleep 1
        done
    else
        echo "Gola Bootlace: no network card answered, the ISO cannot be read over the network"
    fi
fi

`

// shellWord keeps a value from being read as anything but one word.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// readInitrd takes an initramfs apart as far as needed: the start-up script, the kernel's version and the modules it already carries. An initramfs is one or more cpio
// archives (the first often only microcode), the last one compressed.
func readInitrd(r io.Reader) (init []byte, version string, have map[string]bool, err error) {
	br := bufio.NewReaderSize(r, 1<<20)
	have = map[string]bool{}
	for {
		// skip padding between archives
		for {
			b, e := br.Peek(1)
			if e != nil {
				return nil, "", nil, errors.New("no compressed archive found")
			}
			if b[0] != 0 {
				break
			}
			_, _ = br.ReadByte()
		}
		magic, e := br.Peek(6)
		if e != nil {
			return nil, "", nil, e
		}
		var cr io.Reader = br
		switch {
		case string(magic) == "070701":
		case magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
			zr, e := zstd.NewReader(br)
			if e != nil {
				return nil, "", nil, e
			}
			defer zr.Close()
			cr = zr
		case magic[0] == 0x1f && magic[1] == 0x8b:
			zr, e := gzip.NewReader(br)
			if e != nil {
				return nil, "", nil, e
			}
			cr = zr
		default:
			return nil, "", nil, fmt.Errorf("unknown archive start %x", magic)
		}
		found := false
		e = walkCPIO(cr, func(name string, size int64, body io.Reader) error {
			switch {
			case name == "init":
				found = true
				b, e := io.ReadAll(body)
				init = b
				return e
			case strings.HasPrefix(name, "lib/modules/") && strings.HasSuffix(name, ".ko"):
				parts := strings.SplitN(strings.TrimPrefix(name, "lib/modules/"), "/", 2)
				version = parts[0]
				have[strings.TrimSuffix(path.Base(name), ".ko")] = true
			}
			return nil
		})
		if e != nil {
			return nil, "", nil, e
		}
		if found && version != "" {
			return init, version, have, nil
		}
		if string(magic) != "070701" {
			return nil, "", nil, errors.New("the start-up script or the kernel modules were not found")
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

type module struct {
	path string
	data []byte
}

// modules reads the wanted network drivers, and what they need, out of the system image: the kernel's modules, uncompressed, are there under usr/lib/modules.
func modules(sq disc.File, version string, have map[string]bool) ([]module, error) {
	rd, err := squashfs.NewReader(readerAt{sq})
	if err != nil {
		return nil, err
	}
	base := "usr/lib/modules/" + version + "/"
	dep, err := sqRead(rd, base+"modules.dep")
	if err != nil {
		return nil, err
	}
	deps := map[string][]string{} // module path -> the paths it needs
	byName := map[string]string{}
	for _, line := range strings.Split(string(dep), "\n") {
		p, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		deps[p] = strings.Fields(rest)
		byName[strings.TrimSuffix(path.Base(p), ".ko")] = p
	}
	var order []string
	seen := map[string]bool{}
	var add func(p string)
	add = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		for _, d := range deps[p] {
			add(d)
		}
		if !have[strings.TrimSuffix(path.Base(p), ".ko")] {
			order = append(order, p)
		}
	}
	for _, n := range nicModules {
		if p, ok := byName[n]; ok && !have[n] {
			add(p)
		}
	}
	var out []module
	for _, p := range order {
		b, err := sqRead(rd, base+p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, module{p, b})
	}
	if len(out) == 0 {
		return nil, errors.New("no network driver found in the system image")
	}
	return out, nil
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

// readerAt shows a disc file as the io.ReaderAt that the squashfs reader wants.
type readerAt struct{ f disc.File }

func (r readerAt) ReadAt(p []byte, off int64) (int, error) { return r.f.ReadAt(p, off) }
