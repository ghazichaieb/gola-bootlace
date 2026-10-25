// A warning for answer files that hold a password in plain text (issue #60). The address of an answer file works without a login, so any host of the network that can fetch it
// reads what is in it; a hash is the least a password there should be. This finds the lines that carry a password in the form of the kind of file; it does not refuse anything.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package config

import (
	"fmt"
	"regexp"
	"strings"
)

// PlainPassword is one line of an answer file with a password that is not a hash.
type PlainPassword struct {
	Line  int    `json:"line"`  // 1 for the first line
	Where string `json:"where"` // the setting, for example "d-i passwd/user-password"
}

func (p PlainPassword) String() string { return fmt.Sprintf("line %d: %s", p.Line, p.Where) }

var (
	// Debian preseed: the questions that take the password itself (the -crypted ones take a hash); the value is the rest of the line after the type word.
	preseedPlain = regexp.MustCompile(`^\s*(?:d-i|ubiquity)\s+((?:passwd|user-setup)/(?:root-|user-)?password(?:-again)?|partman-crypto/passphrase(?:-again)?|network-console/password(?:-again)?|netcfg/wireless_(?:wpa|wep))\s+(?:password|string)\s+(\S.*)$`)
	// Ubuntu autoinstall / cloud-config: `password: x` (identity), `passwd: x` and `plain_text_passwd: x` of a user, `chpasswd: list` entries `user:password`, `PASSWORD=` and so on.
	cloudPlain   = regexp.MustCompile(`^\s*-?\s*(password|passwd|plain_text_passwd):\s*(\S.*?)\s*$`)
	cloudChpass  = regexp.MustCompile(`^\s*-\s*['"]?[A-Za-z0-9_.-]+:(\S+?)['"]?\s*$`)
	kickRoot     = regexp.MustCompile(`(?i)^\s*(rootpw|sshpw|user|grub2-password|bootloader)\b(.*)$`)
	yastPassword = regexp.MustCompile(`<(user_password|root_password)>\s*([^<\s][^<]*?)\s*</`)
	yastEncoded  = regexp.MustCompile(`(?i)<encrypted[^>]*>\s*true\s*</encrypted>`)
)

// isHash says whether a value is a password hash in the crypt format ($1$, $5$, $6$, $y$, $2b$ ...) or a placeholder of the templates.
func isHash(v string) bool {
	v = strings.Trim(strings.TrimSpace(v), `'"`)
	return strings.HasPrefix(v, "$") && strings.Count(v, "$") >= 2 || strings.HasPrefix(v, "!") || strings.HasPrefix(v, "*") || strings.HasPrefix(v, "PUT-") || strings.Contains(v, "{{") || v == "" || v == Masked
}

// PlainPasswords lists the lines of an answer file of this kind that hold a password in plain text. A hash, a locked password, an empty value, a placeholder of a template and a
// {{word}} are not.
func PlainPasswords(kind, text string) []PlainPassword {
	var out []PlainPassword
	lines := strings.Split(text, "\n")
	switch kind {
	case "preseed":
		for i, l := range lines {
			if m := preseedPlain.FindStringSubmatch(l); m != nil && !isHash(m[2]) {
				out = append(out, PlainPassword{i + 1, "d-i " + m[1]})
			}
		}
	case "autoinstall":
		inChpasswd := false
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				continue
			}
			if m := cloudPlain.FindStringSubmatch(l); m != nil && !isHash(m[2]) && m[2] != "|" && m[2] != ">" && m[2] != "null" && m[2] != "~" {
				out = append(out, PlainPassword{i + 1, m[1]})
			}
			if strings.HasPrefix(t, "chpasswd:") || strings.HasPrefix(t, "list:") {
				inChpasswd = true
				continue
			}
			if inChpasswd {
				if m := cloudChpass.FindStringSubmatch(l); m != nil && !isHash(m[1]) {
					out = append(out, PlainPassword{i + 1, "chpasswd list"})
				} else if t != "" && !strings.HasPrefix(t, "-") {
					inChpasswd = false
				}
			}
		}
	case "kickstart":
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				continue
			}
			m := kickRoot.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			cmd, rest := strings.ToLower(m[1]), m[2]
			switch cmd {
			case "rootpw", "sshpw":
				if strings.Contains(rest, "--lock") || strings.Contains(rest, "--iscrypted") && !strings.Contains(rest, "--plaintext") {
					continue
				}
				if f := strings.Fields(rest); len(f) > 0 && !isHash(f[len(f)-1]) {
					out = append(out, PlainPassword{i + 1, cmd})
				}
			case "user":
				for _, w := range strings.Fields(rest) {
					if v, ok := strings.CutPrefix(w, "--password="); ok && !strings.Contains(rest, "--iscrypted") && !isHash(v) {
						out = append(out, PlainPassword{i + 1, "user --password"})
					}
				}
			case "grub2-password", "bootloader":
				for _, w := range strings.Fields(rest) {
					if v, ok := strings.CutPrefix(w, "--password="); ok && !isHash(v) {
						out = append(out, PlainPassword{i + 1, cmd + " --password"})
					}
				}
			}
		}
	case "autoyast":
		// a password counts as plain unless its <user> block says it is encrypted
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "<!--") {
				continue
			}
			m := yastPassword.FindStringSubmatch(l)
			if m == nil || isHash(m[2]) {
				continue
			}
			from, to := i, i
			for from > 0 && !strings.Contains(lines[from], "<user>") && !strings.Contains(lines[from], "<user ") {
				from--
			}
			for to < len(lines)-1 && !strings.Contains(lines[to], "</user>") {
				to++
			}
			if !yastEncoded.MatchString(strings.Join(lines[from:to+1], "\n")) {
				out = append(out, PlainPassword{i + 1, m[1]})
			}
		}
	}
	return out
}
