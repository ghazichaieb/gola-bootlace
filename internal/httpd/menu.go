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
	ISO       isos.ISO
	Boot      family.Boot
	Known     bool // the ISO is started from its own files; otherwise it is handed to the PC as a disk
	Title     string
	Hidden    bool
	Protected bool // the menu asks for a password before it starts this ISO
	ByRule    bool // hidden by a pattern of the settings, not by the ISO's own switch
	Entry     menu.Entry
	Display   string // the title and what kind of start it gets, as the menu shows it
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

// addArgs puts extra kernel arguments (the person's, for this ISO) at the end of the kernel command of an ISO's start.
func addArgs(commands []string, extra string) []string {
	if extra == "" {
		return commands
	}
	out := append([]string(nil), commands...)
	for i, c := range out {
		if strings.HasPrefix(c, "kernel ") {
			out[i] = c + " " + extra
			break
		}
	}
	return out
}

// debugArgs changes the kernel command of an ISO's start for finding faults: the quiet start-up words go, the kernel's messages are turned up and, if asked, the console
// goes to the first serial port as well (the last console= is the one the kernel treats as its main console).
func debugArgs(commands []string, d config.DebugMode) []string {
	if !d.Verbose && !d.Serial {
		return commands
	}
	out := append([]string(nil), commands...)
	for i, c := range out {
		if !strings.HasPrefix(c, "kernel ") {
			continue
		}
		var words []string
		have := map[string]bool{}
		for _, w := range strings.Fields(c) {
			if d.Verbose && (w == "quiet" || w == "splash" || w == "rhgb") {
				continue
			}
			if d.Serial && strings.HasPrefix(w, "console=") {
				continue // replaced below, so that the serial port is the last one
			}
			have[w] = true
			words = append(words, w)
		}
		if d.Verbose {
			for _, w := range []string{"loglevel=7", "ignore_loglevel", "debug"} {
				if !have[w] {
					words = append(words, w)
				}
			}
		}
		if d.Serial {
			words = append(words, "console=tty0", "console=ttyS0,115200n8")
		}
		out[i] = strings.Join(words, " ")
		break
	}
	return out
}

