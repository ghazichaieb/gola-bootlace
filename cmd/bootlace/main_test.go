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
