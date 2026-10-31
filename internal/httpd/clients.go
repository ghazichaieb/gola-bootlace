// The PCs that asked for a menu, as the web interface lists them, and a few facts about the ISOs and the server for the same pages.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// Client is one PC that asked for a menu. iPXE sends what it knows about itself with the request.
type Client struct {
	MAC          string         `json:"mac"`
	IP           string         `json:"ip"`
	Platform     string         `json:"platform"` // "efi" or "pcbios"
	Manufacturer string         `json:"manufacturer"`
	Product      string         `json:"product"`
	Serial       string         `json:"serial"`
	Hostname     string         `json:"hostname,omitempty"` // the host name the PC gave in its DHCP request (option 12), if it gave one: not trusted, only offered as a name
	Asset        string         `json:"asset,omitempty"`    // the asset tag of its SMBIOS, if it has one
	FirstSeen    time.Time      `json:"firstSeen"`
	LastSeen     time.Time      `json:"lastSeen"`
	Count        int            `json:"count"` // how many times it asked for a menu
	LastAction   string         `json:"lastAction"`
	History      []HistoryEvent `json:"history,omitempty"` // the last things that happened to this PC, oldest first
	// What lets this PC's installer fetch its answer file (see answergate.go): the address it last asked for the menu from, when, and how many files it may still be given.
	MenuIP      string    `json:"menuIp,omitempty"`
	MenuAt      time.Time `json:"menuAt,omitempty"`
	AnswersLeft int       `json:"answersLeft,omitempty"`
	// ShimRefused: the PC fetched the whole Secure Boot shim, did not ask for iPXE within shimGrace and has not reached the menu since (issue #79).
	ShimRefused bool `json:"shimRefused,omitempty"`
	// ShimFallback is what the PC's next network boot gets because of that (see shimfallback.go); only set while ShimRefused is, and not when a rule decides.
	ShimFallback *ShimFallback `json:"shimFallback,omitempty"`
	shimDone     time.Time     // when the shim's transfer to this PC ended, until it asks for iPXE or the menu
}

const maxClients = 200

// HistoryEvent is one thing that happened to a PC: it was shown the menu, a rule sent it somewhere, it started fetching an ISO.
type HistoryEvent struct {
	Time time.Time `json:"time"`
	What string    `json:"what"`
}

const (
	maxHistory      = 40              // events kept per PC
	historyDedupFor = 2 * time.Minute // the same thing again within this long is one event (an ISO is fetched in many requests)
)

// note adds an event to the PC's history (the caller holds clientsMu).
func (c *Client) note(what string, now time.Time) {
	what = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, what)
	if n := len(c.History); n > 0 && c.History[n-1].What == what && now.Sub(c.History[n-1].Time) < historyDedupFor {
		return
	}
	c.History = append(c.History, HistoryEvent{Time: now, What: what})
	if len(c.History) > maxHistory {
		c.History = append([]HistoryEvent(nil), c.History[len(c.History)-maxHistory:]...)
	}
}

// admitLocked returns the record of a PC, adding it to the list when it is new (the caller holds clientsMu). When the list is full, a new PC replaces the least recently seen
// PC that no administrator has configured. Only administrative settings pin a PC: a firmware-provided host name or asset tag is untrusted and must not keep a record. When every
// slot is pinned the PC still gets a record to serve its request, but that record is not put in the list, so the list neither grows nor loses a configured PC's history.
// Every path that adds a PC (the menu, the DHCP listener) goes through here, so the bound holds for all of them.
func (s *Server) admitLocked(mac string, now time.Time) *Client {
	if s.clients == nil {
		s.clients = map[string]*Client{}
	}
	if c := s.clients[mac]; c != nil {
		return c
	}
	if len(s.clients) >= maxClients {
		pinned := s.pinnedPCs()
		var oldest string
		for k, v := range s.clients {
			if !pinned[k] && (oldest == "" || v.LastSeen.Before(s.clients[oldest].LastSeen)) {
				oldest = k
			}
		}
		if oldest != "" {
			delete(s.clients, oldest)
		}
	}
	c := &Client{MAC: mac, FirstSeen: now}
	if len(s.clients) < maxClients {
		s.clients[mac] = c
	}
	return c
}

// pinnedPCs is the set of MAC addresses an administrator has configured: a name, a rule, or kernel/debug/answer options.
func (s *Server) pinnedPCs() map[string]bool {
	cfg := s.settings()
	pinned := map[string]bool{}
	for mac, name := range cfg.PCNames {
		if name != "" {
			pinned[mac] = true
		}
	}
	for _, rule := range cfg.Rules {
		pinned[rule.MAC] = true
	}
	for mac, options := range cfg.PCOptions {
		if !options.Empty() {
			pinned[mac] = true
		}
	}
	return pinned
}

