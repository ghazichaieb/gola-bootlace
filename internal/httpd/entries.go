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
	choice := cfg.ISOEntries[e.ISO.ID].Default
	if choice != "" {
		for _, w := range visibleWays(cfg, e) {
			if w.ID == choice {
				entry := configuredWay(cfg, e.ISO.ID, w)
				entry.Name, entry.Title, entry.Size = e.Entry.Name, e.Entry.Title, e.Entry.Size
				return entry
			}
		}
	}
	return e.Entry
}
