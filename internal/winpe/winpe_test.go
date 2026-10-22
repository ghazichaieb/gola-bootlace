package winpe

import (
	"strings"
	"testing"
)

var set = Settings{Base: "http://10.0.0.1:8080", FS: "http://10.0.0.1:8080/fs/ab", Win: "http://10.0.0.1:8080/w/ab", Host: "10.0.0.1", Share: "iso", Folder: "ab", User: "bootlace", Password: "s3cret", Port: 4445}

func TestEveryFileIsNamedForUefiAndForBios(t *testing.T) {
	// wimboot reads a file's name from the image name (UEFI) or from the image's command line (BIOS): the last word of the line
	for _, want := range []string{"/bootx64.efi bootmgfw.efi\n", "/boot/bcd BCD\n", "/bootmgr bootmgr\n", "/boot/boot.sdi boot.sdi\n", "/boot.wim boot.wim\n", "/winpeshl.ini winpeshl.ini\n", "/bootlace.cmd bootlace.cmd\n"} {
		if s := IPXE(set); !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestTheScriptLoadsWimbootWithTheBootFilesAndOurTwoFiles(t *testing.T) {
	s := IPXE(set)
	for _, want := range []string{"kernel http://10.0.0.1:8080/ipxe/wimboot", "initrd --name boot.wim http://10.0.0.1:8080/fs/ab/sources/boot.wim", "initrd --name BCD http://10.0.0.1:8080/fs/ab/efi/microsoft/boot/bcd",
		"initrd --name winpeshl.ini http://10.0.0.1:8080/w/ab/winpeshl.ini", "initrd --name bootlace.cmd http://10.0.0.1:8080/w/ab/bootlace.cmd", "\nboot\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}

func TestTheCommandFileConnectsToTheShareOnTheRightPortAndStartsTheSetupOfTheEnvironment(t *testing.T) {
	c := Cmd(set)
	for _, want := range []string{`net use Z: \\10.0.0.1\iso /user:bootlace s3cret /TCPPORT:4445`, `set IMAGE=Z:\ab\sources\install.wim`, `install.esd`,
		`/unattend:Z:\ab\autounattend.xml`, `\sources\setup.exe %OPTIONS%`, `cd /d Z:\ab`, "wpeinit"} {
		if !strings.Contains(c, want) {
			t.Fatalf("missing %q in\n%s", want, c)
		}
	}
	set.Port = 445
	if strings.Contains(Cmd(set), "TCPPORT") {
		t.Fatal("the default port needs no option")
	}
	if !strings.Contains(ShellIni(), `bootlace.cmd`) {
		t.Fatal(ShellIni())
	}
}

func TestAHeldCommandFileStopsBeforeSetup(t *testing.T) {
	h := set
	h.Hold = true
	c := Cmd(h)
	if !strings.Contains(c, "Held") || strings.Index(c, "Held") > strings.Index(c, "setup.exe") {
		t.Fatal(c)
	}
}

func TestTheCommandFileRemovesThePxeRecordSoThatSetupDoesNotStartInWdsMode(t *testing.T) {
	c := Cmd(set)
	i, j := strings.Index(c, `reg delete HKLM\SYSTEM\CurrentControlSet\Control\PXE /f`), strings.Index(c, `sources\setup.exe`)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the PXE record must be removed before Setup starts:\n%s", c)
	}
}
