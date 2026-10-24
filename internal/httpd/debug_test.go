package httpd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

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
