package initrd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

func TestTheArchiveIsInTheFormatTheKernelReads(t *testing.T) {
	a := Archive([]File{{Name: "usr", Mode: 0o755, Dir: true}, {Name: "usr/bin/x", Mode: 0o755, Data: []byte("hello")}})
	if !bytes.HasPrefix(a, []byte("070701")) || !bytes.Contains(a, []byte("TRAILER!!!")) || !bytes.Contains(a, []byte("usr/bin/x\x00")) || !bytes.Contains(a, []byte("hello")) {
		t.Fatalf("%q", a)
	}
	if len(a)%4 != 0 {
		t.Fatalf("length %d is not a multiple of 4", len(a))
	}
	// every header is 110 bytes, and names and data are padded to 4
	if string(a[110:114]) != "usr\x00" {
		t.Fatalf("name at 110: %q", a[110:118])
	}
}

func TestTheScriptNamesTheIsoAndNeverCopiesItWhole(t *testing.T) {
	a := string(Extra(Settings{Host: "10.0.0.1", Port: 10809, Name: "abc1234567"}, []byte("ELF")))
	for _, want := range []string{"-host '10.0.0.1' -port 10809 -name 'abc1234567'", "usr/bin/bootlace-nbd", "mount -t iso9660 -o ro /dev/nbd0", "sbin/nbdroot", "usr/bin/nbd-client"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(a, "wget") {
		t.Error("the ISO must never be downloaded whole into memory")
	}
}

func TestADisksBlockSizeIsPassedToTheHelper(t *testing.T) {
	if a := string(Extra(Settings{Host: "h", Port: 1, Name: "x@1", Block: 512}, nil)); !strings.Contains(a, "-block 512") {
		t.Error("a partition of a disk image is attached with 512-byte blocks")
	}
	if a := string(Extra(Settings{Host: "h", Port: 1, Name: "x"}, nil)); !strings.Contains(a, "-block 2048") {
		t.Error("an ISO is attached with 2048-byte blocks")
	}
}

func TestDracutsNetworkRootHandlerKeepsItsShellExpansions(t *testing.T) {
	// "%%" is the shell's longest-match removal; it must reach the PC as it is written
	if !strings.Contains(nbdroot, `${rest%%:*}`) {
		t.Error("the handler lost a %")
	}
}

func TestABuilderMakesAFolderOnceAndNumbersTheModulesInLoadingOrder(t *testing.T) {
	var b Builder
	b.Dir("bootlace")
	b.Dir("bootlace")
	b.Modules("bootlace/modules", []initread.Module{{Path: "kernel/net/ptp.ko.zst", Data: []byte("a")}, {Path: "kernel/drivers/e1000e.ko.xz", Data: []byte("b")}})
	a := string(b.Bytes())
	if strings.Count(a, "bootlace\x00") != 1 {
		t.Error("the folder was made twice")
	}
	first, second := strings.Index(a, "bootlace/modules/00-ptp.ko"), strings.Index(a, "bootlace/modules/01-e1000e.ko")
	if first < 0 || second < 0 || first > second {
		t.Errorf("modules are not numbered in the order they were given: %q", a)
	}
}

func TestTheNetworkScriptGivesTheAddressesAndTheCommandsItWasToldToUse(t *testing.T) {
	s := NetworkScript{Helper: "/usr/bin/bootlace-nbd", Dir: "/bootlace", Host: "10.0.0.1", Port: 10809, Name: "a b", Say: "msg", Fail: "err", Block: 512}.String()
	for _, want := range []string{`msg ":: Gola Bootlace: starting the network"`, "/usr/bin/bootlace-nbd insmod /bootlace/modules/*", "-host '10.0.0.1' -port 10809 -name 'a b' -dev /dev/nbd0 -block 512", "-s /bootlace/udhcpc.sh", `err "Gola Bootlace: no network card`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}

func TestTheRdinitWrapperEndsByStartingTheInitramfsOwnInit(t *testing.T) {
	if s := RdinitWrapper("echo hi\n"); !strings.HasPrefix(s, "#!/bin/sh") || !strings.HasSuffix(s, "echo hi\nexec /init\n") {
		t.Errorf("%q", s)
	}
}
