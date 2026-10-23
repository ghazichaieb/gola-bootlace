package family

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

func TestAnArchRelativeStartsFromItsOwnKernelNotTheLongTermOne(t *testing.T) {
	tree := fake{"/arch/boot/x86_64/vmlinuz-linux-cachyos": true, "/arch/boot/x86_64/initramfs-linux-cachyos.img": true, "/arch/boot/x86_64/vmlinuz-linux-cachyos-lts": true,
		"/arch/boot/x86_64/initramfs-linux-cachyos-lts.img": true}
	detect(t, "cachyos", tree, urls, "Arch Linux", "vmlinuz-linux-cachyos ", "initramfs-linux-cachyos.img", "!-lts")
}

func TestArchisoAttachesTheIsoAsANetworkDiskOnTheStandardPort(t *testing.T) {
	tree := fake{"/sysresccd/boot/x86_64/vmlinuz": true, "/sysresccd/boot/x86_64/sysresccd.img": true}
	u := withNBD(urls)
	u.NBDName = "abc1234567"
	detect(t, "standard port", tree, u, "SystemRescue", "archisobasedir=sysresccd archiso_nbd_srv=10.0.0.1 archiso_nbd_name=abc1234567 copytoram=n", "initrd http://s/x/abc/extra.cpio")
	u.NBDPort = 10810 // its client takes no port: the squashfs comes over HTTP then
	detect(t, "other port", tree, u, "SystemRescue", "archiso_http_srv=", "!archiso_nbd_srv", "!extra.cpio")
	detect(t, "no network disk", tree, urls, "SystemRescue", "archiso_http_srv=http://s/fs/abc/")
}

func TestAnArchisoWhoseKernelIsOnlyInTheEfiImageGetsAHookOfItsOwn(t *testing.T) {
	esp := fake{"/arch/boot/x86_64/vmlinuz-linux-chimeraos": true, "/arch/boot/x86_64/initramfs-linux-chimeraos.img": true}
	iso := fake{"/arch/x86_64/airootfs.sfs": true, "/loader/loader.conf": true}
	vol := func() []Volume { return []Volume{{esp, "http://s/fs/abc@2"}} }
	c := &Context{FS: iso, URLs: withNBD(urls), Volumes: vol}
	b, ok := DetectIn(c)
	if !ok || b.Family != "Arch Linux (kernel in the EFI image)" {
		t.Fatalf("%q %v", b.Family, ok)
	}
	for _, want := range []string{"kernel http://s/fs/abc@2/arch/boot/x86_64/vmlinuz-linux-chimeraos ", "archisodevice=/dev/nbd0", "initrd http://s/fs/abc@2/arch/boot/x86_64/initramfs-linux-chimeraos.img", "initrd http://s/x/abc/extra.cpio"} {
		if !strings.Contains(lines(b), want) {
			t.Errorf("lacks %q\n%s", want, lines(b))
		}
	}
	if !b.HasOwnExtra() {
		t.Error("the hook and the drivers are an archive of its own")
	}
	// without the network disk the start-up disk has no way to find the ISO
	if _, ok := DetectIn(&Context{FS: iso, URLs: urls, Volumes: vol}); ok {
		t.Error("recognised although nothing can bring the ISO to it")
	}
	// an ISO without the system image of an archiso does not count
	if _, ok := DetectIn(&Context{FS: fake{"/loader/loader.conf": true}, URLs: withNBD(urls), Volumes: vol}); ok {
		t.Error("recognised although it is not an archiso")
	}
}

func TestTheMkinitcpioHookIsAFunctionThatRunsTheNetworkScript(t *testing.T) {
	h := mkinitcpioHook(initrd.NetworkScript{Helper: "/usr/bin/bootlace-nbd", Dir: "/bootlace", Host: "h", Port: 1, Name: "n", Say: "msg", Fail: "err"})
	if !strings.HasPrefix(h, "#!/usr/bin/ash\nrun_hook() {\n") || !strings.HasSuffix(h, "}\n") || !strings.Contains(h, "bootlace-nbd -host") {
		t.Errorf("%s", h)
	}
}
