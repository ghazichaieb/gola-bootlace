package winpe

import (
	"strings"
	"testing"
)

var set = Settings{Base: "http://10.0.0.1:8080", FS: "http://10.0.0.1:8080/fs/ab", Win: "http://10.0.0.1:8080/w/ab", Host: "10.0.0.1", Share: "wab", User: "bootlace", Password: "s3cret", Port: 4445}

func TestTheScriptLoadsWimbootWithTheBootFilesAndOurTwoFiles(t *testing.T) {
	s := IPXE(set)
	for _, want := range []string{"kernel http://10.0.0.1:8080/ipxe/wimboot", "initrd --name boot.wim http://10.0.0.1:8080/fs/ab/sources/boot.wim", "initrd --name BCD http://10.0.0.1:8080/fs/ab/efi/microsoft/boot/bcd",
		"initrd --name winpeshl.ini http://10.0.0.1:8080/w/ab/winpeshl.ini", "initrd --name bootlace.cmd http://10.0.0.1:8080/w/ab/bootlace.cmd", "\nboot\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}

func TestTheCommandFileConnectsToTheShareOnTheRightPortAndStartsSetup(t *testing.T) {
	c := Cmd(set)
	if !strings.Contains(c, `net use Z: \\10.0.0.1\wab /user:bootlace s3cret /TCPPORT:4445`) || !strings.Contains(c, `Z:\setup.exe`) || !strings.Contains(c, "wpeinit") {
		t.Fatal(c)
	}
	set.Port = 445
	if strings.Contains(Cmd(set), "TCPPORT") {
		t.Fatal("the default port needs no option")
	}
	if !strings.Contains(ShellIni(), `bootlace.cmd`) {
		t.Fatal(ShellIni())
	}
}
