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

// A PC whose network card Windows PE has no driver for cannot reach the share: the failure branch must show which devices lack a driver (the IDs to look the driver up by) and
// say where a driver goes, instead of only printing an adapter list that has no adapter in it.
func TestWhenTheShareCannotBeReachedTheCommandFileNamesTheDevicesThatLackADriverAndWhereTheDriverGoes(t *testing.T) {
	c := Cmd(set)
	from, to := strings.Index(c, ":failed"), strings.Index(c, ":up")
	if from < 0 || to < from {
		t.Fatalf("no failure branch in\n%s", c)
	}
	failed := c[from:to]
	for _, want := range []string{"ipconfig /all", `pnputil /enum-devices /problem | findstr /i "Instance Description"`, `drivers\all\`, "VEN_"} {
		if !strings.Contains(failed, want) {
			t.Errorf("the failure branch lacks %q:\n%s", want, failed)
		}
	}
	// the command interpreter takes < and > in an echo for redirections (and & for a second command): they have to be escaped
	for _, l := range strings.Split(failed, "\n") {
		if text, ok := strings.CutPrefix(strings.TrimSpace(l), "echo "); ok {
			text = strings.NewReplacer("^<", "", "^>", "", "^&", "").Replace(text)
			if strings.ContainsAny(text, "<>&") {
				t.Errorf("an unescaped redirection or & in %q", l)
			}
		}
	}
}

func TestTheCommandFileConnectsToTheShareOnTheRightPortAndStartsTheSetupOfTheEnvironment(t *testing.T) {
	c := Cmd(set)
	for _, want := range []string{`net use Z: \\10.0.0.1\iso /user:bootlace s3cret /TCPPORT:4445`, `set IMAGE=Z:\ab\sources\install.wim`, `install.esd`,
		`set UNATTEND=Z:\ab\autounattend.xml`, `/unattend:%UNATTEND%`, `copy Z:\ab\_bootlace\%cand%\autounattend.xml`, `ipconfig /all`, `\sources\setup.exe %OPTIONS%`, `cd /d Z:\ab`, "wpeinit"} {
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

// The answer file is fetched before Setup starts, from the share, for the MAC addresses ipconfig lists, and it wins over the one in the ISO's folder.
func TestTheCommandFileCopiesTheAnswerOfThisPCBeforeSetupAndItBeatsTheFileInTheIsoFolder(t *testing.T) {
	c := Cmd(set)
	iFor, iCopy, iIso, iOpt, iSetup := strings.Index(c, "ipconfig /all"), strings.Index(c, `copy Z:\ab\_bootlace\%cand%`), strings.Index(c, `if exist Z:\ab\autounattend.xml`), strings.Index(c, "/unattend:%UNATTEND%"), strings.Index(c, `sources\setup.exe`)
	if iFor < 0 || iCopy < 0 || iIso < 0 || iOpt < 0 || iSetup < 0 {
		t.Fatalf("pieces missing:\n%s", c)
	}
	if !(iFor < iIso && iIso < iOpt && iOpt < iSetup) {
		t.Fatalf("the answer must be looked for, then the ISO's own file, then Setup started:\n%s", c)
	}
	if iCopy < iSetup { // (the subroutine sits after the main flow)
		t.Fatalf("the subroutine must follow the main flow, or the script falls into it:\n%s", c)
	}
	if i := strings.Index(c, ":end"); i < 0 || strings.Index(c, "goto :eof") < i || strings.Index(c, "\r\n:answer\r\n") < strings.Index(c, "goto :eof") {
		t.Fatalf("the main flow must end before the subroutine:\n%s", c)
	}
	// only a word shaped like aa-bb-cc-dd-ee-ff is tried
	for _, at := range []string{"2", "5", "8", "11", "14"} {
		if !strings.Contains(c, `if not "%cand:~`+at+`,1%"=="-" exit /b 0`) {
			t.Errorf("no check of position %s", at)
		}
	}
	if !strings.Contains(c, `if not "%cand:~17%"=="" exit /b 0`) || !strings.Contains(c, `if "%~1"=="" exit /b 0`) {
		t.Errorf("no length check:\n%s", c)
	}
	if strings.Contains(c, "\x07") || strings.Contains(c, "\x08") {
		t.Error("a control character in the command file")
	}
	// every line ends with CR LF
	if strings.Contains(strings.ReplaceAll(c, "\r\n", ""), "\n") {
		t.Error("a line without a carriage return")
	}
}
