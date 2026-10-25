// Answer files for unattended installs (issue #17): the address an installer is pointed at, and the file it finds there. A file is attached on purpose to an ISO or to one PC (package
// config); it is made for the PC that asks (its name, address and serial in place of the {{...}} words) and its text is never written to the log.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// answerPath is where an installer finds the file of a PC: /answers/<file id>/<MAC with dashes>/<name>. The id is not guessable and the file is only given while it is attached to
// something.
const answerPath = "/answers/"

// answerBase is the address of the folder an installer is pointed at for one PC (with a trailing slash).
func (s *Server) answerBase(id, mac string) string {
	if mac == "" {
		mac = "unknown"
	}
	return s.Base + answerPath + id + "/" + strings.ReplaceAll(mac, ":", "-") + "/"
}

// answerArgs are the kernel arguments that point an installer of the kind at its file.
func answerArgs(kind, base string) string {
	switch kind {
	case "preseed":
		// (the installer asks to retry mounting a CD before it can load a preseed over the network, and Bootlace never gives it one: the answer goes on the kernel line)
		return "auto=true priority=critical cdrom-detect/retry=false url=" + base + "preseed.cfg"
	case "autoinstall":
		return "autoinstall ds=nocloud-net;s=" + base
	case "kickstart":
		return "inst.ks=" + base + "ks.cfg"
	case "autoyast":
		return "autoyast=" + base + "autoinst.xml"
	}
	return ""
}

// insertArgs adds arguments to the kernel line of the commands. The installers of Debian and Ubuntu copy what follows a "---" into the installed system's kernel command line: the
// arguments go before it.
func insertArgs(commands []string, args string) []string {
	if args == "" {
		return commands
	}
	out := append([]string(nil), commands...)
	for i, c := range out {
		if !strings.HasPrefix(c, "kernel ") {
			continue
		}
		words := strings.Fields(c)
		at := len(words)
		for j, w := range words {
			if w == "---" {
				at = j
				break
			}
		}
		words = append(words[:at:at], append([]string{args}, words[at:]...)...)
		out[i] = strings.Join(words, " ")
		break
	}
	return out
}

// withAnswer gives a menu entry the answer file the PC is to get for the ISO, when there is one: the installer is pointed at it on the kernel line. An entry without a kernel line (an
// ISO handed over as a disk, Windows) is left as it is.
func (s *Server) withAnswer(cfg config.Config, isoID string, e menu.Entry, mac string) menu.Entry {
	id, a, ok := cfg.AnswerFor(isoID, mac)
	if !ok {
		return e
	}
	args := answerArgs(a.Kind, s.answerBase(id, mac))
	if a.Kind == "preseed" {
		// Network preseeding arrives after netcfg; the hostname must be
		// supplied at boot, using the same value as {{hostname}} in the file.
		// Keep an explicit ISO/PC hostname choice instead of overriding it.
		explicit := false
		for _, command := range e.Commands {
			if !strings.HasPrefix(command, "kernel ") {
				continue
			}
			for _, word := range strings.Fields(command) {
				if strings.HasPrefix(word, "hostname=") || strings.HasPrefix(word, "netcfg/get_hostname=") || strings.HasPrefix(word, "netcfg/hostname=") {
					explicit = true
				}
			}
			break
		}
		if !explicit {
			hostname := hostnameFor(cfg.PCNames[mac], mac)
			args += " hostname=" + hostname + " netcfg/hostname=" + hostname
		}
	}
	e.Commands = insertArgs(e.Commands, args)
	return e
}

// entryForPC is the entry as this PC gets it: its own kernel options, then its answer file.
func (s *Server) entryForPC(cfg config.Config, isoID string, e menu.Entry, pc config.PCOptions, mac string) menu.Entry {
	return s.withAnswer(cfg, isoID, forPC(e, pc), mac)
}

var (
	wordHostname = regexp.MustCompile(`[^a-z0-9-]+`)
	wordSerial   = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	wordAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
)

