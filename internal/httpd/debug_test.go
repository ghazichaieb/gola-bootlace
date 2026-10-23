package httpd

import (
	"reflect"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
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
