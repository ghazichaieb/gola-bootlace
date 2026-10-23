package family

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// initramfs is a start-up disk with the named files (and nothing in them).
func initramfs(names ...string) []byte {
	var files []initrd.File
	for _, n := range names {
		files = append(files, initrd.File{Name: n, Mode: 0o644, Data: []byte("x")})
	}
	return initrd.Archive(files)
}

// modules are the names of the files of kernel modules: the network cards that start-up disks usually carry, and the network disk.
func modules(withNICs, withNBD bool) []string {
	var out []string
	if withNICs {
		for _, m := range []string{"e1000e", "r8169", "igb", "virtio_net"} {
			out = append(out, "usr/lib/modules/6.1.0/kernel/drivers/net/"+m+".ko")
		}
	}
	if withNBD {
		out = append(out, "usr/lib/modules/6.1.0/kernel/drivers/block/nbd.ko")
	}
	return append(out, "usr/lib/modules/6.1.0/kernel/fs/isofs.ko")
}

// isoWith is an ISO of a system that no family knows by name: a boot menu, a kernel and a start-up disk.
func isoWith(menuFile, menu string, initrdNames ...string) memTree {
	return memTree{
		menuFile:             []byte(menu),
		"/newos/vmlinuz":     []byte("kernel"),
		"/newos/initrd":      initramfs(initrdNames...),
		"/newos/live.squash": []byte("x"),
	}
}

const grubMenu = "menuentry \"Try NewOS\" {\n\tlinux\t/newos/vmlinuz %s\n\tinitrd\t/newos/initrd\n}\n"

func generic(t *testing.T, iso memTree, u URLs) (Boot, bool) {
	t.Helper()
	return DetectGeneric(&Context{FS: iso, URLs: u})
}

func TestACasperLikeIsoIsStartedOnTheNetworkDiskWithItsOwnArguments(t *testing.T) {
	iso := isoWith("/boot/grub/grub.cfg", strings.Replace(grubMenu, "%s", "boot=casper findiso=/x.iso toram quiet splash ---", 1), append([]string{"scripts/casper"}, modules(true, true)...)...)
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !strings.HasPrefix(b.Family, "Ubuntu-style (casper)") {
		t.Fatalf("%q %v", b.Family, ok)
	}
	for _, want := range []string{"boot=casper netboot=nfs nfsroot=10.0.0.1:/abc", "live-media-path=/newos", "quiet splash ---", "kernel http://s/fs/abc/newos/vmlinuz ", "initrd http://s/x/abc/extra.cpio"} {
		if !strings.Contains(lines(b), want) {
			t.Errorf("lacks %q\n%s", want, lines(b))
		}
	}
	for _, unwanted := range []string{"findiso", "toram"} { // they look for the ISO on a disk
		if strings.Contains(lines(b), unwanted) {
			t.Errorf("has %q\n%s", unwanted, lines(b))
		}
	}
	if b.HasOwnExtra() {
		t.Error("a start-up disk with network card drivers needs only the usual archive")
	}
}

func TestAStartUpDiskWithoutNetworkCardDriversGetsThemInAnArchiveOfItsOwn(t *testing.T) {
	menu := strings.NewReplacer("/newos/", "/live/", "%s", "boot=live quiet").Replace(grubMenu) // (the usual layout of live-boot: the network disk is used for it)
	iso := memTree{
		"/boot/grub/grub.cfg":       []byte(menu),
		"/live/vmlinuz":             []byte("kernel"),
		"/live/initrd":              initramfs(append([]string{"scripts/live"}, modules(false, true)...)...),
		"/live/filesystem.squashfs": []byte("x"),
	}
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !b.HasOwnExtra() {
		t.Fatalf("%q %v own=%v", b.Family, ok, b.HasOwnExtra())
	}
}

