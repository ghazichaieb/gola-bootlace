package main

import (
	"bytes"
	"strings"
	"testing"
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
