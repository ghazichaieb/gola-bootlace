package family

import (
	"strings"
	"testing"
)

func TestSystemsThatNeedNothingFromTheNetworkSideAreStartedPlainly(t *testing.T) {
	detect(t, "tiny core", fake{"/boot/vmlinuz": true, "/boot/core.gz": true}, urls, "Tiny Core", "initrd http://s/fs/abc/boot/core.gz", "!extra.cpio")
	b := detect(t, "shredos", fake{"/boot/bzImage": true, "/boot/shredos.id": true}, urls, "ShredOS", "kernel http://s/fs/abc/boot/bzImage console=tty3", "!initrd")
	if b.Initrd != "" || b.Kernel != "/boot/bzImage" {
		t.Errorf("a kernel that holds its own start-up disk: %+v", b)
	}
}

func TestLakkaReadsItsBootPartitionFromTheNetworkDiskOnly(t *testing.T) {
	tree := fake{"/KERNEL": true, "/SYSTEM": true, "/syslinux.cfg": true}
	b := detect(t, "lakka", tree, withNBD(urls), "Lakka / LibreELEC", "boot=NBD=10.0.0.1:10809 ip=dhcp rdinit=/bootlace-init", "initrd http://s/x/abc/extra.cpio", "!initrd http://s/fs")
	if !b.HasOwnExtra() {
		t.Error("the rdinit script and the client are an archive of their own")
	}
	if _, ok := Detect(tree, urls); ok {
		t.Error("recognised although the kernel cannot see a disk that the firmware reads")
	}
	if b, _ := Detect(fake{"/KERNEL": true, "/SYSTEM": true, "/live/filesystem.squashfs": true}, withNBD(urls)); b.Family == "Lakka / LibreELEC" {
		t.Error("a live system with a file called KERNEL is not LibreELEC")
	}
}

func TestLakkasArchiveNamesTheExportAndRedirectsTheClientCall(t *testing.T) {
	data, err := libreELECExtra(ExtraEnv{Settings: settings("abc@1", 512), Helper: []byte("ELF")})
	if err != nil {
		t.Fatal(err)
	}
	a := string(data)
	for _, want := range []string{"nbd0 abc@1 512\n", "usr/bin/nbd-client", "bootlace-init", "sed -i 's#nbd-client $NBD_SERVER#/usr/bin/nbd-client $NBD_SERVER#' /init", "exec /init", "etc/bootlace-nbd"} {
		if !strings.Contains(a, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

func TestPorteusGetsItsIsoAsAFileInAFolderTheArchiveMakes(t *testing.T) {
	tree := fake{"/boot/syslinux/vmlinuz": true, "/boot/syslinux/initrd.xz": true, "/porteus/base/001-core.xzm": true}
	b := detect(t, "porteus", tree, withNBD(urls), "Porteus (whole ISO in memory)", "from=/porteus.iso", "initrd --name /mnt/bootlace/porteus.iso http://s/iso/x.iso /mnt/bootlace/porteus.iso mode=0644")
	// the folder comes first: a file in a folder that is not there is dropped without a word
	cmds := b.Commands
	if !strings.HasPrefix(cmds[0], "echo Loading Porteus") || cmds[len(cmds)-1] != "boot" || !strings.HasPrefix(cmds[len(cmds)-3], "initrd http://s/x/abc/extra.cpio") {
		t.Errorf("%v", cmds)
	}
	data, _ := b.BuildExtra(ExtraEnv{})
	if !strings.Contains(string(data), "mnt/bootlace\x00") {
		t.Error("the archive does not make the folder")
	}
	if _, ok := Detect(tree, urls); ok {
		t.Error("recognised although there is no archive to make the folder")
	}
}

func TestProxmoxReadsItsIsoOverTheNetworkWhenItCan(t *testing.T) {
	tree := fake{"/.pve-cd-id.txt": true, "/boot/linux26": true, "/boot/initrd.img": true, "/pve-installer.squashfs": true}
	b := detect(t, "network disk", tree, withNBD(urls), "Proxmox VE", "initrd http://s/x/abc/extra.cpio", "ro rw quiet", "!/proxmox.iso")
	if !b.HasOwnExtra() {
		t.Error("the installer's start-up script is changed in an archive of its own")
	}
	detect(t, "whole ISO", tree, urls, "Proxmox VE", "initrd --name /proxmox.iso http://s/iso/x.iso /proxmox.iso", "ramdisk_size=16777216")
}

func TestTheProxmoxStartUpScriptGetsTheNetworkLinesBeforeItLooksForTheIso(t *testing.T) {
	script := "#!/bin/sh\necho hi\n# we have no iscsi daemon, so we need to scan iscsi device manually.\nfor i in /sys/block/hd* /sys/block/nvme*; do\n    echo $i\ndone\n"
	out, ok := patchProxmoxInit(script, settings("abc", 0))
	if !ok {
		t.Fatal("not patched")
	}
	for _, want := range []string{"/bootlace/bootlace-nbd -host '10.0.0.1' -port 10809 -name 'abc'", "/sys/block/nvme* /sys/block/nbd0; do", "modprobe -q nbd", "if [ -x /bootlace/bootlace-nbd ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q in\n%s", want, out)
		}
	}
	if strings.Index(out, "bootlace-nbd -host") > strings.Index(out, "# we have no iscsi daemon") {
		t.Error("the network must come up before the search for the ISO")
	}
	if _, ok := patchProxmoxInit("#!/bin/sh\necho unrelated\n", settings("abc", 0)); ok {
		t.Error("a script without the anchors must be left alone")
	}
}
