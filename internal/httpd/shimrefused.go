// Detecting a PC whose firmware takes the Secure Boot shim and then does not start iPXE (issue #79).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"net"
	"path"
	"strings"
	"time"
)

const (
	// shimGrace is how long a PC that has fetched the whole shim has to ask for the second stage (ipxe.efi) before it is said to refuse it. The shim checks the
	// signature of iPXE (about 1 MB) and asks right away, so on a working PC the request follows the end of the shim's transfer within a few seconds; the rest is
	// room for a slow firmware or a busy network, so that a PC that is merely slow is not reported.
	shimGrace = 30 * time.Second
	// shimMatchWithin is how recently a PC must have asked for a boot file for its first TFTP request to be taken as its own when its address is not known yet
	// (a PC that never loaded the menu has no address in the list).
	shimMatchWithin = 2 * time.Minute
)

// now is the current time, or what the Now field says (tests).
func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NoteTFTP is told by the TFTP server about the transfers to a PC (see tftp.Server.Served). It keeps what is needed to tell a PC that fetched the whole Secure Boot shim
// and never asked for iPXE (the second stage the shim loads): the end of the shim's transfer is remembered, and any later request for the second stage forgets it.
// Only the files of the Secure Boot route count, and only the end of a complete transfer: one that is still running or was abandoned flags nothing.
func (s *Server) NoteTFTP(client net.IP, name string, done bool) {
	r := s.route()
	if !s.on(r) {
		return
	}
	shim := r.shimName()
	isShim := strings.EqualFold(name, secureBootPrefix+shim)
	isSecond := strings.EqualFold(path.Base(name), secondStage(shim))
	if !isShim && !isSecond {
		return
	}
	now := s.now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c := s.clientAtLocked(client.String(), now)
	if c == nil {
		return
	}
	switch {
	case isSecond:
		c.shimDone = time.Time{}
	case isShim && done:
		c.shimDone = now
		s.changed()
	case isShim:
		c.shimDone = time.Time{} // a new attempt
	}
}

// clientAtLocked finds the PC behind a TFTP client address (the caller holds clientsMu): the one listed with that address, or else the only PC that asked for a boot
// file in the last two minutes and has no address yet, which then gets this one. nil when it cannot be told (none, or several, so that no PC is blamed for another's).
func (s *Server) clientAtLocked(ip string, now time.Time) *Client {
	for _, c := range s.clients {
		if c.IP == ip {
			return c
		}
	}
	var candidate *Client
	for _, c := range s.clients {
		if c.IP == "" && now.Sub(c.LastSeen) <= shimMatchWithin {
			if candidate != nil {
				return nil
			}
			candidate = c
		}
	}
	if candidate != nil {
		candidate.IP = ip
	}
	return candidate
}

// refusesShim says whether the PC fetched the whole shim at least shimGrace ago and has asked for neither the second stage nor the menu since.
func (c *Client) refusesShim(now time.Time) bool {
	return !c.shimDone.IsZero() && now.Sub(c.shimDone) >= shimGrace
}
