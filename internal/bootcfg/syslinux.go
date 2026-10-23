// The syslinux and isolinux menus: "LABEL" starts an entry; KERNEL (or LINUX), INITRD and APPEND say what it starts.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package bootcfg

import "strings"

// maxDepth is how many menu files may include one another (a menu that includes itself must end).
const maxDepth = 4

func parseSyslinux(text string, inc Include, depth int) []Entry {
	var out []Entry
	var cur *Entry
	flush := func() {
		if cur != nil && cur.Kernel != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest := splitKey(line)
		switch strings.ToLower(key) {
		case "label":
			flush()
			cur = &Entry{Title: rest}
		case "menu":
			if sub, title := splitKey(rest); cur != nil && strings.EqualFold(sub, "label") {
				cur.Title = title
			}
		case "kernel", "linux":
			if cur != nil {
				cur.Kernel = firstWord(rest)
			}
		case "initrd":
			if cur != nil {
				cur.Initrds = append(cur.Initrds, strings.Split(firstWord(rest), ",")...)
			}
		case "append":
			if cur != nil {
				args, initrds := splitInitrdArg(rest)
				cur.Args, cur.Initrds = args, append(cur.Initrds, initrds...)
			}
		case "include", "config":
			if inc != nil && depth < maxDepth {
				if sub, ok := inc(firstWord(rest)); ok {
					flush()
					out = append(out, parseSyslinux(sub, inc, depth+1)...)
				}
			}
		}
	}
	flush()
	return out
}

// splitInitrdArg takes the "initrd=a,b" argument out of an APPEND line: syslinux's way to name the start-up disk of a kernel.
func splitInitrdArg(args string) (rest string, initrds []string) {
	var keep []string
	for _, t := range strings.Fields(args) {
		if v, ok := strings.CutPrefix(t, "initrd="); ok {
			initrds = append(initrds, strings.Split(v, ",")...)
			continue
		}
		if t == "-" {
			continue
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, " "), initrds
}

func firstWord(s string) string {
	w, _ := splitKey(strings.TrimSpace(s))
	return w
}
