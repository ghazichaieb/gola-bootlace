package menu

import (
	"strings"
	"testing"
)

// FuzzNamesCannotAddLinesToTheScript: names and titles come from files in the library and from the ISOs themselves, and the menu is an iPXE script in which every line is a
// command. Whatever a name holds, the script must have exactly the lines it has for a harmless name, in the text and in the picture and BIOS shapes.
func FuzzNamesCannotAddLinesToTheScript(f *testing.F) {
	f.Add("Ubuntu 24.04", "Ubuntu", "live", "5.8 GB")
	f.Add("x\nshell\n", "t\r\nchain http://evil/", "k\x00", "s")
	f.Add("a || goto shell", "$ {x} ;", "&& reboot", "|")
	f.Fuzz(func(t *testing.T, name, title, kind, size string) {
		count := func(n, ti, k, s string, o Options) int {
			entries := []Entry{{Name: n, Title: ti, Kind: k, Size: s, Commands: []string{"echo go", "boot"}}, {Name: "b", Commands: []string{"boot"}}, {Name: "c", Commands: []string{"boot"}}}
			return strings.Count(ScriptWith("Gola Bootlace", entries, o), "\n")
		}
		for _, o := range []Options{{}, {Ascii: true, Pages: 2, Page: 1, Next: "http://x/menu?page=2"}, {Picture: "http://x/bg.png", Width: 1024, Height: 768, Prev: "http://x/menu"}} {
			if got, want := count(name, title, kind, size, o), count("a", "A", "live", "1 GB", o); got != want {
				t.Fatalf("%q %q %q %q: %d lines instead of %d (options %+v)", name, title, kind, size, got, want, o)
			}
		}
	})
}
