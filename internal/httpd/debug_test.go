package httpd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

func TestVerboseRemovesValuedSplashAndOverridesLogLevels(t *testing.T) {
	for _, fam := range []string{"Proxmox VE", "Fedora (live)"} {
		commands := []string{"kernel http://s/kernel quiet splash=silent loglevel=3 console=tty0", "initrd http://s/initrd", "boot"}
		for _, mode := range []config.DebugMode{{}, {Serial: true}} {
			got := debugArgs(commands, mode, fam)
			if !strings.Contains(got[0], "splash=silent") || !strings.Contains(got[0], "loglevel=3") {
				t.Fatalf("non-verbose mode changed logging: %v", got)
			}
		}
		mode := config.DebugMode{Verbose: true, Serial: true}
		got := debugArgs(commands, mode, fam)
		if strings.Contains(got[0], "splash=") || strings.Contains(got[0], "loglevel=3") || strings.Count(got[0], "loglevel=7") != 1 {
			t.Fatalf("unexpected verbose command: %s", got[0])
		}
		if again := debugArgs(got, mode, fam); !reflect.DeepEqual(got, again) {
			t.Fatalf("not idempotent: %v / %v", got, again)
		}
		if got[1] != commands[1] || got[2] != commands[2] || !strings.Contains(commands[0], "splash=silent") {
			t.Fatal("changed other commands or original input")
		}
		entry := menu.Entry{Kind: fam, Commands: commands}
		pc := forPC(entry, config.PCOptions{Debug: mode})
		if !reflect.DeepEqual(pc.Commands, got) {
			t.Fatalf("PC options differ: %v", pc.Commands)
		}
	}
}

func TestDebugModeLoudensTheKernelLineAndMovesTheConsoleToTheSerialPort(t *testing.T) {
	commands := []string{"echo Starting", "kernel http://s/vmlinuz boot=casper quiet splash console=tty0 ---", "initrd http://s/initrd", "boot"}
	if got := debugArgs(commands, config.DebugMode{}, ""); !reflect.DeepEqual(got, commands) {
		t.Fatalf("no mode, no change: %v", got)
	}
	verbose := debugArgs(commands, config.DebugMode{Verbose: true}, "")
	if want := "kernel http://s/vmlinuz boot=casper console=tty0 --- loglevel=7 ignore_loglevel debug"; verbose[1] != want {
		t.Fatalf("verbose:\n got  %q\n want %q", verbose[1], want)
	}
	serial := debugArgs(commands, config.DebugMode{Serial: true}, "")
	if want := "kernel http://s/vmlinuz boot=casper quiet splash --- console=tty0 console=ttyS0,115200n8"; serial[1] != want {
		t.Fatalf("serial:\n got  %q\n want %q", serial[1], want)
	}
	both := debugArgs(commands, config.DebugMode{Verbose: true, Serial: true}, "")
	if want := "kernel http://s/vmlinuz boot=casper --- loglevel=7 ignore_loglevel debug console=tty0 console=ttyS0,115200n8"; both[1] != want {
		t.Fatalf("both:\n got  %q\n want %q", both[1], want)
	}
	// the other lines and the caller's slice stay as they were; running it twice does not repeat the words
	if both[0] != commands[0] || both[2] != commands[2] || commands[1] == both[1] {
		t.Fatalf("%v / %v", commands, both)
	}
	again := debugArgs([]string{both[1]}, config.DebugMode{Verbose: true}, "")
	if again[0] != "kernel http://s/vmlinuz boot=casper --- loglevel=7 ignore_loglevel debug console=tty0 console=ttyS0,115200n8" {
		t.Fatalf("a second pass must not repeat words: %q", again[0])
	}
	// an ISO without a kernel line (a disk, a Windows start) is not touched
	disk := []string{"echo disk", "sanboot --drive 0x80 iscsi:x"}
	if got := debugArgs(disk, config.DebugMode{Verbose: true, Serial: true}, ""); !reflect.DeepEqual(got, disk) {
		t.Fatalf("%v", got)
	}
}

func TestVerboseAddsTheWordsOfTheKindOfSystemOnce(t *testing.T) {
	commands := []string{"kernel http://s/linux ip=dhcp quiet rd.live.image", "initrd http://s/initrd", "boot"}
	fedora := debugArgs(commands, config.DebugMode{Verbose: true}, "Fedora (live)")
	if want := "kernel http://s/linux ip=dhcp rd.live.image loglevel=7 ignore_loglevel debug rd.debug systemd.show_status=1"; fedora[0] != want {
		t.Fatalf("Fedora:\n got  %q\n want %q", fedora[0], want)
	}
	if again := debugArgs(fedora, config.DebugMode{Verbose: true}, "Fedora (live)"); again[0] != fedora[0] {
		t.Fatalf("a second pass must not repeat words: %q", again[0])
	}
	if alpine := debugArgs(commands, config.DebugMode{Verbose: true}, "Alpine Linux"); !strings.HasSuffix(alpine[0], " debug debug_init") {
		t.Fatalf("Alpine: %q", alpine[0])
	}
	if tails := debugArgs(commands, config.DebugMode{Verbose: true}, "Tails"); strings.Contains(tails[0], "systemd") || strings.Contains(tails[0], "rd.debug") {
		t.Fatalf("a kind without words of its own gets only the general ones: %q", tails[0])
	}
}

