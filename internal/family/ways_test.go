package family

import (
	"strings"
	"testing"
)

// an Ubuntu-like menu: the ordinary entry, safe graphics, OEM install, a check of the disc and an entry that only chains to another disk
const manyEntries = `menuentry "^Try or Install NewOS" {
	linux	/newos/vmlinuz boot=casper quiet splash ---
	initrd	/newos/initrd
}
menuentry "NewOS (safe graphics)" {
	linux	/newos/vmlinuz boot=casper nomodeset quiet splash ---
	initrd	/newos/initrd
}
menuentry "OEM install (for manufacturers)" {
	linux	/newos/vmlinuz boot=casper only-ubiquity oem-config/enable=true quiet splash ---
	initrd	/newos/initrd
}
menuentry "Check disc for defects" {
	linux	/newos/vmlinuz boot=casper integrity-check quiet splash ---
	initrd	/newos/initrd
}
menuentry "Boot from next volume" {
	chainloader +1
}
`

func casperISO(menu string) memTree {
	return isoWith("/boot/grub/grub.cfg", menu, append([]string{"scripts/casper"}, modules(true, true)...)...)
}

func TestTheOtherEntriesOfAnIsosMenuAreOfferedNextToTheUsualStart(t *testing.T) {
	iso := casperISO(manyEntries)
	c := &Context{FS: iso, URLs: withNBD(urls)}
	primary, ok := DetectGeneric(c)
	if !ok {
		t.Fatal("the ISO is not recognised")
	}
	ways := Ways(c, primary)
	var titles []string
	for _, w := range ways {
		titles = append(titles, w.Title)
	}
	if got := strings.Join(titles, " | "); got != "OEM install (for manufacturers) | NewOS (safe graphics) | Check disc for defects" {
		t.Fatalf("ways: %s", got)
	}
	safe := ways[1]
	if !safe.Special || !strings.Contains(strings.Join(safe.Commands, "\n"), "nomodeset") || !strings.Contains(strings.Join(safe.Commands, "\n"), "netboot=nfs") {
		t.Errorf("safe graphics is a special way started like the usual one, with its own arguments:\n%s", strings.Join(safe.Commands, "\n"))
	}
	for _, w := range ways {
		if strings.Contains(strings.Join(w.Commands, "\n"), "integrity-check") && !w.Special {
			t.Error("a check of the disc is a special way")
		}
	}
	if strings.Contains(strings.Join(primary.Commands, "\n"), "nomodeset") {
		t.Errorf("the usual start is the ordinary entry:\n%s", strings.Join(primary.Commands, "\n"))
	}
}

func TestAnIsoWithOneEntryHasNoOtherWays(t *testing.T) {
	iso := casperISO(strings.Replace(grubMenu, "%s", "boot=casper quiet splash ---", 1))
	c := &Context{FS: iso, URLs: withNBD(urls)}
	primary, _ := DetectGeneric(c)
	if ways := Ways(c, primary); len(ways) != 0 {
		t.Fatalf("%+v", ways)
	}
}

func TestAWayWhoseStartUpDiskNeedsAnArchiveOfItsOwnIsLeftOutUnlessItIsTheUsualDisk(t *testing.T) {
	menu := `menuentry "Live" {
	linux	/live/vmlinuz boot=live quiet
	initrd	/live/initrd
}
menuentry "Live (other disk)" {
	linux	/live2/vmlinuz boot=live quiet
	initrd	/live2/initrd
}
menuentry "Live (failsafe)" {
	linux	/live/vmlinuz boot=live noapic nomodeset
	initrd	/live/initrd
}
`
	// neither start-up disk has network card drivers, so each needs its own archive
	disk := initramfs(append([]string{"scripts/live"}, modules(false, true)...)...)
	iso := memTree{"/boot/grub/grub.cfg": []byte(menu), "/live/vmlinuz": []byte("k"), "/live/initrd": disk, "/live2/vmlinuz": []byte("k"), "/live2/initrd": disk, "/live/filesystem.squashfs": []byte("x")}
	c := &Context{FS: iso, URLs: withNBD(urls)}
	primary, ok := DetectGeneric(c)
	if !ok || !primary.HasOwnExtra() {
		t.Fatalf("setup: %q %v own extra %v", primary.Family, ok, primary.HasOwnExtra())
	}
	var titles []string
	for _, w := range Ways(c, primary) {
		titles = append(titles, w.Title)
	}
	if got := strings.Join(titles, "|"); got != "Live (failsafe)" {
		t.Fatalf("only the way that uses the same start-up disk is offered: %q", got)
	}
}

func TestWayTitlesAreCleanedForAnIpxeScript(t *testing.T) {
	for in, want := range map[string]string{
		"^Try  NewOS":             "Try NewOS",
		"Install\t${net0/ip}\\ x": "Install $ {net0/ip} x",
		"":                        "Entry 3",
		strings.Repeat("a", 100):  strings.Repeat("a", 68) + "..",
	} {
		if got := wayTitle(in, 2); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestWaysAreOnlyOfferedWhenTheUsualStartIsOfTheSameKind(t *testing.T) {
	iso := casperISO(manyEntries)
	c := &Context{FS: iso, URLs: withNBD(urls)}
	installer := Boot{Family: "Red Hat family (installer)", Commands: []string{"kernel http://s/vmlinuz inst.repo=http://s/fs", "boot"}}
	if ways := Ways(c, installer); len(ways) != 0 {
		t.Fatalf("a start of another kind (an installer started by its own rules) gets no ways by the live strategy: %+v", ways)
	}
	named := Boot{Family: "Ubuntu (casper)", Kind: "casper", Commands: []string{"kernel http://s/fs/abc/newos/vmlinuz boot=casper", "boot"}}
	if ways := Ways(c, named); len(ways) != 4 {
		t.Fatalf("a named casper start gets the menu's entries by the same strategy: %+v", ways)
	}
}

func TestTwoWaysWithTheSameTitleAreToldApart(t *testing.T) {
	menu := `menuentry "Live" {
	linux	/newos/vmlinuz boot=casper quiet ---
	initrd	/newos/initrd
}
menuentry "Live (KMS)" {
	linux	/newos/vmlinuz boot=casper quiet vga=788 ---
	initrd	/newos/initrd
}
menuentry "Live (KMS)" {
	linux	/newos/vmlinuz boot=casper quiet vga=791 ---
	initrd	/newos/initrd
}
`
	iso := casperISO(menu)
	c := &Context{FS: iso, URLs: withNBD(urls)}
	primary, _ := DetectGeneric(c)
	var titles []string
	for _, w := range Ways(c, primary) {
		titles = append(titles, w.Title)
	}
	if got := strings.Join(titles, "|"); got != "Live (KMS)|Live (KMS) (2)" {
		t.Fatalf("%q", got)
	}
}
