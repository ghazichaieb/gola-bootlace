// Package menu writes the iPXE script that shows PC users the list of things they can boot.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package menu

import (
	"fmt"
	"regexp"
	"strings"
)

// Entry is one line of the menu: a name to show and the iPXE commands that boot it.
type Entry struct {
	Name     string
	Commands []string
}

var notLabel = regexp.MustCompile(`[^a-z0-9]+`)

// label makes the name iPXE uses inside the script: unique, lower case letters and digits, never empty.
func label(name string, index int) string {
	l := strings.Trim(notLabel.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(l) > 40 {
		l = l[:40]
	}
	return fmt.Sprintf("e%d-%s", index, l)
}

// clean keeps a name on one line and free of anything iPXE would read as a command.
func clean(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ", "${", "$ {").Replace(s)
	return strings.TrimSpace(s)
}

// Script is the iPXE script: a menu with one item per entry, a way to the iPXE shell, and a timeout-free choice. The commands of an entry
// run after the user picks it; the script always ends in the shell when a command fails, so the person sees the error instead of a reboot loop.
func Script(title string, entries []Entry) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	b.WriteString("set menu-default shell\n")
	fmt.Fprintf(&b, "menu %s\n", clean(title))
	if len(entries) == 0 {
		b.WriteString("item --gap -- No ISO files were found in the server's folder.\n")
	}
	for i, e := range entries {
		fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), clean(e.Name))
	}
	b.WriteString("item --gap --\n")
	b.WriteString("item shell iPXE shell\n")
	b.WriteString("item reboot Restart this PC\n")
	b.WriteString("choose --default ${menu-default} selected || goto shell\n")
	b.WriteString("goto ${selected}\n\n")
	for i, e := range entries {
		fmt.Fprintf(&b, ":%s\n", label(e.Name, i))
		for _, c := range e.Commands {
			fmt.Fprintf(&b, "%s || goto failed\n", c)
		}
		b.WriteString("goto failed\n\n")
	}
	b.WriteString(":failed\necho Booting did not work: read the message above, then press a key for the menu.\nprompt\nchain ${self} || goto shell\n\n")
	b.WriteString(":reboot\nreboot\n\n:shell\nshell\n")
	return b.String()
}