func TestLiveBootWithoutTheNetworkDiskDriverFetchesOnlyTheSystemImage(t *testing.T) {
	iso := isoWith("/boot/grub/grub.cfg", strings.Replace(grubMenu, "%s", "boot=live quiet", 1), append([]string{"scripts/live"}, modules(true, false)...)...)
	iso["/live/filesystem.squashfs"] = []byte("x")
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !strings.HasPrefix(b.Family, "Debian-style live") {
		t.Fatalf("%q %v", b.Family, ok)
	}
	if !strings.Contains(lines(b), "fetch=http://s/fs/abc/live/filesystem.squashfs") || strings.Contains(lines(b), "netboot") || strings.Contains(lines(b), "extra.cpio") {
		t.Errorf("%s", lines(b))
	}
}

func TestAnArchisoWithTheNetworkHooksUsesThemAndReadsItsFolderFromTheMenu(t *testing.T) {
	menu := "LABEL arch\nMENU LABEL Boot\nLINUX /newos/vmlinuz\nINITRD /newos/initrd\nAPPEND archisobasedir=custom archisolabel=NEW_2026 cow_spacesize=4G\n"
	iso := isoWith("/isolinux/isolinux.cfg", menu, append([]string{"hooks/archiso", "hooks/archiso_pxe_nbd"}, modules(true, true)...)...)
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !strings.HasPrefix(b.Family, "Arch-style (archiso)") {
		t.Fatalf("%q %v", b.Family, ok)
	}
	for _, want := range []string{"archisobasedir=custom archiso_nbd_srv=10.0.0.1", "cow_spacesize=4G"} {
		if !strings.Contains(lines(b), want) {
			t.Errorf("lacks %q\n%s", want, lines(b))
		}
	}
	if strings.Contains(lines(b), "archisolabel") {
		t.Error("the label would make it look for the ISO on a disk")
	}
}

func TestAnArchisoWithoutNetworkHooksGetsAHookOfItsOwn(t *testing.T) {
	menu := "title New\nlinux /newos/vmlinuz\ninitrd /newos/initrd\noptions archisobasedir=arch archisolabel=X quiet\n"
	iso := isoWith("/loader/entries/new.conf", menu, append([]string{"hooks/archiso", "hooks/archiso_loop_mnt"}, modules(true, true)...)...)
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !strings.HasPrefix(b.Family, "Arch-style (archiso, no network hooks)") || !b.HasOwnExtra() {
		t.Fatalf("%q %v own=%v", b.Family, ok, b.HasOwnExtra())
	}
	if !strings.Contains(lines(b), "archisodevice=/dev/nbd0 copytoram=n quiet") {
		t.Errorf("%s", lines(b))
	}
	if _, ok := generic(t, iso, urls); ok {
		t.Error("recognised although nothing can bring the ISO to it")
	}
}

func TestADracutLiveIsoUsesWhatItsStartUpDiskCanDo(t *testing.T) {
	menu := "menuentry \"New\" {\n linux /newos/vmlinuz root=live:CDLABEL=NEW rd.live.image rd.live.check quiet enforcing=0\n initrd /newos/initrd\n}\n"
	base := []string{"usr/bin/dracut-cmdline", "usr/bin/dmsquash-live-root"}
	both := withISCSI(withNBD(urls))
	iso := isoWith("/boot/grub/grub.cfg", menu, append(append([]string{"usr/bin/netroot", "usr/bin/iscsiroot"}, base...), modules(true, true)...)...)
	b, ok := generic(t, iso, both)
	if !ok || !strings.Contains(lines(b), "netroot=nbd:10.0.0.1:10809:abc root=live:/dev/nbd0") || !strings.Contains(lines(b), "enforcing=0") || strings.Contains(lines(b), "CDLABEL") || strings.Contains(lines(b), "rd.live.check") {
		t.Fatalf("%v\n%s", ok, lines(b))
	}
	// with no network root handler, the iSCSI client is what is left
	iso = isoWith("/boot/grub/grub.cfg", menu, append(append([]string{"usr/bin/iscsiroot"}, base...), modules(true, true)...)...)
	if b, ok := generic(t, iso, both); !ok || !strings.Contains(lines(b), "netroot=iscsi:") || strings.Contains(lines(b), "extra.cpio") {
		t.Errorf("%v\n%s", ok, lines(b))
	}
	// with neither and no way to fetch the image, there is nothing to offer
	iso = isoWith("/boot/grub/grub.cfg", menu, append(append([]string{}, base...), modules(true, true)...)...)
	if _, ok := generic(t, iso, both); ok {
		t.Error("recognised although the start-up disk can reach the ISO in no way the server offers")
	}
	// one that can fetch a live image over HTTP can be given the whole ISO
	iso = isoWith("/boot/grub/grub.cfg", menu, append(append([]string{"usr/bin/livenetroot"}, base...), modules(true, true)...)...)
	if b, ok := generic(t, iso, urls); !ok || !strings.Contains(lines(b), "root=live:http://s/iso/x.iso") {
		t.Errorf("%v\n%s", ok, lines(b))
	}
}

