// GRUB's menu and systemd-boot's entries. GRUB's is a script: only what a menu entry needs of it is read (set, menuentry, linux, initrd, source), and a variable that is not
// set by a plain "set" stays unresolved (its argument is then left out, see Without).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package bootcfg

import (
	"regexp"
	"strings"
)

var variable = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

func parseGrub(text string, inc Include, depth int, vars map[string]string) []Entry {
	var out []Entry
	var cur *Entry
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = expand(line, vars)
		key, rest := splitKey(line)
		switch key {
		case "set":
			if name, val, ok := strings.Cut(rest, "="); ok && !strings.HasPrefix(name, "-") {
				vars[strings.TrimSpace(name)] = strings.Trim(val, `"'`)
			}
		case "menuentry":
			cur = &Entry{Title: menuTitle(rest)}
		case "}":
			if cur != nil && cur.Kernel != "" {
				out = append(out, *cur)
			}
			cur = nil
		case "linux", "linuxefi", "linux16":
			if cur != nil {
				cur.Kernel, cur.Args = splitKey(rest)
			}
		case "initrd", "initrdefi", "initrd16":
			if cur != nil {
				cur.Initrds = append(cur.Initrds, strings.Fields(rest)...)
			}
		case "source", ".", "configfile":
			if inc != nil && depth < maxDepth {
				if sub, ok := inc(firstWord(rest)); ok {
					out = append(out, parseGrub(sub, inc, depth+1, vars)...)
				}
			}
		}
	}
	return out
}

// expand puts in the variables that were set; one that was not stays as it is.
func expand(line string, vars map[string]string) string {
	return variable.ReplaceAllStringFunc(line, func(m string) string {
		if v, ok := vars[variable.FindStringSubmatch(m)[1]]; ok {
			return v
		}
		return m
	})
}

// menuTitle is the first quoted text of a menuentry line.
func menuTitle(rest string) string {
	for _, q := range []string{`"`, `'`} {
		if i := strings.Index(rest, q); i >= 0 {
			if j := strings.Index(rest[i+1:], q); j >= 0 {
				return rest[i+1 : i+1+j]
			}
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(rest, "{")), " ")
}

// parseLoaderEntry reads one systemd-boot entry file (title, linux, initrd, options).
func parseLoaderEntry(text string) []Entry {
	var e Entry
	for _, raw := range strings.Split(text, "\n") {
		key, rest := splitKey(strings.TrimSpace(strings.TrimRight(raw, "\r")))
		switch key {
		case "title":
			e.Title = rest
		case "linux":
			e.Kernel = rest
		case "initrd":
			e.Initrds = append(e.Initrds, rest)
		case "options":
			e.Args = rest
		}
	}
	if e.Kernel == "" {
		return nil
	}
	return []Entry{e}
}
