// The boot menu: built for every PC that asks, from the ISO folder and the settings made in the web interface (names, hidden entries, what starts by
// itself) and from the rule of that PC, when it has one.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/gfx"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// entryInfo is one ISO as the menu and the web interface see it.
type entryInfo struct {
	ISO     isos.ISO
	Boot    family.Boot
	Known   bool // the ISO is started from its own files; otherwise it is handed to the PC as a disk
	Title   string
	Hidden  bool
	Entry   menu.Entry
	Display string // the title and what kind of start it gets, as the menu shows it
}

// settings are the current settings: from the settings file when there is one, otherwise from the fields the program was started with.
func (s *Server) settings() config.Config {
	var c config.Config
	if s.Config != nil {
		c = s.Config.Get()
	} else {
		c = config.Config{MenuTimeout: s.Menu.Timeout, Hidden: map[string]bool{}, Titles: map[string]string{}}
		switch {
		case s.Pick != "":
			c.Default = "pick:" + s.Pick
		case s.Menu.Default < 0:
			c.Default = "local"
		case s.Menu.Default > 0:
			c.Default = "index:" + strconv.Itoa(s.Menu.Default)
		}
	}
	return c
}

func (s *Server) entryInfos(cfg config.Config) ([]entryInfo, error) {
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return nil, err
	}
	out := make([]entryInfo, 0, len(list))
	for _, iso := range list {
		e := entryInfo{ISO: iso, Title: iso.Title, Hidden: cfg.Hidden[iso.ID]}
		if t := cfg.Titles[iso.ID]; t != "" {
			e.Title = t
		}
		size := fmt.Sprintf("%.1f GB", float64(iso.Size)/(1<<30))
		if b, ok := s.recognise(iso); ok {
			e.Boot, e.Known = b, true
			e.Display = fmt.Sprintf("%s  [%s, %s]", e.Title, b.Family, size)
			e.Entry = menu.Entry{Name: e.Display, Commands: b.Commands}
		} else {
			e.Display = fmt.Sprintf("%s  [as a disk, %s]", e.Title, size)
			e.Entry = menu.Entry{Name: e.Display, Commands: []string{
				"echo Loading " + oneWord(e.Title) + " ... the whole ISO is read into memory first, this takes a while",
				"sanboot --no-describe --drive 0x80 " + s.Base + isoPath(iso.Name),
			}}
		}
		out = append(out, e)
	}
	return out, nil
}

// Entries are the menu lines: one per ISO that is not hidden. An ISO of a known kind is started from its own files; any other is handed to the PC
// as a disk.
func (s *Server) Entries() ([]menu.Entry, error) {
	infos, err := s.entryInfos(s.settings())
	if err != nil {
		return nil, err
	}
	var entries []menu.Entry
	for _, e := range infos {
		if !e.Hidden {
			entries = append(entries, e.Entry)
		}
	}
	return entries, nil
}

// defaultChoice turns the "what starts by itself" setting into the number the menu wants: the entry (counting from 1), -1 for the PC's own disk, 0 for
// nothing (the menu waits).
func defaultChoice(def string, visible []entryInfo) int {
	switch {
	case def == "":
		return 0
	case def == "local":
		return -1
	case strings.HasPrefix(def, "iso:"):
		for i, e := range visible {
			if e.ISO.ID == strings.TrimPrefix(def, "iso:") {
				return i + 1
			}
		}
	case strings.HasPrefix(def, "index:"):
		if n, err := strconv.Atoi(strings.TrimPrefix(def, "index:")); err == nil && n >= 1 && n <= len(visible) {
			return n
		}
	case strings.HasPrefix(def, "pick:"):
		want := strings.ToLower(strings.TrimPrefix(def, "pick:"))
		for i, e := range visible {
			if strings.Contains(strings.ToLower(e.Entry.Name), want) {
				return i + 1
			}
		}
	}
	return 0
}

// bootScript is what DHCP points every PC at. It only tells the PC to ask for its menu again with its identity added (iPXE fills in the ${...}), so
// that the menu can be made for that PC and the web interface can list it.
func (s *Server) bootScript(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("mac") != "" { // an older script that already carries the identity
		s.menu(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "#!ipxe\nchain %s/menu?mac=${mac}&ip=${ip}&plat=${platform}&man=${manufacturer:uristring}&prod=${product:uristring}&serial=${serial:uristring} || shell\n", s.Base)
}

// menu is the menu of one PC.
func (s *Server) menu(w http.ResponseWriter, r *http.Request) {
	cfg := s.settings()
	infos, err := s.entryInfos(cfg)
	if err != nil {
		http.Error(w, "cannot read the ISO folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	client := s.noteClient(r)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	for _, rule := range cfg.Rules {
		if rule.MAC != client.MAC || rule.Action == "menu" {
			continue
		}
		why := "Rule for this PC"
		if rule.Note != "" {
			why += " (" + rule.Note + ")"
		}
		if rule.Action == "local" {
			s.logf("%s (%s): its rule sends it to its own disk", client.MAC, client.IP)
			s.setAction(client.MAC, "rule: own disk")
			fmt.Fprint(w, menu.Local(why+": starting from this PC's own disk."))
			return
		}
		for _, e := range infos {
			if "iso:"+e.ISO.ID == rule.Action {
				s.logf("%s (%s): its rule starts %s", client.MAC, client.IP, e.Title)
				s.setAction(client.MAC, "rule: "+e.Title)
				fmt.Fprint(w, menu.Direct(why+": starting "+e.Title+".", e.Entry))
				return
			}
		}
		s.logf("%s: its rule names an ISO that is not there any more: showing the menu", client.MAC)
	}

	var visible []entryInfo
	var entries []menu.Entry
	for _, e := range infos {
		if !e.Hidden {
			visible = append(visible, e)
			entries = append(entries, e.Entry)
		}
	}
	opts := menu.Options{Default: defaultChoice(cfg.Default, visible), Timeout: cfg.MenuTimeout,
		Picture: s.Base + "/bg.png", Width: gfx.Width, Height: gfx.Height, Top: gfx.LogoBottom + 16}
	s.logf("menu for %s (%s): %d ISO file(s)", client.IP, client.MAC, len(entries))
	fmt.Fprint(w, menu.ScriptWith("Gola Bootlace", entries, opts))
}

// queryValue is a value iPXE put in the web address, without anything that could break a line of the log.
func queryValue(q url.Values, key string, max int) string {
	v := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(q.Get(key)))
	if len(v) > max {
		v = v[:max]
	}
	return v
}
