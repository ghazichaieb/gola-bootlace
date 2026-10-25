// Package debinst makes the extra start-up archive that lets the Debian installer of a netinst ISO find its CD over the network.
//
// The start-up disk of the CD flavour of the installer (Debian's netinst, Kali's) has one way to continue: it looks for the CD it was started from, and takes the rest of its
// parts (network card drivers, the network setup) from there. Started over the network there is no such CD, so the first step, "Detect and mount installation media", fails.
// Bootlace already offers every ISO as an iSCSI CD-ROM. The start-up disk has the kernel's iSCSI modules and a busybox with a DHCP client, but no network card drivers and no iSCSI
// client; both are packages (udebs) in the ISO's own pool. So the archive adds, taken from that pool and matching the kernel the ISO starts, the network card drivers and
// iscsistart (with the library it needs), and a start-up script that runs before the installer's first step: it loads the drivers, gets an address, logs in to the CD-ROM target and
// waits for /dev/sr0, which the installer's own CD detection then finds and mounts as it would a real disc.
//
// Nothing here knows a Debian version: the kernel's version is read from the start-up disk, the packages are found in the ISO's Packages index by name, and the libraries that
// iscsistart needs are checked against what the start-up disk has.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package debinst

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
	"github.com/ghazichaieb/gola-bootlace/internal/udeb"
)

// Tree is the ISO's files.
type Tree interface {
	Exists(name string) bool
	List(name string) ([]string, error)
	Open(name string) (disc.File, error)
}

// Target is the iSCSI CD-ROM the installer logs in to.
type Target struct {
	Host string
	Port int
	IQN  string
}

// ModuleParts are the kernel module packages that carry the network card drivers: their names are "<part>-<kernel version>-di".
var ModuleParts = []string{"nic-modules", "nic-shared-modules", "nic-usb-modules"}

// initiatorName is what the installer calls itself when it logs in.
const initiatorName = "iqn.1993-08.org.debian:01:bootlace-installer"

// Parts is what is to be taken from the ISO's pool for a start-up disk.
type Parts struct {
	Kernel   string          // the kernel version of the start-up disk's modules
	Modules  []udeb.Package  // the network card driver packages, for that kernel
	ISCSI    udeb.Package    // the iSCSI client
	Index    udeb.Index      // every package of the ISO's installer index
	Have     map[string]bool // what the start-up disk already has (every entry of its file tree)
	ModBase  string          // where the start-up disk keeps its modules: "lib/modules" or "usr/lib/modules"
	LibDir   string          // where its libraries are
	SbinDir  string          // where its system programs are
	StartDir string          // where it keeps the scripts it runs at start-up
}

// Plan reads the start-up disk (initramfs, a path in the ISO) and the ISO's package index and says what the archive is made of.
func Plan(t Tree, initramfs string) (*Parts, error) {
	f, err := t.Open(initramfs)
	if err != nil {
		return nil, fmt.Errorf("the installer's start-up disk: %w", err)
	}
	l, err := initread.Inspect(f.Reader())
	if err != nil {
		return nil, fmt.Errorf("the installer's start-up disk: %w", err)
	}
	if l.Version == "" {
		return nil, errors.New("the installer's start-up disk has no kernel modules to tell its kernel by")
	}
	p := &Parts{Kernel: l.Version, Have: l.Names}
	p.ModBase = "lib/modules"
	if !hasPrefix(l.Names, "lib/modules/"+l.Version+"/") && hasPrefix(l.Names, "usr/lib/modules/"+l.Version+"/") {
		p.ModBase = "usr/lib/modules"
	}
	p.LibDir, p.SbinDir = "usr/lib/x86_64-linux-gnu", "sbin"
	if l.Names["lib/x86_64-linux-gnu/libc.so.6"] {
		p.LibDir = "lib/x86_64-linux-gnu"
	}
	if l.Names["usr/sbin"] { // a real folder whether /sbin is a link to it (usr-merged) or a folder of its own (both are in the installer's PATH)
		p.SbinDir = "usr/sbin"
	}
	p.StartDir = "lib/debian-installer-startup.d"
	if !l.Names[p.StartDir] && l.Names["usr/"+p.StartDir] {
		p.StartDir = "usr/" + p.StartDir
	}
	if p.Index, err = LoadIndex(t); err != nil {
		return nil, err
	}
	for _, part := range ModuleParts {
		if pkg, ok := p.Index[part+"-"+p.Kernel+"-di"]; ok {
			p.Modules = append(p.Modules, pkg)
		}
	}
	if len(p.Modules) == 0 || p.Modules[0].Name != "nic-modules-"+p.Kernel+"-di" {
		return nil, fmt.Errorf("the ISO has no network card drivers (nic-modules) for the installer's kernel %s", p.Kernel)
	}
	var ok bool
	if p.ISCSI, ok = p.Index["open-iscsi-udeb"]; !ok {
		return nil, errors.New("the ISO has no iSCSI client (open-iscsi-udeb) for the installer")
	}
	return p, nil
}

