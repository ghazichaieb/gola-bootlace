package httpd

import (
	"strings"
	"testing"
)

func TestTheGrubConfigurationStartsTheIsosKernelOverHttp(t *testing.T) {
	cfg := string(grubConfig("http://10.0.0.1:8080/fs/abc", "http://10.0.0.1:8080/iso/u.iso", "/casper/vmlinuz", "/casper/initrd", "http://10.0.0.1:8080/x/abc/extra.cpio", "ip=dhcp url=http://10.0.0.1:8080/iso/u.iso boot=casper"))
	for _, want := range []string{"set timeout=0", "linux (http,10.0.0.1:8080)/fs/abc/casper/vmlinuz ip=dhcp url=http://10.0.0.1:8080/iso/u.iso boot=casper ---", "initrd (http,10.0.0.1:8080)/fs/abc/casper/initrd (http,10.0.0.1:8080)/x/abc/extra.cpio"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("missing %q in:\n%s", want, cfg)
		}
	}
}

func TestShimAndGrubNamesAreMappedToTheIso(t *testing.T) {
	for in, want := range map[string]string{"bootx64.efi": "EFI/boot/bootx64.efi", "shimx64.efi": "EFI/boot/bootx64.efi", `\EFI\boot\mmx64.efi`: "EFI/boot/mmx64.efi", "casper/vmlinuz": "casper/vmlinuz"} {
		if got := ubuntuName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
