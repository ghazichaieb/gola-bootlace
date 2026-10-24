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
	// Guard is run before Commands by the menu (not by a rule, which is a deliberate start): lines that end in `goto denied` when the person may not go on. They are
	// written as they are, without the "|| goto failed" that Commands get.
	Guard []string

	// For the text table of a BIOS screen (Options.Ascii): the name, what kind of start it gets and its size, one to a column. Without them the table
	// shows Name in the first column.
	Title, Kind, Size string
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
	// Focus is the line that starts highlighted when nothing else is chosen by default: "next" or "prev" (the page buttons), for the pages a person
	// reached with the buttons, so that going on through the pages takes one key press each.
	Focus string

	// Ascii draws the text screen as a minimal list on black, with the logo (see Logo) above it. For a BIOS PC, whose screen is code page 437: the blanks
	// that line things up are the byte 0xFF there.
	Ascii bool

	// Again is the address of the menu to go back to when something did not work (and for the entries that say "try again in a minute"): empty goes to
	// the iPXE shell. It is a page of the menu that is shown without the PC's rule and without a timeout, so that a failure never ends at the PC's disk.
	Again string

	// Accent is the colour of the highlighted line and of the section headings, one of AccentNames; empty is Gola's red.
	Accent string
}

// accents are the colours of iPXE's palette that read well as the highlighted line: the background of the chosen line and the text colour on it.
var accents = map[string]struct{ colour, text int }{
	"red": {1, 7}, "green": {2, 0}, "yellow": {3, 0}, "blue": {4, 7}, "magenta": {5, 7}, "cyan": {6, 0}, "white": {7, 0},
}

// AccentNames are the colours that can be chosen as Accent.
var AccentNames = []string{"red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// cpairs sets iPXE's colour pairs: white on black, the chosen line in the accent colour and the section headings in it on black. (The BIOS build of iPXE has
// no colour command: the guard keeps the script going.)
func cpairs(accent string) string {
	a, ok := accents[accent]
	if !ok {
		a = accents["red"]
	}
	return fmt.Sprintf("cpair --foreground 7 --background 0 0 || set colours default\ncpair --foreground 7 --background 0 1 || set colours default\ncpair --foreground %d --background %d 2 || set colours default\ncpair --foreground %d --background 0 3 || set colours default\n", a.text, a.colour, a.colour)
}

// failedPrompt is what a failed start says: the message stays on screen for 20 seconds (a key press ends the wait), then the menu is shown again.
const failedPrompt = "Press a key to go back to the menu now, or wait 20 seconds."

// again is the label that every failure and every "try again later" goes to: the menu again (or the shell where there is no address of it).
func again(addr string) string {
	if addr == "" {
		return ":again\nshell\n"
	}
	return ":again\nchain " + addr + " || goto shell\n"
}

// Logo is the picture a text-mode PC (BIOS, or a screen that cannot show the logo) gets above the list: the lace mark, with the name and what the program is at its
// right. iPXE joins the words of an item with single spaces, so the spaces that line it up are written as byte 0xFF, which a BIOS text screen (code page 437) shows
// as a blank. (Large letters and block characters were tried: a BIOS text screen shows them badly.)
func Logo() []string {
	rows := []string{
		`o \ / o`,
		`o  X  o    GOLA Bootlace`,
		`o / \ o    network boot for ISO files and disk images`,
	}
	// the rows are moved together, so that the circles stay in line: the widest one is centred
	widest := 0
	for _, r := range rows {
		if len(r) > widest {
			widest = len(r)
		}
	}
	pad := (lineLen - widest) / 2
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.ReplaceAll(strings.Repeat(" ", pad)+r, " ", blank)) // (a backslash is shown as it is, but one at the very end of a line would join the next line: none of the rows ends in one)
	}
	return out
}

// Widths of the list: "  " size "  " name "  " kind, and of the lines across the screen.
const (
	colName  = 46
	colKind  = 16
	colSize  = 8
	lineLen  = 2 + colSize + 2 + colName + 2 + colKind
	logoLeft = 12
)

// blank is what lines text up in a BIOS text screen: iPXE joins the words of an item with single spaces, so a real space cannot be repeated.
const blank = "\xff"