// hostnameFor is the name for a PC in a file: the name a person gave it made into a host name, or pc- and the end of its address.
func hostnameFor(name, mac string) string {
	h := strings.Trim(wordHostname.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(h) > 63 {
		h = strings.Trim(h[:63], "-")
	}
	if h != "" {
		return h
	}
	flat := wordAlnum.ReplaceAllString(strings.ToLower(mac), "")
	if len(flat) >= 6 {
		return "pc-" + flat[len(flat)-6:]
	}
	return "pc"
}

// renderAnswer puts the PC's facts in place of the words of a file: {{hostname}}, {{name}}, {{mac}} (aa:bb:...), {{mac-dash}}, {{ip}} and {{serial}}. Other text is left as it is.
func renderAnswer(text string, vars map[string]string) string {
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// xmlKinds are the kinds of answer file that are XML: the words put into them are escaped.
var xmlKinds = map[string]bool{"autoyast": true, "unattend": true}

// renderFile is the text of the file for the PC: its words replaced, and the files that cloud-init asks for besides user-data made up.
func (s *Server) renderFile(a config.AnswerFile, name, mac, host string) string {
	var serial string
	s.clientsMu.Lock()
	if c := s.clients[mac]; c != nil {
		serial = wordSerial.ReplaceAllString(c.Serial, "")
	}
	s.clientsMu.Unlock()
	pcName := s.PCName(mac)
	host2 := hostnameFor(pcName, mac)
	vars := map[string]string{"hostname": host2, "computername": computerNameFor(host2), "name": pcName, "mac": mac, "mac-dash": strings.ReplaceAll(mac, ":", "-"), "ip": host, "serial": serial}
	if xmlKinds[a.Kind] {
		for k, v := range vars {
			var b strings.Builder
			_ = xml.EscapeText(&b, []byte(v))
			vars[k] = b.String()
		}
	}
	body := renderAnswer(a.Content, vars)
	switch {
	case a.Kind == "autoinstall" && name == "meta-data":
		body = "instance-id: " + vars["mac-dash"] + "\nlocal-hostname: " + vars["hostname"] + "\n"
	case a.Kind == "autoinstall" && name == "vendor-data":
		body = "#cloud-config\n{}\n"
	}
	return body
}

// computerNameFor is the host name made into a Windows computer name: at most 15 characters, not ending in a hyphen, and never only digits.
func computerNameFor(host string) string {
	n := host
	if len(n) > 15 {
		n = n[:15]
	}
	n = strings.Trim(n, "-")
	if n == "" || strings.Trim(n, "0123456789") == "" {
		n = "PC-" + n
		if len(n) > 15 {
			n = n[:15]
		}
	}
	return n
}

// answers serves /answers/<id>/<mac>/<name>.
func (s *Server) answers(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, answerPath), "/")
	if len(parts) != 3 || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	id, macPart, name := parts[0], parts[1], parts[2]
	cfg := s.settings()
	a, ok := cfg.Answers[id]
	mac, macOK := config.NormalizeMAC(macPart)
	if !macOK && macPart != "unknown" {
		ok = false
	}
	kind, _ := config.AnswerKindOf(a.Kind)
	allowed := false
	for _, f := range kind.Files {
		allowed = allowed || f == name
	}
	if !ok || !allowed || !answerInUse(cfg, id) {
		http.NotFound(w, r)
		return
	}
	host := access.Host(r.RemoteAddr)
	allowed, note := s.takeAnswer(cfg, mac, host, time.Now(), r.Method == http.MethodGet)
	if !allowed {
		s.logf("answer file %q (%s) refused to %s: %s", a.Name, name, s.who(Client{MAC: mac, IP: host}), note) // (the requester is told nothing more than "not found")
		http.NotFound(w, r)
		return
	}
	body := s.renderFile(a, name, mac, host)
	if note != "" {
		note = " (" + note + ")"
	}
	s.logf("answer file %q (%s, %s) served to %s%s", a.Name, kind.Label, name, s.who(Client{MAC: mac, IP: host}), note) // (never its text)
	if mac != "" {
		s.setAction(mac, "unattended install: "+a.Name)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, body)
}

// WindowsAnswer is the autounattend.xml that Windows' set-up environment reads from the share for the PC with this MAC address when it starts the Windows ISO isoID: the file of
// the PC's own choice, or the one attached to the ISO, when it is a Windows answer file and the PC may have it (the same rules as for the web address, see answergate.go; the share
// cannot tell which address the PC reads from, so the address is not asked for). With commit false it only says whether there is a file (the set-up script asks before it copies);
// with commit true it counts the file against the PC's twelve and notes in its history that it was given one.
func (s *Server) WindowsAnswer(isoID, macText, name string, commit bool) ([]byte, bool) {
	if name != "autounattend.xml" {
		return nil, false
	}
	mac, ok := config.NormalizeMAC(macText)
	if !ok {
		return nil, false
	}
	cfg := s.settings()
	_, a, has := cfg.AnswerFor(isoID, mac)
	if !has || a.Kind != "unattend" {
		// no Windows answer file: the small one of the "local account" setting, unless this PC was told to have none
		if !cfg.SetupChecks.LocalAccount || cfg.PCOptions[mac].Answer == config.NoAnswer {
			return nil, false
		}
		a = config.AnswerFile{Name: "local account at the first start", Kind: "unattend", Content: localAccountXML}
	}
	allowed, note := s.takeAnswer(cfg, mac, "", time.Now(), commit)
	if !allowed {
		s.logf("answer file %q (autounattend.xml) refused to %s through the share: %s", a.Name, s.who(Client{MAC: mac}), note)
		return nil, false
	}
	if commit {
		s.logf("answer file %q (Windows autounattend.xml) given to %s through the share", a.Name, s.who(Client{MAC: mac}))
		s.setAction(mac, "unattended install: "+a.Name)
	}
	return []byte(s.renderFile(a, name, mac, "")), true
}

// answerInUse says whether the file is attached to an ISO or to a PC: a file that is not is not given to anyone.
func answerInUse(cfg config.Config, id string) bool {
	for _, a := range cfg.ISOAnswers {
		if a == id {
			return true
		}
	}
	for _, o := range cfg.PCOptions {
		if o.Answer == id {
			return true
		}
	}
	return false
}
