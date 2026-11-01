// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

func visibleWays(cfg config.Config, e entryInfo) []family.Way {
	var ways []family.Way
	for _, w := range e.Ways {
		if !cfg.ISOEntries[e.ISO.ID].Entries[w.ID].Hidden {
			ways = append(ways, w)
		}
	}
	return ways
}

func configuredWay(cfg config.Config, iso string, w family.Way) menu.Entry {
	options := cfg.ISOEntries[iso].Entries[w.ID]
	title := w.Title
	if options.Title != "" {
		title = options.Title
	}
	commands := addArgs(addArgs(w.Commands, cfg.Args[iso]), options.Args)
	commands = debugArgs(commands, cfg.Debug[iso], w.Family)
	return menu.Entry{Name: title, Title: title, Kind: w.Family, Commands: commands}
}

// A stale or unavailable choice falls back to the usual start. Cached recognition
// data stays untouched: settings and per-PC arguments are applied to copies.
func selectedEntry(cfg config.Config, e entryInfo) menu.Entry {
	if entry, ok := chosenWay(cfg, e, cfg.ISOEntries[e.ISO.ID].Default); ok {
		return entry
	}
	return e.Entry
}

// ruleEntry is what a rule's action starts if it names this ISO: "iso:<id>" the ISO's chosen start, "iso:<id>@<entry>" that entry of its own boot menu, and "native:<id>" the usual
// start (a native rule that reaches iPXE: Secure Boot is off, so the usual way does). ok is false when the action names another ISO, or an entry that is gone or hidden.
func ruleEntry(cfg config.Config, e entryInfo, action string) (menu.Entry, bool) {
	if action == "native:"+e.ISO.ID {
		return e.Entry, true
	}
	id, way, ok := config.ISOAction(action)
	if !ok || id != e.ISO.ID {
		return menu.Entry{}, false
	}
	if way == "" {
		return e.Entry, true
	}
	return chosenWay(cfg, e, way)
}

// chosenWay is the entry of the ISO's own boot menu with this id, as it is started in the ISO's place (it keeps the ISO's name, title and size); ok is false when there is no such
// visible entry (an empty id, one that went away, a hidden one).
func chosenWay(cfg config.Config, e entryInfo, id string) (menu.Entry, bool) {
	for _, w := range visibleWays(cfg, e) {
		if id != "" && w.ID == id {
			entry := configuredWay(cfg, e.ISO.ID, w)
			entry.Name, entry.Title, entry.Size = e.Entry.Name, e.Entry.Title, e.Entry.Size
			return entry, true
		}
	}
	return menu.Entry{}, false
}
