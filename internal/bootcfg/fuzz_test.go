package bootcfg

import (
	"strings"
	"testing"
	"time"
)

// FuzzParse feeds text to the three menu readers. Menus come from ISOs nobody has looked at, so whatever the text says the readers must come back, quickly, without a panic
// and without an entry that has a line break in a field that ends up in an iPXE script.
func FuzzParse(f *testing.F) {
	f.Add("isolinux.cfg", "DEFAULT live\nLABEL live\n  KERNEL /casper/vmlinuz\n  APPEND initrd=/casper/initrd boot=casper quiet splash ---\nINCLUDE isolinux.cfg\n")
	f.Add("grub.cfg", "set default=0\nmenuentry \"Try\" {\n  linux /casper/vmlinuz boot=casper $x quiet\n  initrd /casper/initrd\n}\nsource /boot/grub/grub.cfg\nsubmenu \"More\" {\n menuentry \"a\" { linux /k }\n}\n")
	f.Add("entry.conf", "title Live\nlinux /vmlinuz\ninitrd /initrd.img\noptions root=live:CDLABEL=X rd.live.image\n")
	f.Add("syslinux.cfg", "MENU INCLUDE a.cfg\nLABEL a\nKERNEL ${x}\nAPPEND ${y} ${z}\n")
	f.Fuzz(func(t *testing.T, name, text string) {
		start := time.Now()
		calls := 0
		inc := func(string) (string, bool) { // every include gives the same text back: a menu that includes itself must still end
			calls++
			return text, calls < 200
		}
		for _, e := range Parse(name, text, inc) {
			for _, field := range append([]string{e.Kernel, e.Args}, e.Initrds...) {
				if strings.ContainsAny(field, "\r\n") && field != e.Title {
					// (the title is cleaned where the menu is written, and the source is one of our own file names)
					t.Fatalf("a control character in %q", field)
				}
			}
		}
		if time.Since(start) > 3*time.Second {
			t.Fatalf("took %v for %d bytes", time.Since(start), len(text))
		}
	})
}

func TestAMenuLineWithAControlCharacterIsNotOfferedAndArgumentsAreCleaned(t *testing.T) {
	// found by fuzzing: a carriage return inside a menu line used to end up inside a kernel path
	if got := Parse("entry.conf", "title X\nlinux /vm\rlinuz\ninitrd /init\n", nil); len(got) != 0 {
		t.Errorf("a path with a control character must not be offered: %+v", got)
	}
	got := Parse("isolinux.cfg", "LABEL a\nKERNEL /vmlinuz\nAPPEND initrd=/i boot=live\x01 quiet\r\n", nil)
	if len(got) != 1 || strings.ContainsAny(got[0].Args, "\x01\r") || !strings.Contains(got[0].Args, "boot=live") {
		t.Errorf("arguments lose control characters, not their words: %+v", got)
	}
}