func TestWhatIsNotOfAKnownMakeIsLeftAlone(t *testing.T) {
	menu := strings.Replace(grubMenu, "%s", "quiet", 1)
	if _, ok := generic(t, isoWith("/boot/grub/grub.cfg", menu, modules(true, true)...), withNBD(urls)); ok {
		t.Error("a start-up disk of no known make was recognised")
	}
	if _, ok := generic(t, memTree{"/newos/vmlinuz": []byte("k")}, withNBD(urls)); ok {
		t.Error("an ISO with no boot menu was recognised")
	}
	// a menu whose kernel is not in the ISO
	iso := isoWith("/boot/grub/grub.cfg", strings.Replace(grubMenu, "/newos/vmlinuz", "/gone/vmlinuz", 1), "scripts/casper")
	if _, ok := generic(t, iso, withNBD(urls)); ok {
		t.Error("an entry for a kernel that is not there was started")
	}
}

func TestTheOrdinaryEntryComesBeforeTheSafeGraphicsOne(t *testing.T) {
	menu := "menuentry \"NewOS (safe graphics)\" {\n linux /newos/vmlinuz nomodeset boot=casper\n initrd /newos/initrd\n}\n" +
		"menuentry \"NewOS\" {\n linux /newos/vmlinuz boot=casper quiet\n initrd /newos/other\n}\n"
	iso := isoWith("/boot/grub/grub.cfg", menu, append([]string{"scripts/casper"}, modules(true, true)...)...)
	iso["/newos/other"] = iso["/newos/initrd"]
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || strings.Contains(lines(b), "nomodeset") || !strings.Contains(lines(b), "initrd http://s/fs/abc/newos/other") {
		t.Fatalf("%v\n%s", ok, lines(b))
	}
}

func TestAMicrocodeFileInFrontOfTheStartUpDiskIsLeftOut(t *testing.T) {
	menu := "LABEL a\nLINUX /newos/vmlinuz\nINITRD /newos/intel-ucode.img,/newos/initrd\nAPPEND boot=live\n"
	iso := isoWith("/isolinux/isolinux.cfg", menu, append([]string{"scripts/live"}, modules(true, true)...)...)
	iso["/newos/intel-ucode.img"] = []byte("u")
	iso["/live/filesystem.squashfs"] = []byte("x")
	iso["/live/vmlinuz"] = iso["/newos/vmlinuz"]
	b, ok := generic(t, iso, withNBD(urls))
	if !ok || !strings.Contains(lines(b), "initrd http://s/fs/abc/newos/initrd\n") {
		t.Fatalf("%v\n%s", ok, lines(b))
	}
}

func TestTheNamedFamiliesComeFirstAndTheGenericOneIsNotTriedForThem(t *testing.T) {
	// Ubuntu is known by name: Detect never looks into a start-up disk, which takes seconds
	if b, ok := Detect(fake{"/casper/vmlinuz": true, "/casper/initrd": true}, withNBD(urls)); !ok || b.Family != "Ubuntu (casper)" {
		t.Errorf("%q %v", b.Family, ok)
	}
}