func hasPrefix(names map[string]bool, prefix string) bool {
	for n := range names {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// LoadIndex reads the installer package indexes of every suite of the ISO (dists/<suite>/main/debian-installer/binary-amd64/Packages.gz) into one.
func LoadIndex(t Tree) (udeb.Index, error) {
	suites, err := t.List("/dists")
	if err != nil {
		return nil, errors.New("the ISO has no dists folder: it is not a Debian installer ISO")
	}
	sort.Strings(suites)
	var all []udeb.Package
	for _, s := range suites {
		if !strings.HasSuffix(s, "/") {
			continue
		}
		base := "/dists/" + strings.TrimSuffix(s, "/") + "/main/debian-installer/binary-amd64/"
		for _, name := range []string{"Packages.gz", "Packages.xz", "Packages"} {
			f, err := t.Open(base + name)
			if err != nil {
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(f.Reader(), 64<<20))
			if err != nil {
				return nil, fmt.Errorf("%s%s: %w", base, name, err)
			}
			r, err := udeb.Gunzip(raw)
			if err != nil {
				return nil, fmt.Errorf("%s%s: %w", base, name, err)
			}
			list, err := udeb.ParsePackages(r)
			if err != nil {
				return nil, fmt.Errorf("%s%s: %w", base, name, err)
			}
			all = append(all, list...)
			break
		}
	}
	if len(all) == 0 {
		return nil, errors.New("the ISO has no installer package index (Packages.gz of debian-installer)")
	}
	return udeb.NewIndex(all), nil
}

// Archive makes the extra archive for the installer whose start-up disk is `initramfs` (a path in the ISO) to log in to `target`.
func Archive(t Tree, initramfs string, target Target) ([]byte, error) {
	if target.Host == "" || target.IQN == "" || target.Port == 0 {
		return nil, errors.New("the iSCSI server is not on: there is no network CD-ROM to log in to")
	}
	p, err := Plan(t, initramfs)
	if err != nil {
		return nil, err
	}
	var b initrd.Builder
	made := map[string]bool{}
	dirs := func(name string) { // a folder the start-up disk does not have is made, and only that one (a link such as /lib must stay what it is)
		parts := strings.Split(path.Dir(name), "/")
		for i := range parts {
			d := strings.Join(parts[:i+1], "/")
			if d != "." && !p.Have[d] && !made[d] {
				made[d] = true
				b.Dir(d)
			}
		}
	}
	add := func(name string, mode uint32, data []byte) {
		if made[name] {
			return
		}
		made[name] = true
		dirs(name)
		b.File(name, mode, data)
	}

	// network card drivers
	prefix := p.ModBase + "/" + p.Kernel + "/"
	var modules int
	for _, pkg := range p.Modules {
		err := walkPackage(t, pkg, func(e udeb.Entry) error {
			name := remap(e.Name, p.ModBase, p.Kernel)
			if name == "" || e.Mode.IsDir() || e.Link != "" || !strings.HasPrefix(name, prefix) {
				return nil
			}
			add(name, 0o644, e.Data)
			modules++
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pkg.Name, err)
		}
	}
	if modules == 0 {
		return nil, errors.New("the network card driver packages hold no modules for the installer's kernel")
	}

	// the iSCSI client and the library only it needs
	extra := map[string][]byte{}
	var iscsistart []byte
	err = walkPackage(t, p.ISCSI, func(e udeb.Entry) error {
		switch {
		case e.Name == "usr/sbin/iscsistart" || e.Name == "sbin/iscsistart":
			iscsistart = e.Data
		case strings.HasPrefix(path.Base(e.Name), "libopeniscsiusr.so") && e.Link == "" && !e.Mode.IsDir():
			extra[path.Base(e.Name)] = e.Data
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.ISCSI.Name, err)
	}
	if iscsistart == nil || len(extra) == 0 {
		return nil, errors.New("the iSCSI client package of the ISO has no iscsistart or no libopeniscsiusr")
	}
	// the links of the library (libopeniscsiusr.so.0 -> libopeniscsiusr.so.0.2.0) are written as copies: the archive holds plain files
	for name, data := range extra {
		if soname := sonameOf(data); soname != "" && soname != name {
			extra[soname] = data
		}
	}
	// what iscsistart and its library need must be there: the start-up disk has it, or the package that carries it is added
	provided := map[string]bool{}
	for n := range extra {
		provided[n] = true
	}
	programs := [][]byte{iscsistart}
	for _, d := range extra {
		programs = append(programs, d)
	}
	missing, err := missingLibs(p, programs, provided)
	if err != nil {
		return nil, err
	}
	for _, soname := range missing {
		data, err := findLib(t, p, soname)
		if err != nil {
			return nil, err
		}
		extra[soname] = data
	}
	add(p.SbinDir+"/iscsistart", 0o755, iscsistart)
	names := make([]string, 0, len(extra))
	for n := range extra {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		add(p.LibDir+"/"+n, 0o755, extra[n])
	}
	add("etc/bootlace-udhcpc.sh", 0o755, []byte(udhcpcScript))
	add(p.StartDir+"/S12bootlace-iscsi", 0o755, []byte(Script(target)))
	return b.Bytes(), nil
}

// remap turns the path of a module in a package ("lib/modules/<version>/...", or the same under usr) into the path the start-up disk keeps its modules under.
func remap(name, modBase, kernel string) string {
	for _, from := range []string{"lib/modules/" + kernel + "/", "usr/lib/modules/" + kernel + "/"} {
		if rest, ok := strings.CutPrefix(name, from); ok {
			return modBase + "/" + kernel + "/" + rest
		}
	}
	return ""
}

// sonameOf is the name a shared library goes by ("libopeniscsiusr.so.0"), from its own header.
func sonameOf(data []byte) string {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	defer f.Close()
	names, err := f.DynString(elf.DT_SONAME)
	if err != nil || len(names) == 0 {
		return ""
	}
	return names[0]
}

// neededLibs are the shared libraries a program or library asks for.
func neededLibs(data []byte) ([]string, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ImportedLibraries()
}

// haveLib says whether the start-up disk has a library by that name.
func (p *Parts) haveLib(soname string) bool {
	for n := range p.Have {
		if path.Base(n) == soname {
			return true
		}
	}
	return false
}

// missingLibs lists the libraries the programs need that neither the start-up disk nor `provided` has.
func missingLibs(p *Parts, programs [][]byte, provided map[string]bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, prog := range programs {
		libs, err := neededLibs(prog)
		if err != nil {
			return nil, fmt.Errorf("iscsistart is not a program this machine type can read: %w", err)
		}
		for _, l := range libs {
			if !seen[l] && !provided[l] && !p.haveLib(l) {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// findLib looks for a library the start-up disk lacks in the packages the iSCSI client depends on.
func findLib(t Tree, p *Parts, soname string) ([]byte, error) {
	skip := func(n string) bool { return strings.HasPrefix(n, "libc6") || strings.HasPrefix(n, "kernel-image") }
	for _, pkg := range p.Index.Closure(p.ISCSI.Name, skip) {
		var found []byte
		err := walkPackage(t, pkg, func(e udeb.Entry) error {
			if found == nil && path.Base(e.Name) == soname && e.Link == "" && !e.Mode.IsDir() {
				found = e.Data
			}
			return nil
		})
		if err != nil {
			continue
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("the installer's start-up disk lacks the library %s that the iSCSI client needs, and no package it depends on carries it", soname)
}

// walkPackage reads one udeb of the ISO's pool.
func walkPackage(t Tree, pkg udeb.Package, fn func(udeb.Entry) error) error {
	if pkg.Filename == "" {
		return errors.New("the index does not say where the package is")
	}
	f, err := t.Open("/" + pkg.Filename)
	if err != nil {
		return err
	}
	return udeb.Files(f.Reader(), fn)
}
