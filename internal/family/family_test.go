package family

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// fake is a tree made of file names.
type fake map[string]bool

func (f fake) Exists(name string) bool {
	if f[name] {
		return true
	}
	for k := range f {
		if strings.HasPrefix(k, name+"/") {
			return true
		}
	}
	return false
}

func (f fake) List(name string) ([]string, error) {
	seen := map[string]bool{}
	prefix := strings.TrimRight(name, "/") + "/"
	for k := range f {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			first, more, _ := strings.Cut(rest, "/")
			if more != "" {
				first += "/"
			}
			seen[first] = true
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no such folder")
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

var urls = URLs{ISO: "http://s/iso/x.iso", FS: "http://s/fs/abc"}

func lines(b Boot) string { return strings.Join(b.Commands, "\n") }

func TestAlpineStartsFromItsOwnKernelModulesAndPackages(t *testing.T) {
	b, ok := Detect(fake{"/.alpine-release": true, "/boot/vmlinuz-virt": true, "/boot/initramfs-virt": true, "/boot/modloop-virt": true, "/apks/x86_64/APKINDEX.tar.gz": true}, urls)
	if !ok || b.Family != "Alpine Linux" {
		t.Fatalf("%v %v", b, ok)
	}
	for _, want := range []string{"kernel http://s/fs/abc/boot/vmlinuz-virt ", "alpine_repo=http://s/fs/abc/apks", "modloop=http://s/fs/abc/boot/modloop-virt", "initrd http://s/fs/abc/boot/initramfs-virt", "\nboot"} {
		if !strings.Contains(lines(b), want) {
			t.Errorf("lacks %q in %q", want, lines(b))
		}
	}
}

func TestOtherFamiliesAreToldApartByTheirFiles(t *testing.T) {
	cases := map[string]fake{
		"Debian live":                {"/live/vmlinuz-6.1": true, "/live/initrd.img-6.1": true, "/live/filesystem.squashfs": true},
		"Ubuntu (casper)":            {"/casper/vmlinuz": true, "/casper/initrd": true},
		"Arch Linux":                 {"/arch/boot/x86_64/vmlinuz-linux": true, "/arch/boot/x86_64/initramfs-linux.img": true},
		"Fedora (live)":              {"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true, "/LiveOS/squashfs.img": true},
		"Red Hat family (installer)": {"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true},
	}
	for want, tree := range cases {
		if b, ok := Detect(tree, urls); !ok || b.Family != strings.TrimSpace(want) {
			t.Errorf("%s: got %v %v", want, b.Family, ok)
		}
	}
	if _, ok := Detect(fake{"/sources/install.wim": true, "/bootmgr": true}, urls); ok {
		t.Error("a Windows ISO was taken for a Linux")
	}
}

func TestAnArchRelativeStartsFromItsOwnKernelNotTheLongTermOne(t *testing.T) {
	b, ok := Detect(fake{"/arch/boot/x86_64/vmlinuz-linux-cachyos": true, "/arch/boot/x86_64/initramfs-linux-cachyos.img": true, "/arch/boot/x86_64/vmlinuz-linux-cachyos-lts": true,
		"/arch/boot/x86_64/initramfs-linux-cachyos-lts.img": true}, urls)
	if !ok || !strings.Contains(lines(b), "vmlinuz-linux-cachyos ") || !strings.Contains(lines(b), "initramfs-linux-cachyos.img") || strings.Contains(lines(b), "-lts") {
		t.Fatalf("%v %v", b, ok)
	}
}

func TestProxmoxGetsItsIsoAsASecondStartUpDisk(t *testing.T) {
	b, _ := Detect(fake{"/.pve-cd-id.txt": true, "/boot/linux26": true, "/boot/initrd.img": true}, urls)
	if !strings.Contains(lines(b), "initrd --name /proxmox.iso http://s/iso/x.iso /proxmox.iso") {
		t.Fatal(lines(b))
	}
}

func TestFreeBSDRestartsThePcSoThatItsFirmwareStartsTheLoader(t *testing.T) {
	b, ok := Detect(fake{"/boot/kernel/kernel": true, "/boot/loader.efi": true, "/boot/pxeboot": true}, URLs{ISO: "http://s/iso/x.iso", FS: "http://s/fs/abc", Base: "http://s"})
	if !ok || !strings.HasPrefix(b.Family, "FreeBSD") || !strings.Contains(lines(b), "imgfetch --name handoff http://s/next?iso=abc&mac=${net0/mac}&ip=${net0/ip}") || !strings.Contains(lines(b), "reboot") {
		t.Fatalf("%v %v", b, ok)
	}
	if _, ok := Detect(fake{"/boot/kernel/kernel": true}, urls); ok {
		t.Fatal("a kernel without the UEFI loader is not enough")
	}
}
