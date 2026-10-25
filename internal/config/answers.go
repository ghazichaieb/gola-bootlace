// Answer files for unattended installs (issue #17): the file an installer reads so that it asks nothing (Debian's preseed, Ubuntu's autoinstall, Red Hat's kickstart, openSUSE's
// AutoYaST). The files are kept in the settings, attached by hand to an ISO or to one PC, and served by the web server at an address that goes on the kernel line. A file can hold
// passwords: it is a secret like the boot passwords (not in a backup without secrets), it is shown with the values masked unless asked for, and it is never written to the log.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// AnswerFile is one answer file of the library.
type AnswerFile struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // one of AnswerKinds
	Content string `json:"content"`
}

// AnswerKind is a kind of answer file: how the installer is told where it is, and the names it asks for under that address.
type AnswerKind struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Hint  string   `json:"hint"`  // for which systems
	Files []string `json:"files"` // the names the installer asks for under the address of the file (the first is the one the kernel is pointed at, except for autoinstall which takes the folder)
}

// AnswerKinds are the kinds of answer file Bootlace can hand to an installer.
var AnswerKinds = []AnswerKind{
	{ID: "preseed", Label: "Debian preseed", Hint: "the Debian installer (netinst and other installer ISOs)", Files: []string{"preseed.cfg"}},
	{ID: "autoinstall", Label: "Ubuntu autoinstall (cloud-init user-data)", Hint: "Ubuntu Server's live installer", Files: []string{"user-data", "meta-data", "vendor-data"}},
	{ID: "kickstart", Label: "Red Hat kickstart", Hint: "the installers of Fedora, Alma, Rocky, CentOS and Red Hat", Files: []string{"ks.cfg"}},
	{ID: "autoyast", Label: "openSUSE AutoYaST", Hint: "the installers of openSUSE and SUSE", Files: []string{"autoinst.xml"}},
	{ID: "unattend", Label: "Windows autounattend.xml", Hint: "Windows 10, Windows 11 and Windows Server Setup (the Windows set-up environment fetches it from the share)", Files: []string{"autounattend.xml"}},
}

// AnswerKindOf finds a kind by its id.
func AnswerKindOf(id string) (AnswerKind, bool) {
	for _, k := range AnswerKinds {
		if k.ID == id {
			return k, true
		}
	}
	return AnswerKind{}, false
}

// Limits of the library of answer files.
const (
	MaxAnswers     = 100
	MaxAnswerBytes = 256 << 10
	MaxAnswerName  = 80
)

// NoAnswer is the value of a PC's Answer that keeps an answer file away from that PC although its ISO has one.
const NoAnswer = "none"

// answerID is how the id of an answer file looks: lower case hex, not guessable from the id of another.
var answerID = regexp.MustCompile(`^[0-9a-f]{12,32}$`)

// ValidAnswerID says whether s can be the id of an answer file.
func ValidAnswerID(s string) bool { return answerID.MatchString(s) }

// CleanAnswerName checks the name a person gave a file: one line, 1 to MaxAnswerName characters.
func CleanAnswerName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > MaxAnswerName || strings.ContainsAny(s, "\r\n\x00") {
		return "", fmt.Errorf("the name of an answer file must be one line of 1 to %d characters", MaxAnswerName)
	}
	return s, nil
}

// CleanAnswerContent checks the text of a file: UTF-8, no NUL, at most MaxAnswerBytes.
func CleanAnswerContent(s string) error {
	if len(s) > MaxAnswerBytes {
		return fmt.Errorf("an answer file can be at most %d KB", MaxAnswerBytes>>10)
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return errors.New("an answer file must be text (UTF-8)")
	}
	return nil
}

// validateAnswers checks the library and the choices that name its files; a choice that names a file that is gone is dropped.
func (c *Config) validateAnswers() error {
	if len(c.Answers) > MaxAnswers {
		return fmt.Errorf("at most %d answer files can be kept", MaxAnswers)
	}
	for id, a := range c.Answers {
		if !ValidAnswerID(id) {
			return fmt.Errorf("%q is not the id of an answer file", id)
		}
		name, err := CleanAnswerName(a.Name)
		if err != nil {
			return err
		}
		if _, ok := AnswerKindOf(a.Kind); !ok {
			return fmt.Errorf("%q is not a kind of answer file", a.Kind)
		}
		if err := CleanAnswerContent(a.Content); err != nil {
			return err
		}
		if err := cleanXMLContent(a.Kind, a.Content); err != nil {
			return err
		}
		a.Name = name
		c.Answers[id] = a
	}
	for iso, id := range c.ISOAnswers {
		if !idPattern.MatchString(iso) {
			delete(c.ISOAnswers, iso)
			continue
		}
		if _, ok := c.Answers[id]; !ok {
			delete(c.ISOAnswers, iso)
		}
	}
	for mac, o := range c.PCOptions {
		if o.Answer == "" || o.Answer == NoAnswer {
			continue
		}
		if _, ok := c.Answers[o.Answer]; !ok {
			o.Answer = ""
			if o.Empty() {
				delete(c.PCOptions, mac)
			} else {
				c.PCOptions[mac] = o
			}
		}
	}
	return nil
}