func TestAPCsOwnOptionsComeAfterThoseOfTheISO(t *testing.T) {
	iso := menu.Entry{Name: "Fedora", Kind: "Fedora (live)", Commands: []string{"echo Starting", "kernel http://s/linux quiet rd.live.image isoarg", "initrd http://s/initrd", "boot"}}
	if got := forPC(iso, config.PCOptions{}); !reflect.DeepEqual(got, iso) {
		t.Fatalf("a PC without options gets the entry as it is: %v", got.Commands)
	}
	got := forPC(iso, config.PCOptions{Args: "nomodeset", Debug: config.DebugMode{Verbose: true, Serial: true}})
	want := "kernel http://s/linux rd.live.image isoarg nomodeset loglevel=7 ignore_loglevel debug rd.debug systemd.show_status=1 console=tty0 console=ttyS0,115200n8"
	if got.Commands[1] != want || got.Commands[0] != iso.Commands[0] || got.Commands[2] != iso.Commands[2] {
		t.Fatalf("\n got  %q\n want %q", got.Commands[1], want)
	}
	if iso.Commands[1] != "kernel http://s/linux quiet rd.live.image isoarg" {
		t.Fatal("the entry of the ISO list must not change: the next PC would get this PC's options")
	}
	disk := menu.Entry{Name: "Some disk", Kind: "as a disk", Commands: []string{"sanboot --drive 0x80 x"}}
	if got := forPC(disk, config.PCOptions{Args: "nomodeset"}); !reflect.DeepEqual(got, disk) {
		t.Fatalf("an ISO that is handed over as a disk has no kernel line: %v", got.Commands)
	}
}

func TestTheDebugShellIsItsOwnSwitchAndVerboseNeverOpensOne(t *testing.T) {
	for _, tc := range []struct{ family, kernel, shell string }{
		{"Proxmox VE", "kernel http://s/linux quiet splash=silent ramdisk_size=16777216", "proxdebug"},
		{"Fedora (live)", "kernel http://s/linux quiet rd.live.image", "rd.break=pre-mount"},
		{"Debian live", "kernel http://s/vmlinuz boot=live quiet splash", "break=premount"},
		{"Ubuntu (casper)", "kernel http://s/vmlinuz boot=casper quiet splash ---", "break=premount"},
	} {
		commands := []string{"echo Starting", tc.kernel, "initrd http://s/initrd", "boot"}
		original := append([]string(nil), commands...)
		verbose := debugArgs(commands, config.DebugMode{Verbose: true}, tc.family)
		if strings.Contains(verbose[1], tc.shell) || strings.Contains(verbose[1], "proxdebug") || strings.Contains(verbose[1], "break=") {
			t.Errorf("%s: verbose start-up alone opened a shell: %s", tc.family, verbose[1])
		}
		shell := debugArgs(commands, config.DebugMode{Shell: true}, tc.family)
		if !strings.HasSuffix(shell[1], " "+tc.shell) || strings.Count(shell[1], tc.shell) != 1 {
			t.Errorf("%s: the shell switch alone: %s", tc.family, shell[1])
		}
		if strings.Contains(shell[1], "loglevel=7") || !strings.Contains(shell[1], "quiet") {
			t.Errorf("%s: the shell switch alone must not change the logging: %s", tc.family, shell[1])
		}
		all := config.DebugMode{Verbose: true, Shell: true, Serial: true}
		both := debugArgs(commands, all, tc.family)
		if !strings.Contains(both[1], tc.shell) || !strings.Contains(both[1], "loglevel=7") || !strings.HasSuffix(both[1], "console=tty0 console=ttyS0,115200n8") || strings.Contains(both[1], "quiet") {
			t.Errorf("%s: all three: %s", tc.family, both[1])
		}
		if again := debugArgs(both, all, tc.family); !reflect.DeepEqual(again, both) {
			t.Errorf("%s: a second pass changed the line: %v / %v", tc.family, both, again)
		}
		if !reflect.DeepEqual(commands, original) {
			t.Errorf("%s: the original commands were changed", tc.family)
		}
		entry := menu.Entry{Kind: tc.family, Commands: commands}
		if pc := forPC(entry, config.PCOptions{Debug: config.DebugMode{Shell: true}}); !reflect.DeepEqual(pc.Commands, shell) {
			t.Errorf("%s: a PC switch differs from an ISO switch: %v", tc.family, pc.Commands)
		}
	}
	for _, family := range []string{"Alpine Linux", "Arch Linux", "Tails", "Windows setup", ""} {
		commands := []string{"kernel http://s/linux quiet", "boot"}
		if got := debugArgs(commands, config.DebugMode{Shell: true}, family); !reflect.DeepEqual(got, commands) {
			t.Errorf("%q has no shell word: the kernel line must stay as it is, got %v", family, got)
		}
	}
	disk := []string{"sanboot --drive 0x80 x"}
	if got := debugArgs(disk, config.DebugMode{Shell: true}, "as a disk"); !reflect.DeepEqual(got, disk) {
		t.Errorf("a disk has no kernel line: %v", got)
	}
}