// cell cuts s to n characters (ending in ".." when it was longer) and fills it up with blanks; right puts the text at the right.
func cell(s string, n int, right bool) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ", "${", "$ {").Replace(strings.TrimSpace(s))
	if len(s) > n {
		s = s[:n-2] + ".."
	}
	fill := strings.Repeat(" ", n-len(s))
	if right {
		s = fill + s
	} else {
		s += fill
	}
	return strings.ReplaceAll(s, " ", blank)
}

// centre puts text in the middle of the list's width (blanks as the filler).
func centre(text string) string {
	w := 0
	for range text {
		w++
	}
	if w >= lineLen {
		return strings.ReplaceAll(text, " ", blank)
	}
	return strings.ReplaceAll(strings.Repeat(" ", (lineLen-w)/2)+text, " ", blank)
}

// section is a line across the list with a title in the middle: "------ ISO Boot Menu ------".
func section(title string) string {
	t := " " + title + " "
	fill := lineLen - len(t)
	if fill < 4 {
		return strings.ReplaceAll(t, " ", blank)
	}
	return strings.Repeat("-", fill/2) + strings.ReplaceAll(t, " ", blank) + strings.Repeat("-", fill-fill/2)
}

// listRow is one ISO of the list: its size, its name and what kind of start it gets.
func listRow(name, kind, size string) string {
	return blank + blank + cell(size, colSize, true) + blank + blank + cell(name, colName, false) + blank + blank + cell(kind, colKind, false)
}

// plainRow is a line of text in the list's margin.
func plainRow(text string) string { return blank + blank + cell(text, lineLen-2, false) }

// ScriptWith is Script with options.
func ScriptWith(title string, entries []Entry, o Options) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	// a page reached with the page buttons keeps the highlight on the button that goes on (Previous on the last page); it comes before the PC's default, otherwise the
	// usual default "start from the PC's own disk" would pull the highlight back to that line on every page
	if o.Focus == "next" && o.Next != "" {
		b.WriteString("set menu-default next\n")
	} else if o.Focus != "" && o.Prev != "" {
		b.WriteString("set menu-default prev\n")
	} else if o.Default < 0 {
		b.WriteString("set menu-default local\n")
	} else if o.Default >= 1 && o.Default <= len(entries) {
		fmt.Fprintf(&b, "set menu-default %s\n", label(entries[o.Default-1].Name, o.Default-1))
	} else {
		b.WriteString("set menu-default shell\n")
	}
	gap := func(text string) { fmt.Fprintf(&b, "item --gap -- %s\n", text) }
	// the list: one line to an ISO, then the way to other pages and the three fixed lines; ascii puts it in a table
	items := func(ascii bool) {
		line := func(label, text string) {
			if ascii {
				fmt.Fprintf(&b, "item %s %s\n", label, plainRow(text))
			} else {
				fmt.Fprintf(&b, "item %s %s\n", label, text)
			}
		}
		if ascii {
			for _, l := range Logo() {
				gap(l)
			}
			gap(section("ISO Boot Menu" + pageNote(o, " - ")))
		}
		if len(entries) == 0 {
			if ascii {
				gap(plainRow("No ISO files were found in the server's folder."))
			} else {
				gap("No ISO files were found in the server's folder.")
			}
		}
		for i, e := range entries {
			switch {
			case !ascii:
				fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), clean(e.Name))
			case e.Title != "":
				fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), listRow(e.Title, e.Kind, e.Size))
			default:
				fmt.Fprintf(&b, "item %s %s\n", label(e.Name, i), listRow(e.Name, "", ""))
			}
		}
		if o.Prev != "" || o.Next != "" {
			if !ascii {
				gap("")
			}
			if o.Prev != "" {
				line("prev", "<< Previous page")
			}
			if o.Next != "" {
				line("next", "Next page >>")
			}
		}
		if ascii {
			gap(section("Tools"))
		} else {
			gap("")
		}
		line("local", "Boot from this PC's own disk")
		line("shell", "iPXE shell")
		line("reboot", "Restart this PC")
	}
	if o.Picture != "" {
		if o.Accent != "" { // (a chosen accent colour also colours the picture menu; without one it keeps iPXE's own colours)
			b.WriteString(cpairs(o.Accent))
		}
		// the picture carries the logo, so the menu's own heading only says what to do; a PC that cannot show it falls back to the text menu with the name
		// the picture is set up once: a later page (reached with chain) finds the console as it is, so changing pages only redraws the list instead of blanking the
		// screen and downloading the picture again
		b.WriteString("isset ${gfx-ready} && goto gfx || goto setup\n:setup\n") // (isset fails when the setting is empty, and a failing line without || ends the script)
		fmt.Fprintf(&b, "console --x %d --y %d --picture %s --left 64 --right 64 --top %d --bottom 40 && set gfx-ready 1 && goto gfx || goto plain\n", o.Width, o.Height, o.Picture, o.Top)
		fmt.Fprintf(&b, ":gfx\nmenu Choose what to start%s\n", pageNote(o, " - "))
		gap("")
		items(false)
		b.WriteString("goto choose\n:plain\n")
	}
	// the text screen: Gola's red for the chosen line, on black
	// (the BIOS build of iPXE has no colour command: the guard keeps the script going)
	b.WriteString(cpairs(o.Accent))
	fmt.Fprintf(&b, "menu %s%s\n", clean(title), pageNote(o, " - "))
	if !o.Ascii {
		gap("")
	}
	items(o.Ascii)
	b.WriteString(":choose\n")
	if o.Timeout > 0 {
		fmt.Fprintf(&b, "choose --default ${menu-default} --timeout %d selected || goto shell\n", o.Timeout*1000)
	} else {
		b.WriteString("choose --default ${menu-default} selected || goto shell\n")
	}
	b.WriteString("goto ${selected}\n\n")
	for i, e := range entries {
		fmt.Fprintf(&b, ":%s\n", label(e.Name, i))
		for _, g := range e.Guard {
			b.WriteString(g + "\n")
		}
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
	b.WriteString(":failed\necho Booting did not work: read the message above.\nprompt --timeout 20000 " + failedPrompt + " || echo\ngoto again\n\n")
	b.WriteString(":denied\necho Wrong password, or none given. Back to the menu in 3 seconds.\nsleep 3\ngoto again\n\n")
	b.WriteString(again(o.Again) + "\n")
	b.WriteString(":local\necho Leaving the network boot: the PC goes on to its own disk.\nexit\n\n:reboot\nreboot\n\n:shell\nshell\n")
	return b.String()
}

// Direct is the script for a PC that a rule sends straight to one entry: no menu, the entry's commands, and the same way out when they fail.
func Direct(why string, e Entry, back string) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	fmt.Fprintf(&b, "echo %s\n", clean(why))
	for _, c := range e.Commands {
		fmt.Fprintf(&b, "%s || goto failed\n", c)
	}
	b.WriteString("goto failed\n\n")
	b.WriteString(":failed\necho Booting did not work: read the message above.\nprompt --timeout 20000 " + failedPrompt + " || echo\ngoto again\n\n")
	b.WriteString(again(back))
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
