package family

import (
	"strings"
	"testing"
)

func TestCasperReadsTheIsoFromTheNetworkDiskWhenItCan(t *testing.T) {
	tree := fake{"/casper/vmlinuz": true, "/casper/initrd": true}
	b := detect(t, "network disk", tree, withNBD(urls), "Ubuntu (casper)",
		"netboot=nfs nfsroot=10.0.0.1:/abc", "cloud-init=disabled", "initrd http://s/x/abc/extra.cpio", "!url=")
	if b.HasOwnExtra() {
		t.Error("casper needs only the usual archive")
	}
	detect(t, "no network disk", tree, urls, "Ubuntu (casper)", "url=http://s/iso/x.iso boot=casper", "cloud-init=disabled", "!netboot", "!extra.cpio")
}

func TestCloudInitStaysOnForTheServerIsos(t *testing.T) {
	// casper hands a server ISO the network settings of the start-up, which the network disk needs kept
	tree := fake{"/casper/vmlinuz": true, "/casper/initrd": true, "/casper/ubuntu-server-minimal.squashfs": true}
	detect(t, "server", tree, withNBD(urls), "Ubuntu (casper)", "netboot=nfs", "!cloud-init=disabled")
}

func TestPopOSKeepsItsLiveSystemInAFolderOfItsOwnName(t *testing.T) {
	tree := fake{"/casper_pop-os_24.04/vmlinuz.efi": true, "/casper_pop-os_24.04/initrd.gz": true, "/casper_pop-os_24.04/filesystem.squashfs": true}
	detect(t, "pop", tree, withNBD(urls), "Ubuntu (casper)", "live-media-path=/casper_pop-os_24.04", "kernel http://s/fs/abc/casper_pop-os_24.04/vmlinuz.efi")
}

func TestDebianLiveReadsTheIsoFromTheNetworkDiskOrFetchesOnlyTheSystemImage(t *testing.T) {
	tree := fake{"/live/vmlinuz-6.1": true, "/live/initrd.img-6.1": true, "/live/filesystem.squashfs": true}
	detect(t, "network disk", tree, withNBD(urls), "Debian live", "boot=live components netboot=nfs nfsroot=10.0.0.1:/abc", "initrd http://s/x/abc/extra.cpio")
	detect(t, "no network disk", tree, urls, "Debian live", "ip=dhcp fetch=http://s/fs/abc/live/filesystem.squashfs", "!netboot")
}

func TestVyOSFetchesItsSystemImageAndPassesItsOwnOptions(t *testing.T) {
	// its start-up disk has no network block disk driver, and without noautologin its console logs in as a user that does not exist
	tree := fake{"/live/filesystem.squashfs": true, "/live/vmlinuz-6.18.57-vyos": true, "/live/initrd.img-6.18.57-vyos": true}
	detect(t, "vyos", tree, withNBD(urls), "Debian live", "fetch=http://s/fs/abc/live/filesystem.squashfs", "noautologin", "union=overlay", "!netboot")
}

func TestADebianLiveKernelAtTheTopOfTheIsoIsFetchedNotAttached(t *testing.T) {
	// (TrueNAS SCALE keeps it there, and its start-up disk is not the one that the network disk's mount command stands in for)
	tree := fake{"/live/filesystem.squashfs": true, "/vmlinuz": true, "/initrd.img": true}
	detect(t, "truenas", tree, withNBD(urls), "Debian live", "kernel http://s/fs/abc/vmlinuz", "fetch=", "!netboot")
}

func TestTailsIsStartedWithItsOwnArgumentsAndTheNetworkDisk(t *testing.T) {
	tree := fake{"/live/vmlinuz": true, "/live/initrd.img": true, "/live/filesystem.squashfs": true, "/live/Tails.module": true}
	b := detect(t, "network disk", tree, withNBD(urls), "Tails", "module=Tails", "netboot=nfs nfsroot=10.0.0.1:/abc", "slab_nomerge", "noautologin", "!live-media")
	if !b.HasOwnExtra() {
		t.Error("Tails needs the network card drivers in its archive")
	}
	detect(t, "no network disk", tree, urls, "Tails", "module=Tails", "fetch=http://s/fs/abc/live/filesystem.squashfs", "!netboot")
	// a Debian live system without the module name of Tails is not Tails
	if b, _ := Detect(fake{"/live/vmlinuz": true, "/live/initrd.img": true, "/live/filesystem.squashfs": true}, urls); b.Family != "Debian live" {
		t.Errorf("recognised as %q", b.Family)
	}
}

func TestTheInitTopScriptLoadsTheDriversWithTheHelper(t *testing.T) {
	for _, want := range []string{"#!/bin/sh", "prereqs", "/usr/bin/bootlace-nbd insmod /bootlace/modules/*", "exit 0"} {
		if !strings.Contains(initTopLoader, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

func TestDebiansInstallerPrefersTheGraphicalStartUpDisk(t *testing.T) {
	detect(t, "gtk", fake{"/install.amd/vmlinuz": true, "/install.amd/initrd.gz": true, "/install.amd/gtk/initrd.gz": true}, urls, "Debian installer", "/install.amd/gtk/initrd.gz")
	detect(t, "text", fake{"/install.amd/vmlinuz": true, "/install.amd/initrd.gz": true}, urls, "Debian installer", "initrd http://s/fs/abc/install.amd/initrd.gz")
	if _, ok := Detect(fake{"/install.amd/vmlinuz": true}, urls); ok {
		t.Error("a kernel with no start-up disk is not an installer")
	}
}