// AnswerFor is the answer file a PC gets for an ISO: its own choice when it has one ("none" keeps the file away), otherwise the one attached to the ISO.
func (c Config) AnswerFor(isoID, mac string) (id string, a AnswerFile, ok bool) {
	id = c.ISOAnswers[isoID]
	if o, has := c.PCOptions[mac]; has && o.Answer != "" {
		if o.Answer == NoAnswer {
			return "", AnswerFile{}, false
		}
		id = o.Answer
	}
	a, ok = c.Answers[id]
	return id, a, ok
}

// Masking: the values of an answer file that look like secrets are shown as Masked, and a file saved with that text in a line takes the old line back.
const Masked = "********"

var (
	secretWord = regexp.MustCompile(`(?i)pass|secret|token|crypt|hash|rootpw|\bkey\b|psk`)
	// the shapes of a value in the files: `d-i x password VALUE`, `key: VALUE`, `key=VALUE`, `--password=VALUE`, <tag>VALUE</tag>
	maskXML   = regexp.MustCompile(`(<[^>/]+>)([^<]*)(</[^>]+>)`)
	maskFlag  = regexp.MustCompile(`(?i)(--[a-z-]*(?:pass|secret|key|crypt)[a-z-]*[= ])(\S+)`)
	maskKV    = regexp.MustCompile(`^(\s*-?\s*[^:=#]*[:=]\s*)(.*\S)(\s*)$`)
	maskKS    = regexp.MustCompile(`(?i)^(\s*(?:rootpw|sshpw)\s+(?:--\S+\s+)*)([^-\s]\S*)(\s*)$`)
	maskWords = regexp.MustCompile(`^(\s*\S+\s+\S+\s+\S+\s+)(\S.*)$`)
)

// maskLine hides the value in one line that looks like it holds a secret; other lines are returned as they are.
func maskLine(l string) string {
	if !secretWord.MatchString(l) {
		return l
	}
	if strings.Contains(l, Masked) {
		return l
	}
	t := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(t, "#"):
		return l // a comment
	case strings.HasPrefix(t, "<") && maskXML.MatchString(l):
		return maskXML.ReplaceAllStringFunc(l, func(m string) string {
			sub := maskXML.FindStringSubmatch(m)
			if !secretWord.MatchString(sub[1]) || strings.TrimSpace(sub[2]) == "" {
				return m
			}
			return sub[1] + Masked + sub[3]
		})
	case maskKS.MatchString(l): // kickstart's rootpw and sshpw take the password as the last word
		return maskKS.ReplaceAllString(l, "${1}"+Masked+"${3}")
	case maskFlag.MatchString(l):
		return maskFlag.ReplaceAllString(l, "${1}"+Masked)
	case strings.HasPrefix(t, "d-i ") || strings.HasPrefix(t, "ubiquity "):
		if m := maskWords.FindStringSubmatch(l); m != nil { // a preseed line: owner, question, type, value
			return m[1] + Masked
		}
	case maskKV.MatchString(l):
		m := maskKV.FindStringSubmatch(l)
		if secretWord.MatchString(m[1]) && m[2] != "|" && m[2] != ">" {
			return m[1] + Masked + m[3]
		}
	}
	return l
}

// maskText is the lines of an answer file with the values that look like secrets hidden (the carriage returns stay). Most secrets are found line by line; the <Value> of a Windows
// answer file's <Password> needs to know that it is inside one.
func maskText(text string) []string {
	lines := strings.Split(text, "\n")
	inValue := map[int]bool{}
	for _, i := range unattendValues(lines) {
		inValue[i] = true
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		cr := strings.HasSuffix(l, "\r")
		l = strings.TrimSuffix(l, "\r")
		if inValue[i] {
			l = unattendValue.ReplaceAllString(l, "${1}"+Masked+"${3}")
		} else {
			l = maskLine(l)
		}
		if cr {
			l += "\r"
		}
		out[i] = l
	}
	return out
}

// MaskAnswer is the text of an answer file with the values that look like secrets hidden.
func MaskAnswer(text string) string { return strings.Join(maskText(text), "\n") }

// RestoreMasked takes the old line back for each line of `edited` that still shows the mask in place of a value that was in `old`, so that saving a file that was shown masked keeps
// its secrets. A masked line that matches no old line stays as it is.
func RestoreMasked(old, edited string) string {
	byMasked := map[string][]string{}
	oldLines := strings.Split(old, "\n")
	for i, m := range maskText(old) {
		m = strings.TrimSuffix(m, "\r")
		if strings.Contains(m, Masked) {
			byMasked[m] = append(byMasked[m], oldLines[i])
		}
	}
	lines := strings.Split(edited, "\n")
	for i, l := range lines {
		bare := strings.TrimSuffix(l, "\r")
		if !strings.Contains(bare, Masked) {
			continue
		}
		if olds := byMasked[bare]; len(olds) > 0 {
			lines[i] = olds[0]
			if len(olds) > 1 {
				byMasked[bare] = olds[1:]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// cleanXMLContent refuses, in the kinds that are XML (AutoYaST, Windows), the control characters that XML 1.0 does not allow (a stray form feed or vertical tab, from a paste or
// from a "\v" in a path that was typed into a script): Windows Setup then stops with "an internal error while loading or searching for an unattend answer file", which says nothing
// about the cause.
func cleanXMLContent(kind, s string) error {
	if kind != "unattend" && kind != "autoyast" {
		return nil
	}
	for i, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			line := 1 + strings.Count(s[:i], "\n")
			return fmt.Errorf("line %d holds a control character (code %d) that XML does not allow: remove it", line, r)
		}
	}
	return nil
}
