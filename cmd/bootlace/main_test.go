package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
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

// A new installation has the built-in Secure Boot route on for every 64-bit UEFI PC; a settings file from before the setting existed is left off, so that an upgrade changes
// no behaviour (and the web interface asks).
func TestANewInstallationStartsWithTheBuiltInRouteAndAnOlderOneStaysOff(t *testing.T) {
	dir := t.TempDir()
	seed := config.Config{MenuTimeout: 20, PCDays: config.DefaultPCDays, Hidden: map[string]bool{}, Titles: map[string]string{}, Default: "local"}
	freshSecureBoot(&seed)
	path := filepath.Join(dir, "bootlace.json")
	store, created, err := config.Open(path, seed)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	if mode, scope, chosen := store.SecureBoot(); mode != "builtin" || scope != "all" || chosen {
		t.Fatalf("a new installation: %q %q chosen=%v", mode, scope, chosen)
	}
	if again, created, err := config.Open(path, config.Config{}); err != nil || created {
		t.Fatal(created, err)
	} else if mode, scope, _ := again.SecureBoot(); mode != "builtin" || scope != "all" {
		t.Fatalf("kept in the file: %q %q", mode, scope)
	}

	older := filepath.Join(dir, "older.json")
	if err := os.WriteFile(older, []byte(`{"menuTimeout":20,"pcDays":30,"default":"local","hidden":{},"titles":{},"adminHash":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, created, err = config.Open(older, seed) // (the seed is only for a file that does not exist)
	if err != nil || created {
		t.Fatal(created, err)
	}
	if mode, scope, chosen := store.SecureBoot(); mode != "" || scope != "" || chosen {
		t.Fatalf("an older file has no Secure Boot setting: %q %q chosen=%v", mode, scope, chosen)
	}
}

func TestTheSecureBootOptionsAreInTheEnvironmentToo(t *testing.T) {
	for option, want := range map[string]string{"secure-boot": "BOOTLACE_SECURE_BOOT", "secure-boot-scope": "BOOTLACE_SECURE_BOOT_SCOPE", "secure-boot-dir": "BOOTLACE_SECURE_BOOT_DIR",
		"secure-boot-shim": "BOOTLACE_SECURE_BOOT_SHIM", "secure-boot-all": "BOOTLACE_SECURE_BOOT_ALL"} {
		if got := envName(option); got != want {
			t.Errorf("%s: %s, want %s", option, got, want)
		}
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	mode := fs.String("secure-boot", "", "")
	scope := fs.String("secure-boot-scope", "", "")
	all := fs.Bool("secure-boot-all", false, "")
	if err := applyEnv(fs, fakeEnv(map[string]string{"BOOTLACE_SECURE_BOOT": "builtin", "BOOTLACE_SECURE_BOOT_SCOPE": "all", "BOOTLACE_SECURE_BOOT_ALL": "true"})); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse([]string{"-secure-boot", "own"}); err != nil {
		t.Fatal(err)
	}
	if *mode != "own" || *scope != "all" || !*all {
		t.Fatalf("the command line wins over the environment: %q %q %v", *mode, *scope, *all)
	}
	// a value that is not a choice stops the start with a message that names the option
	if err := (httpd.SecureBoot{Mode: "maybe"}).Check(); err == nil || !strings.Contains(err.Error(), "-secure-boot") {
		t.Fatalf("%v", err)
	}
}
