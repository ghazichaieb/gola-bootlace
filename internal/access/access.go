// Package access decides which addresses may use the server. Bootlace is for a local network: by default only addresses of the local network (private ranges, the
// loopback address and link-local addresses) are answered, on every service. More networks can be added, and "from anywhere" has to be switched on on purpose.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package access

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Policy is the rule. It is safe to use from several goroutines, and can be changed while the server runs.
type Policy struct {
	mu          sync.RWMutex
	nets        []*net.IPNet
	allowPublic bool

	refused  int
	recent   map[string]time.Time // address -> when it was last refused (kept short)
	logf     func(format string, args ...any)
	loggedAt map[string]time.Time
}

// New makes a policy that allows the local network only.
func New(logf func(format string, args ...any)) *Policy {
	return &Policy{recent: map[string]time.Time{}, loggedAt: map[string]time.Time{}, logf: logf}
}

// Parse reads a list of networks, each "a.b.c.d/nn", an IPv6 prefix or a single address. A network so wide that it is most of the internet is refused:
// "from anywhere" is its own setting.
func Parse(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("%q is not an address or a network (like 192.168.1.0/24)", s)
			}
			if v4 := ip.To4(); v4 != nil {
				s += "/32"
			} else {
				s += "/128"
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a network (like 192.168.1.0/24)", s)
		}
		ones, bits := n.Mask.Size()
		if (bits == 32 && ones < 8) || (bits == 128 && ones < 16) {
			return nil, fmt.Errorf("%s is far too wide: it is most of the internet. To answer any address, use \"from anywhere\"", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// Set replaces the extra networks and the "from anywhere" switch.
func (p *Policy) Set(networks []string, allowPublic bool) error {
	nets, err := Parse(networks)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.nets, p.allowPublic = nets, allowPublic
	p.mu.Unlock()
	return nil
}

// local says whether an address belongs to a local network: private ranges (10/8, 172.16/12, 192.168/16, fc00::/7), loopback and link-local. The unspecified
// address is what a PC that has no address yet sends from (DHCP).
func local(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// Allowed says whether requests from ip are answered.
func (p *Policy) Allowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // an IPv4 address that arrives in IPv6 form is the IPv4 address
	}
	if local(ip) {
		return true
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.allowPublic {
		return true
	}
	for _, n := range p.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// AllowedAddr is Allowed for the address of a connection or a packet; a refusal is counted and, once an hour for each address, written to the log.
func (p *Policy) AllowedAddr(a net.Addr) bool {
	var ip net.IP
	switch v := a.(type) {
	case *net.TCPAddr:
		ip = v.IP
	case *net.UDPAddr:
		ip = v.IP
	default:
		host, _, err := net.SplitHostPort(a.String())
		if err != nil {
			host = a.String()
		}
		ip = net.ParseIP(host)
	}
	if p.Allowed(ip) {
		return true
	}
	p.refuse(ip)
	return false
}

func (p *Policy) refuse(ip net.IP) {
	key := "unknown"
	if ip != nil {
		key = ip.String()
	}
	now := time.Now()
	p.mu.Lock()
	p.refused++
	p.recent[key] = now
	if len(p.recent) > 50 { // keep the list short: forget the oldest
		oldest, at := "", now
		for k, t := range p.recent {
			if t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(p.recent, oldest)
	}
	say := now.Sub(p.loggedAt[key]) > time.Hour
	if say {
		p.loggedAt[key] = now
		if len(p.loggedAt) > 500 {
			p.loggedAt = map[string]time.Time{key: now}
		}
	}
	p.mu.Unlock()
	if say && p.logf != nil {
		p.logf("refused a request from %s: not a local address (see Server > Access)", key)
	}
}

// Stats is what the web interface shows.
type Stats struct {
	Networks    []string `json:"networks"`
	AllowPublic bool     `json:"allowPublic"`
	Refused     int      `json:"refused"`
	Recent      []string `json:"recent"` // the addresses refused lately, newest first
}

// Stats reads the counters and the settings.
func (p *Policy) Stats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := Stats{Networks: []string{}, AllowPublic: p.allowPublic, Refused: p.refused, Recent: []string{}}
	for _, n := range p.nets {
		s.Networks = append(s.Networks, n.String())
	}
	type seen struct {
		ip string
		at time.Time
	}
	var list []seen
	for k, t := range p.recent {
		list = append(list, seen{k, t})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	for i, e := range list {
		if i == 10 {
			break
		}
		s.Recent = append(s.Recent, e.ip)
	}
	return s
}

// ServerAddress says what is wrong with the address PCs are told to use, if anything: a public address means the server is, or is meant to be, reachable
// from the internet.
func ServerAddress(ip net.IP) error {
	if ip != nil && !local(ip) {
		return errors.New("this server's address is a public one: Bootlace is for a local network and should not be reachable from the internet")
	}
	return nil
}
