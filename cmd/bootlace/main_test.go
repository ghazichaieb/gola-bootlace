package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
)

func TestVersionNamesTheProjectAndCarriesTheAttributionLine(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut.String())
	}
	for _, want := range []string{"Gola Bootlace", "by Gola Services", "GPL-3.0-or-later"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("version output lacks %q: %q", want, out.String())
		}
	}
}

func TestNoArgumentsShowsHowToStartTheServer(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code == 0 {
		t.Fatal("expected a non-zero exit code")
	}
	if !strings.Contains(errOut.String(), "bootlace serve") {
		t.Errorf("unexpected message %q", errOut.String())
	}
}

// An unquoted folder name with a space left a server running on the real network with the wrong settings: a stray word must stop the start.
func TestAStrayWordAfterTheOptionsStopsTheStart(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"serve", "-iso-dir", "/data/Some", "Folder/isos", "-ip", "10.0.0.1"}, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "unexpected argument") || !strings.Contains(errOut.String(), "nothing was started") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestTheSecureBootRouteIsReportedToTheWebInterfaceAsOffMarkedOrAll(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		all  bool
		want string
	}{{"", false, ""}, {"", true, ""}, {"/sb", false, "pcs"}, {"/sb", true, "all"}} {
		if got := secureBootMode(tc.dir, tc.all); got != tc.want {
			t.Errorf("%q %v: %q, want %q", tc.dir, tc.all, got, tc.want)
		}
	}
}

func TestTheLogSaysAtTheStartWhetherTheSecureBootRouteIsOnOrOffAndWhatIsMissing(t *testing.T) {
	off := strings.Join(secureBootStartLines(httpd.SecureBoot{}), "\n")
	for _, want := range []string{"Secure Boot route: OFF", "BOOTLACE_SECURE_BOOT_DIR", "docs/secure-boot.md"} {
		if !strings.Contains(off, want) {
			t.Errorf("off: %q lacks %q", off, want)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ipxe-shimx64.efi"), []byte("SHIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	sb := httpd.SecureBoot{Dir: dir, Shim: "ipxe-shimx64.efi", All: true}
	lines := secureBootStartLines(sb)
	if len(lines) != 2 || !strings.Contains(lines[0], "Secure Boot route: ON for every 64-bit UEFI PC") || !strings.Contains(lines[0], "ipxe-shimx64.efi") || !strings.Contains(lines[0], "asks for ipxe.efi") ||
		!strings.HasPrefix(lines[1], "warning: ") || !strings.Contains(lines[1], "has no ipxe.efi") {
		t.Fatalf("a folder without the second stage: %q", lines)
	}
	if err := os.WriteFile(filepath.Join(dir, "ipxe.efi"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lines = secureBootStartLines(httpd.SecureBoot{Dir: dir, Shim: "ipxe-shimx64.efi"}); len(lines) != 1 || !strings.Contains(lines[0], "the PCs marked for it") {
		t.Fatalf("complete: %q", lines)
	}
}
