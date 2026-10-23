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
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// Client is one PC that asked for a menu. iPXE sends what it knows about itself with the request.
type Client struct {
	MAC          string    `json:"mac"`
	IP           string    `json:"ip"`
	Platform     string    `json:"platform"` // "efi" or "pcbios"
	Manufacturer string    `json:"manufacturer"`
	Product      string    `json:"product"`
	Serial       string    `json:"serial"`
	Hostname     string    `json:"hostname,omitempty"` // the host name the PC gave in its DHCP request (option 12), if it gave one: not trusted, only offered as a name
	Asset        string    `json:"asset,omitempty"`    // the asset tag of its SMBIOS, if it has one
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	Count        int       `json:"count"` // how many times it asked for a menu
	LastAction   string    `json:"lastAction"`
}

const maxClients = 200

// noteClient records the PC behind a menu request and returns what is known of it.
func (s *Server) noteClient(r *http.Request) Client {
	q := r.URL.Query()
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := host // where the request really came from; what iPXE says about itself is only used when that is not an address
	if net.ParseIP(ip) == nil {
		ip = queryValue(q, "ip", 45)
	}
	mac, _ := config.NormalizeMAC(q.Get("mac"))
	if mac == "" {
		mac = "unknown-" + host // an old script without its identity: still listed, by address
	}
	now := time.Now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if s.clients == nil {
		s.clients = map[string]*Client{}
	}
	c := s.clients[mac]
	if c == nil {
		if len(s.clients) >= maxClients {
			var oldest string
			for k, v := range s.clients {
				if oldest == "" || v.LastSeen.Before(s.clients[oldest].LastSeen) {
					oldest = k
				}
			}
			delete(s.clients, oldest)
		}
		c = &Client{MAC: mac, FirstSeen: now}
		s.clients[mac] = c
	}
	c.IP, c.LastSeen = ip, now
	c.Count++
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
		s.changed()
	}
}

// noteFetch notes that the PC at this address started fetching files of an ISO.
func (s *Server) noteFetch(remoteAddr, title string) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return
	}
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for _, c := range s.clients {
		if c.IP == host {
			c.LastAction = "booting " + title
			c.LastSeen = time.Now()
			s.changed()
		}
	}
}

// Clients are the PCs that asked for a menu, the one seen last first.
func (s *Server) Clients() []Client {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if days := s.settings().PCDays; days > 0 {
		s.forgetBefore(time.Now().AddDate(0, 0, -days))
	}
	out := make([]Client, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// ISOInfo is one ISO for the web interface.
type ISOInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`  // path under the ISO folder
	Title    string    `json:"title"` // the name shown in the menu (the file name unless it was renamed)
	Original string    `json:"original"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Hidden   bool      `json:"hidden"`
	Method   string    `json:"method"`           // "recognised" or "disk"
	Family   string    `json:"family,omitempty"` // for example "Windows setup" or "Ubuntu (casper)"
	Args     string    `json:"args,omitempty"`   // extra kernel arguments for this ISO
}

// ISOInfos lists the ISOs of the folder with what is known of each.
func (s *Server) ISOInfos() ([]ISOInfo, error) {
	infos, err := s.entryInfos(s.settings())
	if err != nil {
		return nil, err
	}
	out := make([]ISOInfo, 0, len(infos))
	for _, e := range infos {
		i := ISOInfo{ID: e.ISO.ID, Name: e.ISO.Name, Title: e.Title, Original: e.ISO.Title, Size: e.ISO.Size, Modified: e.ISO.ModTime, Hidden: e.Hidden, Method: "disk"}
		if e.Known {
			i.Method, i.Family = "recognised", e.Boot.Family
		}
		i.Args = s.settings().Args[e.ISO.ID]
		out = append(out, i)
	}
	return out, nil
}

// Rescan forgets what was found out about the ISOs, so that the next menu looks inside them again.
func (s *Server) Rescan() {
	s.mu.Lock()
	s.known = nil
	s.mu.Unlock()
}

// Started is when the server started.
func (s *Server) StartedAt() time.Time { return s.started }

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