func (s *Server) entryInfos(cfg config.Config) ([]entryInfo, error) {
	list, err := s.scan()
	if err != nil {
		return nil, err
	}
	out := make([]entryInfo, 0, len(list))
	for _, iso := range list {
		byRule := config.MatchesHide(cfg.HidePatterns, iso.Name)
		e := entryInfo{ISO: iso, Title: iso.Title, Hidden: cfg.Hidden[iso.ID] || byRule, ByRule: byRule}
		if t := cfg.Titles[iso.ID]; t != "" {
			e.Title = t
		}
		size := fmt.Sprintf("%.1f GB", float64(iso.Size)/(1<<30))
		if iso.Packed != "" { // packed, and not unpacked yet
			e.Display, e.Entry, e.Known, e.Boot = s.diskEntry(iso, e.Title, size)
			out = append(out, e)
			continue
		}
		if b, ok := s.recognise(iso); ok { // started from the files inside (an ISO's, or a disk image's partition)
			where := b.Family
			if iso.Disk {
				where += ", from a disk image"
			}
			e.Boot, e.Known = b, true
			e.Display = fmt.Sprintf("%s  [%s, %s]", e.Title, where, size)
			e.Entry = menu.Entry{Name: e.Display, Commands: debugArgs(addArgs(b.Commands, cfg.Args[iso.ID]), cfg.Debug[iso.ID]), Title: e.Title, Kind: b.Family, Size: size}
		} else if iso.Disk {
			e.Display, e.Entry, e.Known, e.Boot = s.diskEntry(iso, e.Title, size)
		} else {
			e.Display = fmt.Sprintf("%s  [as a disk, %s]", e.Title, size)
			e.Entry = menu.Entry{Name: e.Display, Title: e.Title, Kind: "as a disk", Size: size, Commands: []string{
				s.loadingNote(iso, e.Title),
				"sanboot --no-describe --drive 0x80 " + s.sanbootSource(iso),
			}}
		}
		out = append(out, e)
	}
	for i := range out { // an ISO with a password: the menu asks for it first (a rule that starts it does not: Direct leaves the guard out)
		if cfg.BootPasswords[out[i].ISO.ID] != "" {
			out[i].Protected = true
			out[i].Entry.Guard = s.guardLines(out[i].ISO.ID)
		}
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
	fmt.Fprintf(w, "#!ipxe\nchain %s || shell\n", s.menuURL(0))
}

// menuURL is the address of a menu as iPXE asks for it: its identity filled in by iPXE itself, and a page when there is more than one.
func (s *Server) menuURL(page int) string {
	u := s.Base + "/menu?mac=${mac}&ip=${ip}&plat=${platform}&man=${manufacturer:uristring}&prod=${product:uristring}&serial=${serial:uristring}&host=${hostname:uristring}&asset=${asset:uristring}"
	if page > 0 {
		u += "&page=" + strconv.Itoa(page)
	}
	return u
}

// entriesPerPage is how many ISOs one page of the menu holds. The screen's size is known here: a UEFI PC shows the picture (a text area of 24 lines under
// the logo) and a BIOS PC has the 25 lines of its text mode; the heading, the lace mark, the buttons to other pages and the three fixed lines take the rest.
func entriesPerPage(platform string) int {
	switch platform {
	case "efi":
		return 12
	case "pcbios":
		return 8 // the logo takes three lines
	}
	return 10
}

// menu is the menu of one PC.
func (s *Server) menu(w http.ResponseWriter, r *http.Request) {
	cfg := s.settings()
	infos, err := s.entryInfos(cfg)
	if err != nil {
		http.Error(w, "cannot read the ISO folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	paged := page > 0 // a later page of a menu already shown: not a new start, and the PC's rule was applied at the first
	var client Client
	if paged {
		client = Client{MAC: "page", IP: r.RemoteAddr, Platform: queryValue(r.URL.Query(), "plat", 20)}
	} else {
		client = s.noteClient(r)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	type applied struct{ Action, Note, Who string }
	var rules []applied // the rules that apply to this PC, in the order they are tried: its own first, then its groups
	if !paged {
		for _, rule := range cfg.Rules {
			if rule.MAC == client.MAC {
				rules = append(rules, applied{rule.Action, rule.Note, "Rule for this PC"})
			}
		}
		for _, g := range cfg.GroupRules {
			if g.Matches(client.Manufacturer, client.Product, client.Platform) {
				rules = append(rules, applied{g.Action, g.Note, "Rule for " + g.Describe()})
			}
		}
	}
	for _, rule := range rules {
		if rule.Action == "menu" {
			break // the menu is what this PC (or its group) is to get: no later rule decides
		}
		why := rule.Who
		if rule.Note != "" {
			why += " (" + rule.Note + ")"
		}
		if rule.Action == "local" {
			s.logf("%s: its rule sends it to its own disk", s.who(client))
			s.setAction(client.MAC, "rule: own disk")
			fmt.Fprint(w, menu.Local(why+": starting from this PC's own disk."))
			return
		}
		for _, e := range infos {
			if "iso:"+e.ISO.ID == rule.Action || "native:"+e.ISO.ID == rule.Action { // (a native rule that reaches iPXE: Secure Boot is off, the usual way does)
				s.logf("%s: its rule starts %s", s.who(client), e.Title)
				s.setAction(client.MAC, "rule: "+e.Title)
				fmt.Fprint(w, menu.Direct(why+": starting "+e.Title+".", e.Entry, s.menuURL(1)))
				return
			}
		}
		s.logf("%s: its rule names an ISO that is not there any more: showing the menu", s.who(client))
	}

	var visible []entryInfo
	var entries []menu.Entry
	for _, e := range infos {
		if !e.Hidden {
			visible = append(visible, e)
			entries = append(entries, e.Entry)
		}
	}
	opts := menu.Options{Default: defaultChoice(cfg.Default, visible), Timeout: cfg.MenuTimeout, Again: s.menuURL(1), Accent: cfg.MenuAccent}
	if client.Platform == "efi" {
		opts.Picture, opts.Width, opts.Height, opts.Top = s.Base+"/bg.png", gfx.Width, gfx.Height, gfx.LogoBottom+16
	} else if client.Platform == "pcbios" {
		opts.Ascii = true // a BIOS PC gets the ASCII frame and table
	}
	perPage := entriesPerPage(client.Platform)
	opts.Pages = (len(entries) + perPage - 1) / perPage
	if opts.Pages < 1 {
		opts.Pages = 1
	}
	if page == 0 && opts.Default >= 1 { // the page of the entry that starts by itself is the one to show first
		page = (opts.Default-1)/perPage + 1
	}
	if page < 1 {
		page = 1
	}
	if page > opts.Pages {
		page = opts.Pages
	}
	opts.Page = page
	first := (page - 1) * perPage
	last := first + perPage
	if last > len(entries) {
		last = len(entries)
	}
	if opts.Default >= 1 { // the default is counted in the whole list; the page counts its own
		if opts.Default > first && opts.Default <= last {
			opts.Default -= first
		} else {
			opts.Default = 0
		}
	}
	if paged {
		opts.Timeout = 0 // the PC is being looked through, not left alone
	}
	if page > 1 {
		opts.Prev = s.menuURL(page - 1)
	}
	if page < opts.Pages {
		opts.Next = s.menuURL(page + 1)
	}
	if paged { // reached with the buttons: the highlight stays on the one that goes on (the last page: the one that goes back)
		opts.Focus = "next"
	}
	if !paged {
		s.logf("menu for %s: %d ISO file(s)", s.who(client), len(entries))
	}
	fmt.Fprint(w, menu.ScriptWith("Gola Bootlace", entries[first:last], opts))
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

// sanbootSource is where sanboot reads an ISO from: the iSCSI target (a block at a time) when there is one, otherwise the whole file over HTTP (which iPXE keeps in the
// PC's memory).
func (s *Server) sanbootSource(iso isos.ISO) string {
	if u := s.iscsiURL(iso); u != "" {
		return u
	}
	return s.Base + isoPath(iso.Name)
}
