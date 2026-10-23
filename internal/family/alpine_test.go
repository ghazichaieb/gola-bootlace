package family

import (
	"strings"
	"testing"
)

func TestAlpineStartsFromItsOwnKernelModulesAndPackages(t *testing.T) {
	tree := fake{"/.alpine-release": true, "/boot/vmlinuz-virt": true, "/boot/initramfs-virt": true, "/boot/modloop-virt": true, "/apks/x86_64/APKINDEX.tar.gz": true}
	b := detect(t, "alpine", tree, urls, "Alpine Linux",
		"kernel http://s/fs/abc/boot/vmlinuz-virt ", "alpine_repo=http://s/fs/abc/apks", "modloop=http://s/fs/abc/boot/modloop-virt", "initrd http://s/fs/abc/boot/initramfs-virt", "\nboot", "!rdinit")
	if b.HasOwnExtra() || b.ExtraInitrd != "" {
		t.Error("without the network disk server there is no archive to bring the drivers")
	}
}

func TestAlpineGetsTheNetworkCardDriversItsStartUpDiskLacks(t *testing.T) {
	tree := fake{"/.alpine-release": true, "/boot/vmlinuz-lts": true, "/boot/initramfs-lts": true, "/boot/modloop-lts": true}
	b := detect(t, "alpine", tree, withNBD(urls), "Alpine Linux", "rdinit=/bootlace-init", "initrd http://s/x/abc/extra.cpio")
	if !b.HasOwnExtra() || !strings.Contains(b.Args, "rdinit=/bootlace-init") {
		t.Errorf("the archive with the drivers and the script is not used: %+v", b)
	}
	// without the modloop there are no drivers to bring
	detect(t, "no modloop", fake{"/.alpine-release": true, "/boot/vmlinuz-lts": true, "/boot/initramfs-lts": true}, withNBD(urls), "Alpine Linux", "!rdinit")
}
