package httpd

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// The kernel stops reading its parameters at a "--" (Linux Mint's boot menu entries have one): words that follow are handed to init. Arguments a person adds
// (the ISO's, an entry's, a PC's) and the debug words must therefore go in front of it, in that order.
func TestExtraArgumentsGoInFrontOfTheKernelsDoubleDash(t *testing.T) {
	line := "kernel http://s/vmlinuz boot=casper username=mint quiet splash -- tail=1"
	commands := []string{"echo x", line, "initrd http://s/initrd", "boot"}
	got := addArgs(addArgs(addArgs(commands, "isoarg=1"), "entryarg=2"), "pcarg=3")
	if want := "kernel http://s/vmlinuz boot=casper username=mint quiet splash isoarg=1 entryarg=2 pcarg=3 -- tail=1"; got[1] != want {
		t.Fatalf("\n got  %q\n want %q", got[1], want)
	}
	if commands[1] != line || got[0] != commands[0] || got[2] != commands[2] {
		t.Fatalf("changed what it should not: %v / %v", commands, got)
	}
	// no separator: the words end the line, as before; "---" (installers) is no separator for the kernel
	for _, plain := range []string{"kernel http://s/vmlinuz boot=live", "kernel http://s/vmlinuz boot=casper quiet ---"} {
		if got := addArgs([]string{plain, "boot"}, "extra=1"); got[0] != plain+" extra=1" {
			t.Fatalf("%q -> %q", plain, got[0])
		}
	}
	// a "--" inside a word is not the separator
	if got := addArgs([]string{"kernel http://s/vmlinuz a--b -- z"}, "x=1"); got[0] != "kernel http://s/vmlinuz a--b x=1 -- z" {
		t.Fatalf("%q", got[0])
	}
}

func TestDebugWordsGoInFrontOfTheKernelsDoubleDash(t *testing.T) {
	commands := []string{"kernel http://s/vmlinuz boot=casper quiet splash console=tty0 -- tail=1", "boot"}
	got := debugArgs(commands, config.DebugMode{Verbose: true, Serial: true}, "")
	want := "kernel http://s/vmlinuz boot=casper loglevel=7 ignore_loglevel debug console=tty0 console=ttyS0,115200n8 -- tail=1"
	if got[0] != want {
		t.Fatalf("\n got  %q\n want %q", got[0], want)
	}
	if again := debugArgs(got, config.DebugMode{Verbose: true, Serial: true}, ""); again[0] != want {
		t.Fatalf("not idempotent: %q", again[0])
	}
	if !strings.HasSuffix(commands[0], "-- tail=1") || strings.Contains(commands[0], "loglevel") {
		t.Fatal("changed the input")
	}
}
