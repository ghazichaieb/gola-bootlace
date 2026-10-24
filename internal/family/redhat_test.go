package family

import (
	"reflect"
	"strings"
	"testing"
)

func TestVerboseArgumentsFollowTheDetectedBootStrategy(t *testing.T) {
	for _, tc := range []struct {
		name string
		tree fake
		want []string
	}{
		{"fedora", fake{"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true, "/LiveOS/squashfs.img": true}, []string{"rd.debug"}},
		{"redhat installer", fake{"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true}, []string{"rd.debug"}},
		{"gentoo", fake{"/boot/gentoo": true, "/boot/gentoo.igz": true, "/image.squashfs": true}, []string{"rd.debug"}},
		{"proxmox", fake{"/.pve-cd-id.txt": true, "/boot/linux26": true, "/boot/initrd.img": true, "/pve-installer.squashfs": true}, []string{"proxdebug"}},
		{"tinycore", fake{"/boot/vmlinuz": true, "/boot/core.gz": true}, nil},
	} {
		for _, u := range []URLs{urls, withNBD(urls), withISCSI(urls)} {
			b, ok := Detect(tc.tree, u)
			if !ok || !reflect.DeepEqual(b.VerboseArgs, tc.want) {
				t.Fatalf("%s: detected=%v verbose args=%v, want %v", tc.name, ok, b.VerboseArgs, tc.want)
			}
			for _, arg := range tc.want {
				if strings.Contains(strings.Join(b.Commands, " "), arg) || strings.Contains(b.Args, arg) {
					t.Fatalf("%s: debug enabled in an ordinary boot", tc.name)
				}
			}
		}
	}
}

func TestADracutLiveIsoUsesTheNetworkDiskThenIscsiThenTheWholeIso(t *testing.T) {
	tree := fake{"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true, "/LiveOS/squashfs.img": true}
	both := withISCSI(withNBD(urls))
	detect(t, "network disk", tree, both, "Fedora (live)", "netroot=nbd:10.0.0.1:10809:abc root=live:/dev/nbd0", "initrd http://s/x/abc/extra.cpio", "enforcing=0", "!netroot=iscsi")
	detect(t, "iSCSI", tree, withISCSI(urls), "Fedora (live)", "netroot=iscsi:10.0.0.1::3260::iqn.x:abc", "root=live:/dev/disk/by-path/ip-10.0.0.1:3260-iscsi-iqn.x:abc-lun-0", "!extra.cpio")
	detect(t, "whole ISO", tree, urls, "Fedora (live)", "root=live:http://s/iso/x.iso", "!netroot")
}

func TestFedorasKiwiLiveIsoHasItsFilesInTheLoaderFolder(t *testing.T) {
	tree := fake{"/boot/x86_64/loader/linux": true, "/boot/x86_64/loader/initrd": true, "/LiveOS/squashfs.img": true}
	detect(t, "fedora 44", tree, withISCSI(urls), "Fedora (live)", "netroot=iscsi:", "kernel http://s/fs/abc/boot/x86_64/loader/linux", "!enforcing=0")
	// the same files with a .treeinfo are openSUSE's, not Fedora's
	if b, _ := Detect(fake{"/boot/x86_64/loader/linux": true, "/boot/x86_64/loader/initrd": true, "/LiveOS/squashfs.img": true, "/.treeinfo": true}, urls); b.Family == "Fedora (live)" {
		t.Error("an openSUSE-style tree is not Fedora")
	}
}

func TestAnInstallerOfTheRedHatFamilyReadsItsPackagesFromTheWebServer(t *testing.T) {
	detect(t, "installer", fake{"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true}, urls, "Red Hat family (installer)", "inst.repo=http://s/fs/abc")
}

func TestGentooLiveIsADracutLiveSystemWithItsOwnImageName(t *testing.T) {
	tree := fake{"/boot/gentoo": true, "/boot/gentoo.igz": true, "/image.squashfs": true}
	detect(t, "gentoo", tree, withNBD(urls), "Gentoo (live)", "netroot=nbd:10.0.0.1:10809:abc root=live:/dev/nbd0", "rd.live.squashimg=image.squashfs", "kernel http://s/fs/abc/boot/gentoo ")
}

func TestOpenSUSEReadsPackagesFromTheIsoOnlyWhenTheIsoHasThem(t *testing.T) {
	base := fake{"/boot/x86_64/loader/linux": true, "/boot/x86_64/loader/initrd": true, "/.treeinfo": true}
	detect(t, "net", base, urls, "openSUSE", "netsetup=dhcp", "!install=")
	dvd := fake{"/boot/x86_64/loader/linux": true, "/boot/x86_64/loader/initrd": true, "/.treeinfo": true, "/repodata/repomd.xml": true}
	detect(t, "dvd", dvd, urls, "openSUSE", "install=http://s/fs/abc/")
}