// noteClient records the PC behind a menu request and returns what is known of it.
func (s *Server) noteClient(r *http.Request) Client {
	q := r.URL.Query()
	host := access.Host(r.RemoteAddr)
	ip := host // where the request really came from; what iPXE says about itself is only used when that is not an address
	if net.ParseIP(ip) == nil {
		ip = queryValue(q, "ip", 45)
	}
	mac, _ := config.NormalizeMAC(q.Get("mac"))
	if mac == "" {
		mac = "unknown-" + host // an old script without its identity: still listed, by address
	}
	now := s.now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if s.clients == nil {
		s.clients = map[string]*Client{}
	}
	c := s.admitLocked(mac, now)
	if warn := s.openAnswerGrant(c, ip, now); warn != "" {
		s.logf("%s", warn)
	}
	c.IP, c.LastSeen = ip, now
	c.shimDone = time.Time{} // it reached the menu, so iPXE did start
	c.Count++
	c.note("asked for the menu", now)
	s.changed()
	// a request without these (an old script, a test) must not wipe what an earlier one told
	for key, dst := range map[string]*string{"plat": &c.Platform, "man": &c.Manufacturer, "prod": &c.Product, "serial": &c.Serial, "host": &c.Hostname, "asset": &c.Asset} {
		if v := queryValue(q, key, 60); v != "" {
			*dst = v
		}
	}
	return *c
}

// PCName is the name a person gave the PC with this MAC address, or "".
func (s *Server) PCName(mac string) string {
	if s.Config == nil {
		return ""
	}
	return s.Config.Get().PCNames[mac]
}

// who is how a PC is named in the log: its name first when it has one.
func (s *Server) who(c Client) string {
	if name := s.PCName(c.MAC); name != "" {
		return fmt.Sprintf("%s (%s, %s)", name, c.MAC, c.IP)
	}
	return fmt.Sprintf("%s (%s)", c.MAC, c.IP)
}

// setAction notes what the PC was last sent to.
func (s *Server) setAction(mac, action string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if c := s.clients[mac]; c != nil {
		c.LastAction = action
		c.note(action, time.Now())
		s.changed()
	}
}

// noteFetch notes that the PC at this address started fetching files of an ISO.
func (s *Server) noteFetch(remoteAddr, title string) {
	host := access.Host(remoteAddr)
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for _, c := range s.clients {
		if c.IP == host {
			s.keepAnswerGrant(c, host, time.Now())
			c.LastAction = "booting " + title
			c.LastSeen = time.Now()
			c.note(c.LastAction, c.LastSeen)
			s.changed()
		}
	}
}

// Clients are the PCs that asked for a menu, the one seen last first.
func (s *Server) Clients() []Client {
	out := s.clientsSnapshot()
	s.annotateShimFallback(out) // (outside the lock: it looks at the library)
	return out
}

