package winpe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDriverFoldersAreFoundForEveryIsoAndForOneIsoAndNothingElse(t *testing.T) {
	lib := t.TempDir()
	put(t, filepath.Join(lib, "drivers", "all", "vioscsi", "vioscsi.inf"), "x")
	put(t, filepath.Join(lib, "drivers", "Win11 Pro", "nic", "nic.inf"), "y")
	put(t, filepath.Join(lib, "drivers", "other", "z", "z.inf"), "z")
	sets := SetsFor(lib, "sub/Win11 Pro.iso")
	if len(sets) != 2 || sets[0].WIM != "/$WinPEDriver$/all" || sets[1].WIM != "/$WinPEDriver$/iso" || filepath.Base(sets[1].Dir) != "Win11 Pro" {
		t.Fatalf("%+v", sets)
	}
	if got := SetsFor(lib, "nothing.iso"); len(got) != 1 || got[0].WIM != "/$WinPEDriver$/all" {
		t.Fatalf("%+v", got)
	}
	if got := SetsFor(lib, "all.iso"); len(got) != 1 {
		t.Fatalf("an ISO called all must not take the folder twice: %+v", got)
	}
	use, print, bytes, err := Plan(sets)
	if err != nil || len(use) != 2 || print == "" || bytes != 2 {
		t.Fatalf("%v %q %d %v", use, print, bytes, err)
	}
	// a change makes another fingerprint; a folder without a .inf is not a driver
	put(t, filepath.Join(lib, "drivers", "all", "vioscsi", "vioscsi.sys"), "more")
	_, print2, _, _ := Plan(sets)
	if print2 == print {
		t.Fatal("the fingerprint did not change")
	}
	put(t, filepath.Join(lib, "drivers", "empty", "readme.txt"), "no driver")
	if use, _, _, _ := Plan(SetsFor(lib, "empty.iso")); len(use) != 1 { // (only "all")
		t.Fatalf("%v", use)
	}
	if use, p, _, err := Plan(SetsFor(t.TempDir(), "a.iso")); err != nil || len(use) != 0 || p != "" {
		t.Fatal("no folder means nothing to add")
	}
}

func TestTheCommandFileLoadsTheDriversFirstAndTheIpxeScriptTakesTheCopyThatHoldsThem(t *testing.T) {
	s := set
	s.Drivers, s.PatchedWIM = true, true
	c := Cmd(s)
	i, j := strings.Index(c, "drvload"), strings.Index(c, "wpeinit")
	if i < 0 || j < 0 || i > j || !strings.Contains(c, `$WinPEDriver$`) || strings.Contains(c, "installdrivers") {
		t.Fatalf("drivers go in before the network starts, and Setup has no /installdrivers:\n%s", c)
	}
	if strings.Contains(Cmd(set), "drvload") {
		t.Fatal("no drivers, no drvload")
	}
	if got := IPXE(s); !strings.Contains(got, "initrd --name boot.wim http://10.0.0.1:8080/w/ab/boot.wim boot.wim") {
		t.Fatalf("%s", got)
	}
	if got := IPXE(set); !strings.Contains(got, "/fs/ab/sources/boot.wim") {
		t.Fatalf("%s", got)
	}
}
