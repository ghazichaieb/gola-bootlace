// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package debinst

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// memFile and memTree are an ISO held in memory.
type memFile struct{ r *bytes.Reader }

func (f memFile) Size() int64                           { return f.r.Size() }
func (f memFile) Reader() io.ReadSeeker                 { return io.NewSectionReader(f.r, 0, f.r.Size()) }
func (f memFile) ReadAt(p []byte, o int64) (int, error) { return f.r.ReadAt(p, o) }

type memTree map[string][]byte

func (m memTree) Exists(name string) bool { _, ok := m[name]; return ok }
func (m memTree) Open(name string) (disc.File, error) {
	b, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("%s: no such file", name)
	}
	return memFile{r: bytes.NewReader(b)}, nil
}
func (m memTree) List(name string) ([]string, error) {
	prefix := strings.TrimSuffix(name, "/") + "/"
	seen := map[string]bool{}
	for n := range m {
		if rest, ok := strings.CutPrefix(n, prefix); ok {
			first, _, more := strings.Cut(rest, "/")
			if more {
				first += "/"
			}
			seen[first] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%s: no such folder", name)
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// buildELF makes the smallest shared object that debug/elf reads a SONAME and NEEDED entries from: a header, a string table, a dynamic section and the section headers.
func buildELF(soname string, needed ...string) []byte {
	dynstr := []byte{0}
	add := func(s string) uint64 {
		off := uint64(len(dynstr))
		dynstr = append(append(dynstr, s...), 0)
		return off
	}
	var dyn []byte
	entry := func(tag elf.DynTag, v uint64) {
		dyn = binary.LittleEndian.AppendUint64(dyn, uint64(tag))
		dyn = binary.LittleEndian.AppendUint64(dyn, v)
	}
	for _, n := range needed {
		entry(elf.DT_NEEDED, add(n))
	}
	if soname != "" {
		entry(elf.DT_SONAME, add(soname))
	}
	entry(elf.DT_NULL, 0)
	shstr := []byte("\x00.dynstr\x00.dynamic\x00.shstrtab\x00")
	const hdr = 64
	dynstrOff := uint64(hdr)
	dynOff := dynstrOff + uint64(len(dynstr))
	shstrOff := dynOff + uint64(len(dyn))
	shOff := (shstrOff + uint64(len(shstr)) + 7) &^ 7

	le := binary.LittleEndian
	h := make([]byte, hdr)
	copy(h, "\x7fELF")
	h[4], h[5], h[6] = byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)
	le.PutUint16(h[16:], uint16(elf.ET_DYN))
	le.PutUint16(h[18:], uint16(elf.EM_X86_64))
	le.PutUint32(h[20:], 1)
	le.PutUint64(h[40:], shOff)
	le.PutUint16(h[52:], hdr)
	le.PutUint16(h[58:], 64) // section header size
	le.PutUint16(h[60:], 4)  // section count
	le.PutUint16(h[62:], 3)  // the section names are in section 3

	out := append(h, dynstr...)
	out = append(out, dyn...)
	out = append(out, shstr...)
	for uint64(len(out)) < shOff {
		out = append(out, 0)
	}
	section := func(name uint32, typ elf.SectionType, off, size uint64, link uint32, entsize uint64) {
		s := make([]byte, 64)
		le.PutUint32(s[0:], name)
		le.PutUint32(s[4:], uint32(typ))
		le.PutUint64(s[24:], off)
		le.PutUint64(s[32:], size)
		le.PutUint32(s[40:], link)
		le.PutUint64(s[56:], entsize)
		out = append(out, s...)
	}
	section(0, elf.SHT_NULL, 0, 0, 0, 0)
	section(1, elf.SHT_STRTAB, dynstrOff, uint64(len(dynstr)), 0, 0)
	section(9, elf.SHT_DYNAMIC, dynOff, uint64(len(dyn)), 1, 16)
	section(18, elf.SHT_STRTAB, shstrOff, uint64(len(shstr)), 0, 0)
	return out
}

// udebOf makes a udeb: an ar archive of debian-binary and data.tar.gz.
func udebOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: "./" + n, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
			t.Fatal(err)
		}
		tw.Write(files[n])
	}
	tw.Close()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(raw.Bytes())
	zw.Close()
	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	for _, m := range []struct {
		name string
		body []byte
	}{{"debian-binary", []byte("2.0\n")}, {"data.tar.gz", gz.Bytes()}} {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", m.name+"/", 0, 0, 0, "100644", len(m.body))
		out.Write(m.body)
		if len(m.body)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	io.WriteString(w, s)
	w.Close()
	return b.Bytes()
}

