// The PCs that asked for a menu, as the web interface lists them, and a few facts about the ISOs and the server for the same pages.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"net"
	"net/http"
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
	ip := queryValue(q, "ip", 45)
	if net.ParseIP(ip) == nil {
		ip = host
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
	// a request without these (an old script, a test) must not wipe what an earlier one told
	for key, dst := range map[string]*string{"plat": &c.Platform, "man": &c.Manufacturer, "prod": &c.Product, "serial": &c.Serial} {
		if v := queryValue(q, key, 60); v != "" {
			*dst = v
		}
	}
	return *c
}

// setAction notes what the PC was last sent to.
func (s *Server) setAction(mac, action string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if c := s.clients[mac]; c != nil {
		c.LastAction = action
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
		}
	}
}

// Clients are the PCs that asked for a menu, the one seen last first.
func (s *Server) Clients() []Client {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
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
