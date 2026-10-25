// The other ways an ISO's own boot menu starts it ("safe graphics", an OEM install, a rescue entry, text mode ...), offered next to the usual start. Each is an entry of the ISO's
// menu (package bootcfg) started the way the generic recognition starts an entry: the same strategy and extra archive, with that entry's kernel, start-up disk and arguments.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"fmt"
	"strconv"
	"strings"
)

// Way is one entry of the ISO's own menu that Bootlace can start.
type Way struct {
	Title    string   // as the ISO's menu gives it, on one line
	Family   string   // what started it ("Ubuntu-style (casper), by its boot menu")
	Commands []string // iPXE commands, like Boot.Commands
	Special  bool     // a way other than the ordinary one: safe graphics, a rescue entry, a check of the disc ...
}

// maxWays is how many ways one ISO offers; maxWayDisks is how many different start-up disks of it are looked into (a menu often starts the same one in several ways).
const (
	maxWays     = 12
	maxWayDisks = 6
	maxWayTitle = 70
)

// Ways are the entries of the ISO's own menu that can be started the way `primary` (the usual start) is: the same kinds of start-up disk, and no extra archive other than the one
// the usual start uses (the server offers one archive per ISO). The usual start itself is left out. The order is the menu's, the ordinary ones first.
func Ways(c *Context, primary Boot) []Way {
	var out []Way
	seen := map[string]bool{strings.Join(primary.Commands, "\n"): true}
	disks := map[string]bool{}
	titles := map[string]int{}
	usual := firstInitrdCommand(primary.Commands)
	for _, m := range c.allMenuEntries() {
		key := m.vol.URL + m.initrd
		if !disks[key] && len(disks) == maxWayDisks {
			continue
		}
		disks[key] = true
		fa, ok := c.Findings.remember(c.Stamp+"|"+key, func() (facts, bool) { return inspectStartUpDisk(m) })
		if !ok {
			continue
		}
		b, ok := c.fromFacts(m, fa)
		if !ok || b.Kind == "" || b.Kind != primary.Kind { // only a start of the same kind as the usual one: another strategy may not suit what the ISO is
			continue
		}
		// the archive that adds network drivers is made from one start-up disk: another way may use it only when it is that same disk, or when neither needs one of its own
		if firstInitrdCommand(b.Commands) != usual && (b.HasOwnExtra() || primary.HasOwnExtra()) {
			continue
		}
		joined := strings.Join(b.Commands, "\n")
		if seen[joined] {
			continue
		}
		seen[joined] = true
		title := wayTitle(m.Title, len(out))
		if titles[title]++; titles[title] > 1 { // the same words for two different starts (a BIOS and a UEFI entry): told apart by a number
			title = fmt.Sprintf("%s (%d)", title, titles[title])
		}
		out = append(out, Way{Title: title, Family: b.Family, Commands: b.Commands, Special: special(m.Entry)})
		if len(out) == maxWays {
			break
		}
	}
	return out
}

// firstInitrdCommand is the first "initrd" line of the commands, which names the ISO's own start-up disk.
func firstInitrdCommand(cmds []string) string {
	for _, c := range cmds {
		if strings.HasPrefix(c, "initrd ") {
			return c
		}
	}
	return ""
}

// wayTitle is a menu title kept on one line, without anything iPXE or a table could misread, and not too long. A menu entry without a title is numbered.
func wayTitle(title string, index int) string {
	t := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		switch {
		case r == '^': // syslinux marks the key of an entry with it
			return -1
		case r < 0x20 || r == 0x7f || r == '\\':
			return ' '
		}
		return r
	}, title)), " ")
	t = strings.NewReplacer("${", "$ {").Replace(t)
	if t == "" {
		return "Entry " + strconv.Itoa(index+1)
	}
	if r := []rune(t); len(r) > maxWayTitle {
		t = string(r[:maxWayTitle-2]) + ".."
	}
	return t
}
