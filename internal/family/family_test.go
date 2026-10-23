package family

import (
	"strings"
	"testing"
)

func TestNothingIsRecognisedFromAnUnknownTree(t *testing.T) {
	if b, ok := Detect(fake{"/readme.txt": true}, urls); ok {
		t.Fatalf("recognised as %q", b.Family)
	}
}

func TestFamiliesAreToldApartByTheirFiles(t *testing.T) {
	for want, tree := range map[string]fake{
		"Debian live":                {"/live/vmlinuz-6.1": true, "/live/initrd.img-6.1": true, "/live/filesystem.squashfs": true},
		"Ubuntu (casper)":            {"/casper/vmlinuz": true, "/casper/initrd": true},
		"Arch Linux":                 {"/arch/boot/x86_64/vmlinuz-linux": true, "/arch/boot/x86_64/initramfs-linux.img": true},
		"Fedora (live)":              {"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true, "/LiveOS/squashfs.img": true},
		"Red Hat family (installer)": {"/images/pxeboot/vmlinuz": true, "/images/pxeboot/initrd.img": true},
	} {
		detect(t, want, tree, urls, want)
	}
	if _, ok := Detect(fake{"/sources/install.wim": true, "/bootmgr": true}, urls); ok {
		t.Error("a Windows ISO without its boot files was taken for a system")
	}
}

func TestTheCommandsAreKernelThenStartUpDiskThenBoot(t *testing.T) {
	b := newBoot("X", files{"http://s/fs/abc", "/k", "/i"}, "a=1 b=2")
	if got := lines(b); got != "kernel http://s/fs/abc/k a=1 b=2\ninitrd http://s/fs/abc/i\nboot" {
		t.Errorf("%s", got)
	}
	if b.Kernel != "/k" || b.Initrd != "/i" || b.Args != "a=1 b=2" {
		t.Errorf("the files for a signed GRUB are not kept: %+v", b)
	}
	if got := lines(newBoot("X", files{"http://s/fs/abc", "/k", ""}, "a")); got != "kernel http://s/fs/abc/k a\nboot" {
		t.Errorf("a kernel that holds its own start-up disk: %s", got)
	}
}

func TestAnExtraArchiveIsLoadedAfterTheIsosOwnAndBeforeBoot(t *testing.T) {
	u := withNBD(urls)
	b := newBoot("X", files{u.FS, "/k", "/i"}, "a").withExtra(u, nil)
	if got := lines(b); !strings.HasSuffix(got, "initrd http://s/fs/abc/i\ninitrd http://s/x/abc/extra.cpio\nboot") || b.ExtraInitrd != u.Extra {
		t.Errorf("%s", got)
	}
	if b.HasOwnExtra() {
		t.Error("the usual archive is not an archive of its own")
	}
	own := b.withExtra(u, func(ExtraEnv) ([]byte, error) { return []byte("mine"), nil })
	if data, _ := own.BuildExtra(ExtraEnv{}); string(data) != "mine" || !own.HasOwnExtra() {
		t.Error("an archive of its own is not used")
	}
	if data, _ := b.BuildExtra(ExtraEnv{}); !strings.Contains(string(data), "usr/bin/nfsmount") {
		t.Error("the usual archive has the mount command")
	}
}

func TestAFileIsLookedForInTheIsoFirstAndThenInTheOtherVolumes(t *testing.T) {
	esp := fake{"/loader/x.conf": true}
	c := &Context{FS: fake{"/a": true}, URLs: urls, Volumes: func() []Volume { return []Volume{{esp, "http://s/fs/abc@2"}} }}
	if v, ok := c.volumeWith("/a"); !ok || v.URL != urls.FS {
		t.Errorf("the ISO's own file: %+v %v", v, ok)
	}
	if v, ok := c.volumeWith("/loader"); !ok || v.URL != "http://s/fs/abc@2" {
		t.Errorf("a file in the EFI image: %+v %v", v, ok)
	}
	if _, ok := c.volumeWith("/nothing"); ok {
		t.Error("a missing file was found")
	}
	c.Volumes = nil
	if _, ok := c.volumeWith("/loader"); ok {
		t.Error("a context with no other volumes found a file in one")
	}
}

func TestFirstMatchIsTheSameEveryTime(t *testing.T) {
	tree := fake{"/d/vmlinuz-b": true, "/d/vmlinuz-a": true, "/d/vmlinuz-c/x": true, "/d/other": true}
	for i := 0; i < 20; i++ {
		if got := firstMatch(tree, "/d", "vmlinuz"); got != "/d/vmlinuz-a" {
			t.Fatalf("got %q", got)
		}
	}
	if firstMatch(tree, "/none", "x") != "" || firstMatch(tree, "/d", "zzz") != "" {
		t.Error("a missing folder or prefix found something")
	}
}
