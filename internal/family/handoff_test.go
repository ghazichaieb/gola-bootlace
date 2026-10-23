package family

import (
	"strings"
	"testing"
)

func TestWindowsMediaAreStartedThroughTheirOwnScript(t *testing.T) {
	u := urls
	u.Win = "http://s/w/abc"
	boot := []string{"/sources/boot.wim", "/boot/boot.sdi", "/efi/boot/bootx64.efi", "/efi/microsoft/boot/bcd"}
	tree := func(more ...string) fake {
		f := fake{}
		for _, n := range append(boot, more...) {
			f[n] = true
		}
		return f
	}
	detect(t, "disc", tree("/sources/install.wim"), u, "Windows setup", "chain http://s/w/abc/boot.ipxe")
	detect(t, "disc with an esd", tree("/sources/install.esd"), u, "Windows setup", "chain http://s/w/abc/boot.ipxe")
	detect(t, "pe", tree(), u, "Windows PE", "chain http://s/w/abc/boot.ipxe")
	if _, ok := Detect(fake{"/sources/boot.wim": true, "/sources/install.wim": true}, u); ok {
		t.Error("a tree without the boot files of Windows is taken for Windows media")
	}
}

func TestAWindowsPeWithoutAnInstallImageStartsAsAnIscsiDisc(t *testing.T) {
	u := withISCSI(urls)
	u.Win = "http://s/w/abc"
	pe := fake{"/sources/boot.wim": true, "/boot/boot.sdi": true, "/efi/boot/bootx64.efi": true, "/efi/microsoft/boot/bcd": true}
	detect(t, "pe over iSCSI", pe, u, "Windows PE", "sanboot --no-describe --drive 0x80 iscsi:10.0.0.1", "!chain")
}

func TestFreeBSDRestartsThePcSoThatItsFirmwareStartsTheLoader(t *testing.T) {
	u := urls
	b := detect(t, "freebsd", fake{"/boot/kernel/kernel": true, "/boot/loader.efi": true, "/boot/pxeboot": true}, u, "FreeBSD, experimental",
		"imgfetch --name handoff http://s/next?iso=abc&mac=${net0/mac}&ip=${net0/ip}", "reboot")
	// the menu adds "|| goto failed" to every line: the line that stops a BIOS PC must not fail on a UEFI PC (where iseq is false)
	for _, c := range b.Commands {
		if strings.HasPrefix(c, "iseq ${platform} pcbios") && !strings.HasSuffix(c, "|| echo") {
			t.Fatalf("the BIOS check fails on a UEFI PC: %q", c)
		}
	}
	if _, ok := Detect(fake{"/boot/kernel/kernel": true}, urls); ok {
		t.Fatal("a kernel without the UEFI loader is not enough")
	}
}
