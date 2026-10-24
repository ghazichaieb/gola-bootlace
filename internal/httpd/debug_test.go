package httpd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
)

func TestDebugModeLoudensTheKernelLineAndMovesTheConsoleToTheSerialPort(t *testing.T) {
	commands := []string{"echo Starting", "kernel http://s/vmlinuz boot=casper quiet splash console=tty0 ---", "initrd http://s/initrd", "boot"}
	if got := debugArgs(commands, config.DebugMode{}); !reflect.DeepEqual(got, commands) {
		t.Fatalf("no mode, no change: %v", got)
	}
	verbose := debugArgs(commands, config.DebugMode{Verbose: true})
	if want := "kernel http://s/vmlinuz boot=casper console=tty0 --- loglevel=7 ignore_loglevel debug"; verbose[1] != want {
		t.Fatalf("verbose:\n got  %q\n want %q", verbose[1], want)
	}
	serial := debugArgs(commands, config.DebugMode{Serial: true})
	if want := "kernel http://s/vmlinuz boot=casper quiet splash --- console=tty0 console=ttyS0,115200n8"; serial[1] != want {
		t.Fatalf("serial:\n got  %q\n want %q", serial[1], want)
	}
	both := debugArgs(commands, config.DebugMode{Verbose: true, Serial: true})
	if want := "kernel http://s/vmlinuz boot=casper --- loglevel=7 ignore_loglevel debug console=tty0 console=ttyS0,115200n8"; both[1] != want {
		t.Fatalf("both:\n got  %q\n want %q", both[1], want)
	}
	// the other lines and the caller's slice stay as they were; running it twice does not repeat the words
	if both[0] != commands[0] || both[2] != commands[2] || commands[1] == both[1] {
		t.Fatalf("%v / %v", commands, both)
	}
	again := debugArgs([]string{both[1]}, config.DebugMode{Verbose: true})
	if again[0] != "kernel http://s/vmlinuz boot=casper --- loglevel=7 ignore_loglevel debug console=tty0 console=ttyS0,115200n8" {
		t.Fatalf("a second pass must not repeat words: %q", again[0])
	}
	// an ISO without a kernel line (a disk, a Windows start) is not touched
	disk := []string{"echo disk", "sanboot --drive 0x80 iscsi:x"}
	if got := debugArgs(disk, config.DebugMode{Verbose: true, Serial: true}); !reflect.DeepEqual(got, disk) {
		t.Fatalf("%v", got)
	}
}

func TestVerboseModeAddsFamilyDiagnosticsAndRemovesSilentSplash(t *testing.T) {
	for _, arg := range []string{"rd.debug", "proxdebug"} {
		commands := []string{"kernel http://s/kernel quiet splash=silent loglevel=3 console=tty0", "initrd http://s/initrd", "boot"}
		for _, mode := range []config.DebugMode{{}, {Serial: true}} {
			got := debugArgs(commands, mode, arg)
			if strings.Contains(got[0], arg) || !strings.Contains(got[0], "splash=silent") {
				t.Fatalf("family diagnostics require verbose mode: %v", got)
			}
		}
		mode := config.DebugMode{Verbose: true, Serial: true}
		got := debugArgs(commands, mode, arg, arg)
		words := strings.Fields(got[0])
		counts := map[string]int{}
		for _, w := range words {
			counts[w]++
		}
		if counts[arg] != 1 || counts["loglevel=7"] != 1 || counts["quiet"] != 0 || counts["splash=silent"] != 0 || counts["loglevel=3"] != 0 {
			t.Fatalf("unexpected debug arguments: %v", words)
		}
		if again := debugArgs(got, mode, arg); !reflect.DeepEqual(got, again) {
			t.Fatalf("not idempotent: %v / %v", got, again)
		}
		if got[1] != commands[1] || got[2] != commands[2] || !strings.Contains(commands[0], "splash=silent") {
			t.Fatal("changed other commands or original input")
		}
	}
}

func TestEntryInfosUsesFamilyVerboseArgumentsOnlyWhenEnabled(t *testing.T) {
	_, s, store, _ := managed(t)
	list, err := s.scan()
	if err != nil {
		t.Fatal(err)
	}
	s.known = map[string]recognised{}
	for _, iso := range list {
		s.known["named|"+stamp(iso)] = recognised{ok: true, boot: family.Boot{Family: "Proxmox VE", Commands: []string{"kernel http://s/linux26 quiet splash=silent", "boot"}, VerboseArgs: []string{"proxdebug"}}}
	}
	cfg := store.Get()
	cfg.Debug = map[string]config.DebugMode{list[0].ID: {Verbose: true}}
	cfg.Args = map[string]string{list[0].ID: "nomodeset"}
	infos, err := s.entryInfos(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		cmd := info.Entry.Commands[0]
		if info.ISO.ID == list[0].ID {
			if !strings.Contains(cmd, "proxdebug") || !strings.Contains(cmd, "nomodeset") || strings.Contains(cmd, "splash=silent") {
				t.Fatalf("verbose ISO: %s", cmd)
			}
		} else if strings.Contains(cmd, "proxdebug") {
			t.Fatalf("debug leaked to another ISO: %s", cmd)
		}
	}
	cfg.Debug = nil
	infos, err = s.entryInfos(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if strings.Contains(info.Entry.Commands[0], "proxdebug") || !strings.Contains(info.Entry.Commands[0], "splash=silent") {
			t.Fatal("disabling debug did not restore normal boot")
		}
	}
}
