package bootcfg

import (
	"reflect"
	"strings"
	"testing"
)

// The menus below are pieces of the real ones of the ISOs that were tested.

const tailsGrub = `
set timeout=4
probe --set rootuuid --fs-uuid ($root)

menuentry 'Tails 7.15' --id 'live' {
	  echo "Loading the Linux kernel..."
	  linux /live/vmlinuz initrd=/live/initrd.img boot=live config live-media=removable nopersistence noprompt timezone=Etc/UTC splash noautologin module=Tails FSUUID=${rootuuid} quiet
	  echo "Loading the initramfs..."
	  initrd /live/initrd.img
}
`

const porteusSyslinux = `
PROMPT 0
UI vesamenu.c32
DEFAULT GRAPHICAL

LABEL GRAPHICAL
MENU LABEL Graphics mode
KERNEL /boot/syslinux/vmlinuz
INITRD /boot/syslinux/initrd.xz
APPEND changes=/porteus

LABEL text
MENU LABEL Text mode (safe)
KERNEL vmlinuz
APPEND initrd=initrd.xz 3 nomagic base_only norootcopy

LABEL hd
MENU LABEL Boot from the disk
KERNEL chain.c32
APPEND hd0
`

const nixosGrub = `
menuentry 'NixOS Installer GNOME (Linux LTS)' --class installer {
  linux /boot//nix/store/ipabfanvi3crlh1zf18ss5fzh6lk1glg-linux-6.18.57/bzImage ${isoboot} init=/nix/store/pyzqx738-nixos-system/init elevator=noop root=fstab splash
  initrd /boot//nix/store/fiy21fk1bjw4ywsbn9grppchjczwrg99-initrd-linux-6.18.57/initrd
}
`

const chimeraEntry = `
title   Install ChimeraOS
linux   /arch/boot/x86_64/vmlinuz-linux-chimeraos
initrd  /arch/boot/x86_64/initramfs-linux-chimeraos.img
options archisobasedir=arch archisolabel=CHIMERAOS_202502 nomodeset splash quiet nowatchdog
`

func TestGrubEntryOfTailsKeepsItsOwnArgumentsAndResolvesWhatWasSet(t *testing.T) {
	es := Parse("/EFI/debian/grub.cfg", tailsGrub, nil)
	if len(es) != 1 {
		t.Fatalf("%+v", es)
	}
	e := es[0]
	if e.Title != "Tails 7.15" || e.Kernel != "/live/vmlinuz" || !reflect.DeepEqual(e.Initrds, []string{"/live/initrd.img"}) || !strings.Contains(e.Args, "module=Tails") {
		t.Errorf("%+v", e)
	}
	// FSUUID=${rootuuid} comes from "probe", which cannot be run: it is left unresolved and dropped
	if got := Without(e.Args, func(string) bool { return false }); strings.Contains(got, "FSUUID") || !strings.Contains(got, "noautologin") {
		t.Errorf("%q", got)
	}
}

func TestSyslinuxLabelsGiveKernelsAndSkipMenusAndChains(t *testing.T) {
	es := Parse("/boot/syslinux/porteus.cfg", porteusSyslinux, nil)
	if len(es) != 2 {
		t.Fatalf("%d entries: %+v", len(es), es)
	}
	if e := es[0]; e.Title != "Graphics mode" || e.Kernel != "/boot/syslinux/vmlinuz" || !reflect.DeepEqual(e.Initrds, []string{"/boot/syslinux/initrd.xz"}) || e.Args != "changes=/porteus" {
		t.Errorf("%+v", e)
	}
	// the start-up disk named by initrd= in APPEND is taken out of the arguments
	if e := es[1]; e.Kernel != "/vmlinuz" || !reflect.DeepEqual(e.Initrds, []string{"/initrd.xz"}) || e.Args != "3 nomagic base_only norootcopy" {
		t.Errorf("%+v", e)
	}
}

func TestAMenuVariableThatWasNeverSetLeavesItsArgumentOut(t *testing.T) {
	es := Parse("/EFI/BOOT/grub.cfg", nixosGrub, nil)
	if len(es) != 1 || es[0].Kernel != "/boot/nix/store/ipabfanvi3crlh1zf18ss5fzh6lk1glg-linux-6.18.57/bzImage" {
		t.Fatalf("%+v", es)
	}
	got := Without(es[0].Args, func(string) bool { return false })
	if strings.Contains(got, "isoboot") || !strings.HasPrefix(got, "init=/nix/store/") || !strings.Contains(got, "root=fstab") {
		t.Errorf("%q", got)
	}
}

func TestASystemdBootEntryIsOneFile(t *testing.T) {
	es := Parse("/loader/entries/archiso-x86_64-linux.conf", chimeraEntry, nil)
	if len(es) != 1 || es[0].Title != "Install ChimeraOS" || es[0].Kernel != "/arch/boot/x86_64/vmlinuz-linux-chimeraos" || !strings.Contains(es[0].Args, "archisolabel=CHIMERAOS_202502") {
		t.Fatalf("%+v", es)
	}
	if got := Parse("/loader/entries/empty.conf", "title Nothing\n", nil); got != nil {
		t.Errorf("an entry with no kernel: %+v", got)
	}
}

func TestVariablesSetInOneFileAreSeenInTheOnesItIncludes(t *testing.T) {
	main := "set isoboot=\"findiso=x\"\nsource /boot/grub/kernels.cfg\n"
	sub := "menuentry \"live\" {\n linux /vmlinuz ${isoboot} quiet\n initrd /initrd\n}\n"
	es := Parse("/boot/grub/grub.cfg", main, func(name string) (string, bool) { return sub, name == "/boot/grub/kernels.cfg" })
	if len(es) != 1 || es[0].Args != "findiso=x quiet" {
		t.Errorf("%+v", es)
	}
	// a menu that includes itself ends
	loop := "LABEL a\nKERNEL /k\nINCLUDE loop.cfg\n"
	if es := Parse("/isolinux/isolinux.cfg", loop, func(string) (string, bool) { return loop, true }); len(es) == 0 {
		t.Error("the entries before the include are lost")
	}
}

func TestPathsAreMadeIntoPathsInTheIso(t *testing.T) {
	for in, want := range map[string]string{"(hd0,1)/boot/vmlinuz": "/boot/vmlinuz", "($root)/casper/vmlinuz": "/casper/vmlinuz", "/boot//nix//store/x": "/boot/nix/store/x", "vmlinuz": "/vmlinuz", "": ""} {
		if got := clean(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestWithoutDropsTheTokensItIsToldTo(t *testing.T) {
	got := Without("quiet splash findiso=/x.iso toram live-media=/dev/sr0 ---", func(t string) bool {
		return strings.HasPrefix(t, "findiso=") || t == "toram" || strings.HasPrefix(t, "live-media=")
	})
	if got != "quiet splash ---" {
		t.Errorf("%q", got)
	}
}
