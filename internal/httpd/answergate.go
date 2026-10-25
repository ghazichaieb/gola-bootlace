// Who may fetch an answer file (issue #60). An installer cannot log in, so the address of a file has to work without a password. What narrows it: a file is given only to a MAC address
// that asked for the menu a while ago, and only a few times; after that the address is closed until the PC asks for the menu again. A stricter setting also asks for the same network
// address as the menu request. This stops a blind download by a host that never asked for a menu. It does NOT stop a host that asks the menu first with the MAC address it wants and
// then fetches: any host of the local network can pose as a PC. The documentation says so.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

const (
	// answerWindow is how long after the last menu request (or the last file an ISO's start fetched, which is a sign the PC is still starting) a PC's installer may fetch its file.
	// An installer fetches after the kernel and the initrd (or a whole ISO held in memory) came down, which takes minutes on a slow link, and a person may leave a PC at a prompt:
	// a window of minutes would break real installs.
	answerWindow = 2 * time.Hour
	// answerFetches is how many files one menu request lets a PC fetch. Ubuntu fetches three files, and may ask for each again; Debian reads its preseed twice; the others once.
	answerFetches = 12
)

// openAnswerGrant records that the PC asked for the menu from this address: it may fetch answer files for a while. It returns a line for the log when another address had asked for
// the same MAC address a short while before (the PC changed address, or another host poses as it); the caller holds clientsMu.
func (s *Server) openAnswerGrant(c *Client, ip string, now time.Time) string {
	warn := ""
	if ip != "" && c.MenuIP != "" && c.MenuIP != ip && now.Sub(c.MenuAt) < answerWindow {
		warn = fmt.Sprintf("%s asked for the menu from %s, and from %s %s ago: if that is not the same PC, another host poses as it", c.MAC, ip, c.MenuIP, now.Sub(c.MenuAt).Round(time.Second))
	}
	c.MenuIP, c.MenuAt, c.AnswersLeft = ip, now, answerFetches
	return warn
}

// keepAnswerGrant renews the window of a PC that is still fetching the files of an ISO from the address it asked from (the caller holds clientsMu). The number of files does not
// grow, and a window that ran out is not opened again by it.
func (s *Server) keepAnswerGrant(c *Client, ip string, now time.Time) {
	if c.MenuIP == ip && !c.MenuAt.IsZero() && now.Sub(c.MenuAt) < answerWindow {
		c.MenuAt = now
	}
}

// takeAnswer says whether the PC with this MAC address may fetch an answer file from this address now, and counts the file. The reason it may not goes to the log, never to the
// requester; a file that is given from another address than the menu was asked from comes with a note for the log. A refused file is not counted.
//
// The address is not asked for by default because it does not hold in real networks: iPXE and the installer's own DHCP client identify themselves differently, and a router that
// leases by client identifier gives the installer another address than the one iPXE had (seen with Ubuntu Server on a home router: the menu at .154, the installer at .155).
func (s *Server) takeAnswer(cfg config.Config, mac, host string, now time.Time, count bool) (ok bool, note string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c := s.clients[mac]
	switch {
	case mac == "" || c == nil:
		return false, "that PC never asked for the menu"
	case c.MenuAt.IsZero() || now.Sub(c.MenuAt) >= answerWindow:
		return false, "that PC asked for the menu too long ago (more than " + answerWindow.String() + ")"
	case host != "" && c.MenuIP != host && cfg.AnswersSameAddress:
		return false, fmt.Sprintf("that PC asked for the menu from %s, not from %s (the setting for the same address is on)", c.MenuIP, host)
	case c.AnswersLeft <= 0:
		return false, fmt.Sprintf("that PC was already given %d files since it asked for the menu: it has to ask for the menu again", answerFetches)
	}
	if count {
		c.AnswersLeft--
		s.changed()
	}
	if host != "" && c.MenuIP != host {
		return true, fmt.Sprintf("the PC asked for the menu from %s", c.MenuIP)
	}
	return true, ""
}

// grantAnswers is the menu request of a PC that the firmware starts by itself (a "native" rule: the DHCP answer names the boot file and there is no menu): the PC may fetch the
// answer file of its Windows set-up from the share.
func (s *Server) grantAnswers(mac string) {
	now := time.Now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c := s.admitLocked(mac, now)
	s.openAnswerGrant(c, "", now)
	s.changed()
}
