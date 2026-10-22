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

	// Picture is the address of a PNG of Width x Height pixels shown behind the menu on a PC whose iPXE has a graphical console (UEFI builds);
	// the text starts Top pixels from the top. Empty keeps the plain text menu. A PC without graphical console gets the text menu anyway.
	Picture       string
	Width, Height int
	Top           int

	// Page of Pages: a long list is shown a page at a time, with a way to the page before and after (the addresses of those menus; empty where
	// there is none). The entries given are the ones of this page.
	Page, Pages int
	Prev, Next  string

	// TextBanner puts the ASCII mark (Banner) above the list of a text-mode screen. Only for a BIOS PC: its screen is code page 437, which is what the blanks need.
	TextBanner bool
}

// Banner is the picture a text-mode PC (BIOS, or a screen that cannot show the logo) gets above the list: the lace mark and the name, in plain ASCII.
// iPXE joins the words of an item with single spaces, so the spaces that line the mark up are written as byte 0xFF, which a BIOS text screen (code page 437)
// shows as a blank.
var Banner = []string{
	`  o \ / o`,
	`  o  X  o    GOLA Bootlace`,
	`  o / \ o    network boot for ISO files`,
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
	if o.Picture != "" {
		// the picture carries the logo, so the menu's own heading only says what to do; a PC that cannot show it falls back to the text menu with the name
		fmt.Fprintf(&b, "console --x %d --y %d --picture %s --left 64 --right 64 --top %d --bottom 40 && goto gfx || goto plain\n", o.Width, o.Height, o.Picture, o.Top)
		fmt.Fprintf(&b, ":gfx\nmenu Choose what to start%s\ngoto items\n:plain\n", pageNote(o, " - "))
	}
	// the text screen: Gola's red for the chosen line, on black, with the lace mark above the list
	// (the BIOS build of iPXE has no colour command: the guard keeps the script going)
	b.WriteString("cpair --foreground 7 --background 0 0 || set colours default\ncpair --foreground 7 --background 1 2 || set colours default\n")
	fmt.Fprintf(&b, "menu %s%s\n", clean(title), pageNote(o, " - "))
	if o.TextBanner {
		for _, line := range Banner {
			fmt.Fprintf(&b, "item --gap -- %s\n", strings.ReplaceAll(line, " ", "\xff"))
		}
	}
	b.WriteString("item --gap --\n")
	if o.Picture != "" {
		b.WriteString(":items\n")
	}
	if len(entries) == 0 {
		b.WriteString("item --gap -- No ISO files were found in the server's folder.\n")
	}
	for i, e := range entries {
		fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), clean(e.Name))
	}
	if o.Prev != "" || o.Next != "" {
		b.WriteString("item --gap --\n")
		if o.Prev != "" {
			b.WriteString("item prev << Previous page\n")
		}
		if o.Next != "" {
			b.WriteString("item next Next page >>\n")
		}
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
	if o.Prev != "" {
		fmt.Fprintf(&b, ":prev\nchain %s || goto shell\n\n", o.Prev)
	}
	if o.Next != "" {
		fmt.Fprintf(&b, ":next\nchain %s || goto shell\n\n", o.Next)
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

func pageNote(o Options, sep string) string {
	if o.Pages > 1 {
		return fmt.Sprintf("%spage %d of %d", sep, o.Page, o.Pages)
	}
	return ""
}