// clientsSnapshot copies the list of PCs, the one seen last first, with the flag of those that refuse the shim.
func (s *Server) clientsSnapshot() []Client {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if days := s.settings().PCDays; days > 0 {
		s.forgetBefore(time.Now().AddDate(0, 0, -days))
	}
	out := make([]Client, 0, len(s.clients))
	now := s.now()
	for _, c := range s.clients {
		copied := *c
		copied.ShimRefused = c.refusesShim(now)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// ISOInfo is one ISO for the web interface.
type ISOInfo struct {
	EntrySettings config.ISOEntrySettings `json:"entrySettings"`
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`  // path under the ISO folder
	Title         string                  `json:"title"` // the name shown in the menu (the file name unless it was renamed)
	Original      string                  `json:"original"`
	Size          int64                   `json:"size"`
	Modified      time.Time               `json:"modified"`
	Hidden        bool                    `json:"hidden"`
	ByRule        bool                    `json:"hiddenByRule,omitempty"` // hidden by a pattern (Menu page), so its own switch does not apply
	Method        string                  `json:"method"`                 // "recognised" or "disk"
	Family        string                  `json:"family,omitempty"`       // for example "Windows setup" or "Ubuntu (casper)"
	Args          string                  `json:"args,omitempty"`         // extra kernel arguments for this ISO
	Protected     bool                    `json:"protected"`              // the menu asks for a password before it starts it
	Debug         config.DebugMode        `json:"debug"`                  // how it is started for finding faults
	Commands      []string                `json:"commands,omitempty"`     // what the menu gives a PC that chooses it (the iPXE commands), so that the final command line can be read
	Ways          []WayInfo               `json:"ways,omitempty"`         // the other entries of its own boot menu that can start it (issue #35)
	Answer        string                  `json:"answer,omitempty"`       // the id of the answer file every PC that starts it gets (an unattended install, issue #17)
}

// WayInfo is one of those entries as the web page lists it.
type WayInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Special bool   `json:"special,omitempty"` // safe graphics, a rescue entry, a check of the disc ...
}

// ISOInfos lists the ISOs of the folder with what is known of each.
func (s *Server) ISOInfos() ([]ISOInfo, error) {
	cfg := s.settings()
	infos, err := s.entryInfos(cfg)
	if err != nil {
		return nil, err
	}
	out := make([]ISOInfo, 0, len(infos))
	for _, e := range infos {
		i := ISOInfo{ID: e.ISO.ID, Name: e.ISO.Name, Title: e.Title, Original: e.ISO.Title, Size: e.ISO.Size, Modified: e.ISO.ModTime, Hidden: e.Hidden, ByRule: e.ByRule, Method: "disk"}
		if e.Known {
			i.Method, i.Family = "recognised", e.Boot.Family
		}
		i.Args = cfg.Args[e.ISO.ID]
		i.Debug = cfg.Debug[e.ISO.ID]
		i.Protected = e.Protected
		i.Answer = cfg.ISOAnswers[e.ISO.ID]
		i.Commands = e.Entry.Commands
		i.EntrySettings = cfg.ISOEntries[e.ISO.ID]
		for _, w := range e.Ways {
			i.Ways = append(i.Ways, WayInfo{ID: w.ID, Title: w.Title, Special: w.Special})
		}
		out = append(out, i)
	}
	return out, nil
}

// Rescan refreshes ISO recognition after routine changes, retaining stamped initramfs facts.
func (s *Server) Rescan() {
	s.findingsCacheMu.Lock()
	s.findingsEpoch++
	s.findingsCacheMu.Unlock()
	s.mu.Lock()
	s.known = nil
	s.waysKnown = nil
	s.looking = nil
	s.mu.Unlock()
}

// Started is when the server started.
func (s *Server) StartedAt() time.Time { return s.started }

// ReplaceClients replaces the list of PCs (a restored backup).
func (s *Server) ReplaceClients(list []Client) {
	s.clientsMu.Lock()
	s.clients = map[string]*Client{}
	for i := range list {
		if mac, ok := config.NormalizeMAC(list[i].MAC); ok && len(s.clients) < maxClients {
			list[i].MAC = mac
			s.clients[mac] = &list[i]
		}
	}
	s.changed()
	s.clientsMu.Unlock()
}

// ForgetPC drops one PC from the list; it is listed again when it asks for a menu.
func (s *Server) ForgetPC(mac string) bool {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	_, ok := s.clients[mac]
	delete(s.clients, mac)
	if ok {
		s.changed()
	}
	return ok
}

// ForgetPCsNotSeenFor drops the PCs that did not ask for a menu for this long and says how many there were.
func (s *Server) ForgetPCsNotSeenFor(d time.Duration) int {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	return s.forgetBefore(time.Now().Add(-d))
}

// forgetBefore is for callers that hold clientsMu.
func (s *Server) forgetBefore(cutoff time.Time) int {
	n := 0
	for mac, c := range s.clients {
		if c.LastSeen.Before(cutoff) {
			delete(s.clients, mac)
			n++
		}
	}
	if n > 0 {
		s.changed()
	}
	return n
}

// The list of PCs is kept in a file next to the settings, so that it survives a restart (the date of last sight is what the clean-up goes by).

// changed schedules a write of the file; many changes close together make one write.
func (s *Server) changed() {
	if s.PCFile == "" {
		return
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.savePending {
		return
	}
	s.savePending = true
	time.AfterFunc(2*time.Second, s.saveClients)
}

func (s *Server) saveClients() {
	s.saveMu.Lock()
	s.savePending = false
	s.saveMu.Unlock()
	s.clientsMu.Lock()
	list := make([]Client, 0, len(s.clients))
	for _, c := range s.clients {
		list = append(list, *c)
	}
	s.clientsMu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].MAC < list[j].MAC })
	data, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		err = writeFileAtomic(s.PCFile, append(data, '\n'))
	}
	if err != nil && s.Logf != nil {
		s.Logf("cannot save the list of PCs to %s: %v", s.PCFile, err)
	}
}

// LoadClients reads the file at start; a missing file is an empty list.
func (s *Server) LoadClients() error {
	if s.PCFile == "" {
		return nil
	}
	data, err := os.ReadFile(s.PCFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []Client
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients = map[string]*Client{}
	for i := range list {
		if list[i].MAC != "" {
			s.clients[list[i].MAC] = &list[i]
		}
	}
	return nil
}

// FlushClients writes the list now (used when the server stops).
func (s *Server) FlushClients() {
	if s.PCFile != "" {
		s.saveClients()
	}
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bootlace-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
