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
func Script(title string, entries []Entry) string { return ScriptWith(title, entries, Options{}) }

// Options changes how the menu starts. By default it waits for a choice and offers the iPXE shell first, so that a PC is never sent anywhere by accident.
type Options struct {
	Default int // the entry (counting from 1) that is chosen when the time runs out; 0 keeps the shell; -1 goes on to the PC's own disk
	Timeout int // seconds to wait before choosing it; 0 waits for ever
}

// ScriptWith is Script with options.
func ScriptWith(title string, entries []Entry, o Options) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	if o.Default < 0 {
		b.WriteString("set menu-default local\n")
	} else if o.Default >= 1 && o.Default <= len(entries) {
		fmt.Fprintf(&b, "set menu-default %s\n", label(entries[o.Default-1].Name, o.Default-1))
	} else {
		b.WriteString("set menu-default shell\n")
	}
	fmt.Fprintf(&b, "menu %s\n", clean(title))
	if len(entries) == 0 {
		b.WriteString("item --gap -- No ISO files were found in the server's folder.\n")
	}
	for i, e := range entries {
		fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), clean(e.Name))
	}
	b.WriteString("item --gap --\n")
	b.WriteString("item local Boot from this PC's own disk\n")
	b.WriteString("item shell iPXE shell\n")
	b.WriteString("item reboot Restart this PC\n")
	if o.Timeout > 0 {
		fmt.Fprintf(&b, "choose --default ${menu-default} --timeout %d selected || goto shell\n", o.Timeout*1000)
	} else {
		b.WriteString("choose --default ${menu-default} selected || goto shell\n")
	}
	b.WriteString("goto ${selected}\n\n")
	for i, e := range entries {
		fmt.Fprintf(&b, ":%s\n", label(e.Name, i))
		for _, c := range e.Commands {
			fmt.Fprintf(&b, "%s || goto failed\n", c)
		}
		b.WriteString("goto failed\n\n")
	}
	b.WriteString(":failed\necho Booting did not work: read the message above, then press a key for the menu.\nprompt\nchain ${self} || goto shell\n\n")
	b.WriteString(":local\necho Leaving the network boot: the PC goes on to its own disk.\nexit\n\n:reboot\nreboot\n\n:shell\nshell\n")
	return b.String()
}

// Direct is the script for a PC that a rule sends straight to one entry: no menu, the entry's commands, and the same way out when they fail.
func Direct(why string, e Entry) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	fmt.Fprintf(&b, "echo %s\n", clean(why))
	for _, c := range e.Commands {
		fmt.Fprintf(&b, "%s || goto failed\n", c)
	}
	b.WriteString("goto failed\n\n")
	b.WriteString(":failed\necho Booting did not work: read the message above, then press a key for the shell.\nprompt\nshell\n")
	return b.String()
}

// Local is the script for a PC that a rule sends to its own disk.
func Local(why string) string {
	return "#!ipxe\necho " + clean(why) + "\nexit\n"
}
