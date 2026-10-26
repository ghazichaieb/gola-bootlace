// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ISOEntrySettings customize the compatible entries found in one ISO's menu.
// An empty Default keeps Bootlace's usual start. Missing entries are retained so
// temporarily unavailable recognition does not discard a person's settings.
type ISOEntrySettings struct {
	Default string                  `json:"default,omitempty"`
	Entries map[string]EntryOptions `json:"entries,omitempty"`
}

// EntryOptions apply to one boot payload; Args are appended to its own arguments.
type EntryOptions struct {
	Hidden bool   `json:"hidden,omitempty"`
	Title  string `json:"title,omitempty"`
	Args   string `json:"args,omitempty"`
}

var entryIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (c *Config) validateISOEntries() error {
	for id, settings := range c.ISOEntries {
		if !idPattern.MatchString(id) {
			return fmt.Errorf("invalid ISO id for boot entries: %q", id)
		}
		if settings.Default != "" && !entryIDPattern.MatchString(settings.Default) {
			return fmt.Errorf("invalid default boot entry")
		}
		if len(settings.Entries) > 128 {
			return fmt.Errorf("at most 128 saved boot entries per ISO")
		}
		for key, options := range settings.Entries {
			if !entryIDPattern.MatchString(key) {
				return fmt.Errorf("invalid boot entry id")
			}
			options.Title = strings.TrimSpace(options.Title)
			if utf8.RuneCountInString(options.Title) > 120 || strings.IndexFunc(options.Title, unicode.IsControl) >= 0 {
				return fmt.Errorf("an entry title must be one line of at most 120 characters")
			}
			args, err := CleanArgs(options.Args)
			if err != nil {
				return err
			}
			options.Args = args
			if options.Hidden && key == settings.Default {
				return fmt.Errorf("the default boot entry cannot be hidden")
			}
			settings.Entries[key] = options
		}
		c.ISOEntries[id] = settings
	}
	return nil
}