// layout says how the fake installer's start-up disk is laid out.
type layout struct {
	usrMerged bool   // lib and sbin are links to usr/lib and usr/sbin: the archive has only the usr names
	kernel    string // the kernel version of the modules
	libInUsr  bool
}

// isoFor makes an ISO tree with a start-up disk of the given layout and the packages of the installer's pool.
func isoFor(t *testing.T, l layout, mutate func(memTree)) (memTree, string) {
	t.Helper()
	var b initrd.Builder
	mods := "lib/modules/" + l.kernel
	start, libdir := "lib/debian-installer-startup.d", "lib/x86_64-linux-gnu"
	sbin := "sbin"
	if l.usrMerged {
		mods, start, libdir, sbin = "usr/"+mods, "usr/"+start, "usr/lib/x86_64-linux-gnu", "usr/sbin"
		b.Dir("usr")
		b.Dir("usr/lib")
		b.Dir("usr/lib/x86_64-linux-gnu")
		b.Dir("usr/lib/debian-installer-startup.d")
		b.Dir("usr/sbin")
		b.Dir("lib") // (a link to usr/lib in the real one: it is listed all the same)
		b.Dir("sbin")
	}
	b.File(libdir+"/libc.so.6", 0o755, buildELF("libc.so.6"))
	b.File(libdir+"/libkmod.so.2", 0o755, buildELF("libkmod.so.2", "libc.so.6"))
	b.File(libdir+"/libcrypto.so.3", 0o755, buildELF("libcrypto.so.3", "libc.so.6"))
	b.File(start+"/S01mount", 0o755, []byte("#!/bin/sh\n"))
	b.File(sbin+"/modprobe", 0o755, []byte("x"))
	b.File(mods+"/kernel/drivers/scsi/iscsi_tcp.ko", 0o644, []byte("m"))
	initramfs := b.Bytes()

	iscsistart := buildELF("", "libopeniscsiusr.so.0", "libkmod.so.2", "libc.so.6")
	usr := buildELF("libopeniscsiusr.so.0", "libc.so.6")
	pool := "pool/main/"
	tree := memTree{
		"/install.amd/initrd.gz": gz(string(initramfs)),
	}
	tree["/"+pool+"o/open-iscsi/open-iscsi-udeb_2.1.11_amd64.udeb"] = udebOf(t, map[string][]byte{
		"usr/sbin/iscsistart":                               iscsistart,
		"usr/lib/x86_64-linux-gnu/libopeniscsiusr.so.0.2.0": usr,
	})
	nic := func(part string, files ...string) string {
		name := part + "-" + l.kernel + "-di"
		m := map[string][]byte{}
		for _, f := range files {
			m["lib/modules/"+l.kernel+"/kernel/drivers/net/"+f] = []byte("mod " + f)
		}
		file := pool + "l/linux/" + name + "_1_amd64.udeb"
		tree["/"+file] = udebOf(t, m)
		return fmt.Sprintf("Package: %s\nFilename: %s\n\n", name, file)
	}
	index := nic("nic-modules", "e1000e.ko", "virtio_net.ko") +
		nic("nic-shared-modules", "mii.ko") +
		nic("nic-usb-modules", "r8152.ko") +
		"Package: open-iscsi-udeb\nFilename: " + pool + "o/open-iscsi/open-iscsi-udeb_2.1.11_amd64.udeb\nDepends: libc6-udeb, libkmod2-udeb\n\n" +
		// a package of another kernel version: never taken
		"Package: nic-modules-5.1.0-other-di\nFilename: pool/main/l/linux/nic-modules-5.1.0-other-di_1_amd64.udeb\n\n"
	tree["/dists/stable/main/debian-installer/binary-amd64/Packages.gz"] = gz(index)
	if mutate != nil {
		mutate(tree)
	}
	return tree, "/install.amd/initrd.gz"
}

var target = Target{Host: "192.168.8.201", Port: 3260, IQN: "iqn.2026-10.org.gola:bootlace:abc123"}

// names lists what an archive holds.
func names(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	err := initread.Walk(bytes.NewReader(archive), func(name string, size int64, body io.Reader) error {
		b, _ := io.ReadAll(body)
		got[name] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPlanReadsTheKernelVersionFromTheStartUpDisk(t *testing.T) {
	for _, l := range []layout{
		{kernel: "6.1.0-18-amd64"},
		{kernel: "6.12.107+deb13-amd64", usrMerged: true},
		{kernel: "7.3.0-1-amd64", usrMerged: true}, // a kernel that does not exist yet: nothing in the code names a version
	} {
		t.Run(l.kernel, func(t *testing.T) {
			tree, rd := isoFor(t, l, nil)
			p, err := Plan(tree, rd)
			if err != nil {
				t.Fatal(err)
			}
			if p.Kernel != l.kernel {
				t.Errorf("kernel %q, want %q", p.Kernel, l.kernel)
			}
			if len(p.Modules) != 3 || p.Modules[0].Name != "nic-modules-"+l.kernel+"-di" {
				t.Errorf("driver packages: %+v", p.Modules)
			}
			wantMods, wantSbin := "lib/modules", "sbin"
			if l.usrMerged {
				wantMods, wantSbin = "usr/lib/modules", "usr/sbin"
			}
			if p.ModBase != wantMods || p.SbinDir != wantSbin {
				t.Errorf("layout: modules %q, sbin %q", p.ModBase, p.SbinDir)
			}
			if p.ISCSI.Name != "open-iscsi-udeb" {
				t.Errorf("iscsi package %q", p.ISCSI.Name)
			}
		})
	}
}

func TestPlanSaysWhatIsMissing(t *testing.T) {
	l := layout{kernel: "6.12.107+deb13-amd64", usrMerged: true}
	for _, c := range []struct {
		name   string
		mutate func(memTree)
		want   string
	}{
		{"no dists folder", func(m memTree) { delete(m, "/dists/stable/main/debian-installer/binary-amd64/Packages.gz") }, "no dists folder"},
		{"index without the drivers of this kernel", func(m memTree) {
			m["/dists/stable/main/debian-installer/binary-amd64/Packages.gz"] = gz("Package: open-iscsi-udeb\nFilename: x\n\nPackage: nic-modules-9.9-di\nFilename: y\n\n")
		}, "no network card drivers"},
		{"no iSCSI client", func(m memTree) {
			m["/dists/stable/main/debian-installer/binary-amd64/Packages.gz"] = gz("Package: nic-modules-" + l.kernel + "-di\nFilename: y\n\n")
		}, "no iSCSI client"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, rd := isoFor(t, l, c.mutate)
			if _, err := Plan(tree, rd); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want an error with %q", err, c.want)
			}
		})
	}
	tree, _ := isoFor(t, l, nil)
	if _, err := Plan(tree, "/install.amd/missing.gz"); err == nil {
		t.Error("a start-up disk that is not there is planned")
	}
	// a start-up disk without modules cannot tell its kernel
	var b initrd.Builder
	b.File("bin/sh", 0o755, []byte("x"))
	tree["/install.amd/bare.gz"] = gz(string(b.Bytes()))
	if _, err := Plan(tree, "/install.amd/bare.gz"); err == nil || !strings.Contains(err.Error(), "kernel") {
		t.Errorf("no modules: %v", err)
	}
}

func TestArchiveHoldsWhatTheInstallerLacks(t *testing.T) {
	for _, l := range []layout{
		{kernel: "6.1.0-18-amd64"},
		{kernel: "6.12.107+deb13-amd64", usrMerged: true},
		{kernel: "7.3.0-1-amd64", usrMerged: true},
	} {
		t.Run(l.kernel, func(t *testing.T) {
			tree, rd := isoFor(t, l, nil)
			a, err := Archive(tree, rd, target)
			if err != nil {
				t.Fatal(err)
			}
			got := names(t, a)
			mods, sbin, libs, start := "lib/modules/", "sbin/", "lib/x86_64-linux-gnu/", "lib/debian-installer-startup.d/"
			if l.usrMerged {
				mods, sbin, libs, start = "usr/lib/modules/", "usr/sbin/", "usr/lib/x86_64-linux-gnu/", "usr/lib/debian-installer-startup.d/"
			}
			for _, want := range []string{
				mods + l.kernel + "/kernel/drivers/net/e1000e.ko",
				mods + l.kernel + "/kernel/drivers/net/virtio_net.ko",
				mods + l.kernel + "/kernel/drivers/net/mii.ko",
				mods + l.kernel + "/kernel/drivers/net/r8152.ko",
				sbin + "iscsistart",
				libs + "libopeniscsiusr.so.0",     // the library, under the name its program asks for
				libs + "libopeniscsiusr.so.0.2.0", // and under its own
				"etc/bootlace-udhcpc.sh",
				start + "S12bootlace-iscsi",
			} {
				if _, ok := got[want]; !ok {
					t.Errorf("missing %s; has %v", want, sortedKeys(got))
				}
			}
			if !strings.Contains(string(got[start+"S12bootlace-iscsi"]), "'192.168.8.201'") {
				t.Error("the start-up script does not name the server")
			}
			// the drivers of another kernel are never taken
			for n := range got {
				if strings.Contains(n, "5.1.0-other") {
					t.Errorf("a package of another kernel: %s", n)
				}
			}
			// nothing replaces a folder or a link the start-up disk has: in a usr-merged one /lib and /sbin are links, and only names under usr exist
			if l.usrMerged {
				for _, n := range []string{"lib", "sbin", "usr", "usr/lib", "usr/lib/x86_64-linux-gnu", "usr/lib/debian-installer-startup.d"} {
					if _, ok := got[n]; ok {
						t.Errorf("the archive makes %q, which the start-up disk has", n)
					}
				}
			}
			// every new folder is made before the files in it
			seen := map[string]bool{}
			err = initread.Walk(bytes.NewReader(a), func(name string, size int64, body io.Reader) error {
				seen[name] = true
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func sortedKeys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestArchiveAddsALibraryTheStartUpDiskLacks(t *testing.T) {
	l := layout{kernel: "6.12.107+deb13-amd64", usrMerged: true}
	tree, rd := isoFor(t, l, func(m memTree) {
		// iscsistart now also needs libextra.so.1, which only a package open-iscsi-udeb depends on carries
		m["/pool/main/o/open-iscsi/open-iscsi-udeb_2.1.11_amd64.udeb"] = udebOf(t, map[string][]byte{
			"usr/sbin/iscsistart":                               buildELF("", "libopeniscsiusr.so.0", "libextra.so.1", "libc.so.6"),
			"usr/lib/x86_64-linux-gnu/libopeniscsiusr.so.0.2.0": buildELF("libopeniscsiusr.so.0", "libc.so.6"),
		})
		m["/pool/main/e/extra/libextra1-udeb_1_amd64.udeb"] = udebOf(t, map[string][]byte{"usr/lib/x86_64-linux-gnu/libextra.so.1": buildELF("libextra.so.1", "libc.so.6")})
		idx := "Package: nic-modules-" + l.kernel + "-di\nFilename: pool/main/l/linux/nic-modules-" + l.kernel + "-di_1_amd64.udeb\n\n" +
			"Package: open-iscsi-udeb\nFilename: pool/main/o/open-iscsi/open-iscsi-udeb_2.1.11_amd64.udeb\nDepends: libextra1-udeb\n\n" +
			"Package: libextra1-udeb\nFilename: pool/main/e/extra/libextra1-udeb_1_amd64.udeb\n\n"
		m["/dists/stable/main/debian-installer/binary-amd64/Packages.gz"] = gz(idx)
	})
	a, err := Archive(tree, rd, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := names(t, a)["usr/lib/x86_64-linux-gnu/libextra.so.1"]; !ok {
		t.Errorf("the library the start-up disk lacks is not added: %v", sortedKeys(names(t, a)))
	}

	// and when no package carries it, the archive is refused instead of made broken
	tree["/dists/stable/main/debian-installer/binary-amd64/Packages.gz"] = gz("Package: nic-modules-" + l.kernel + "-di\nFilename: pool/main/l/linux/nic-modules-" + l.kernel + "-di_1_amd64.udeb\n\n" +
		"Package: open-iscsi-udeb\nFilename: pool/main/o/open-iscsi/open-iscsi-udeb_2.1.11_amd64.udeb\n\n")
	if _, err := Archive(tree, rd, target); err == nil || !strings.Contains(err.Error(), "libextra.so.1") {
		t.Errorf("got %v, want an error naming the missing library", err)
	}
}

func TestArchiveNeedsAServer(t *testing.T) {
	tree, rd := isoFor(t, layout{kernel: "6.12.107+deb13-amd64", usrMerged: true}, nil)
	for _, bad := range []Target{{}, {Host: "h", IQN: "q"}, {Host: "h", Port: 1}, {Port: 1, IQN: "q"}} {
		if _, err := Archive(tree, rd, bad); err == nil {
			t.Errorf("%+v is accepted", bad)
		}
	}
}

func TestRemap(t *testing.T) {
	for _, c := range []struct{ name, base, kernel, want string }{
		{"lib/modules/6.1.0/kernel/a.ko", "usr/lib/modules", "6.1.0", "usr/lib/modules/6.1.0/kernel/a.ko"},
		{"usr/lib/modules/6.1.0/kernel/a.ko", "lib/modules", "6.1.0", "lib/modules/6.1.0/kernel/a.ko"},
		{"lib/modules/6.1.0/kernel/a.ko", "lib/modules", "6.1.0", "lib/modules/6.1.0/kernel/a.ko"},
		{"lib/modules/6.2.0/kernel/a.ko", "lib/modules", "6.1.0", ""}, // another kernel
		{"lib/modules/6.1.0", "lib/modules", "6.1.0", ""},             // not inside it
		{"usr/share/doc/x", "lib/modules", "6.1.0", ""},
	} {
		if got := remap(c.name, c.base, c.kernel); got != c.want {
			t.Errorf("remap(%q, %q, %q) = %q, want %q", c.name, c.base, c.kernel, got, c.want)
		}
	}
}

func TestELFHelpers(t *testing.T) {
	lib := buildELF("libx.so.1", "libc.so.6", "libkmod.so.2")
	if got := sonameOf(lib); got != "libx.so.1" {
		t.Errorf("soname %q", got)
	}
	need, err := neededLibs(lib)
	if err != nil || strings.Join(need, ",") != "libc.so.6,libkmod.so.2" {
		t.Errorf("needed %v, %v", need, err)
	}
	if got := sonameOf(buildELF("")); got != "" {
		t.Errorf("a program has a soname %q", got)
	}
	if got := sonameOf([]byte("not an elf")); got != "" {
		t.Errorf("soname of text: %q", got)
	}
	if _, err := neededLibs([]byte("not an elf")); err == nil {
		t.Error("text is read as a program")
	}
}

func TestScriptQuotesWhatItIsGiven(t *testing.T) {
	s := Script(Target{Host: "it's", Port: 3260, IQN: "iqn.x:a b"})
	for _, want := range []string{"HOST='it'''s'", "PORT=3260", "IQN='iqn.x:a b'", "INITIATOR=" + initiatorName} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	// the format verbs must be gone, and the shell's own %% stay as it is written in the text
	if strings.Contains(s, "%!") || strings.Contains(s, "%[") {
		t.Errorf("a format verb is left or misused:\n%s", s)
	}
	if !strings.Contains(s, "${mm%%:*}") {
		t.Error("the shell's own ${mm%%:*} did not come out right")
	}
	if !strings.HasPrefix(s, "#!/bin/sh\n") {
		t.Error("not a shell script")
	}
	if !strings.Contains(udhcpcScript, "ip route add default via") || !strings.HasPrefix(udhcpcScript, "#!/bin/sh\n") {
		t.Error("the DHCP handler does not set the route")
	}
}
