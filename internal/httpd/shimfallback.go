// The automatic Windows-native start for a PC whose firmware refuses the Secure Boot shim (issue #79).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// The states of ShimFallback.
const (
	fallbackOffered = "offered" // exactly one Windows ISO can be started this way: the PC's next network boot gets it
	fallbackChoose  = "choose"  // several can: nothing changes for the boot until a person gives the PC a "native" rule for one of them
	fallbackNone    = "none"    // none in the library
	fallbackOff     = "off"     // a person turned the automatic offer off for this PC
)

// ShimFallback is what the PCs page says about a PC flagged as refusing the shim: what its next network boot gets, or why it gets nothing different.
type ShimFallback struct {
	State string `json:"state"`
	ISO   string `json:"iso,omitempty"`   // the ISO that is offered (state "offered")
	Title string `json:"title,omitempty"` // its title
	Count int    `json:"count,omitempty"` // how many ISOs can be started this way (state "choose")
}

// nativeWindows are the ISOs the Windows-native start works for and a person can see in the menu: Windows installation media.
func nativeWindows(list []entryInfo) []entryInfo {
	var out []entryInfo
	for _, e := range list {
		if e.Known && e.Boot.Family == "Windows setup" && !e.Hidden {
			out = append(out, e)
		}
	}
	return out
}

// hasRule says that a person has given this PC, or the kind of PC it is, a rule: a rule of theirs is never overridden by the automatic offer.
func hasRule(cfg config.Config, c Client) bool {
	for _, r := range cfg.Rules {
		if r.MAC == c.MAC {
			return true
		}
	}
	for _, g := range cfg.GroupRules {
		if g.Matches(c.Manufacturer, c.Product, c.Platform) {
			return true
		}
	}
	return false
}

// shimFallbackFor decides what a PC flagged as refusing the shim is offered, from the settings and the ISOs of the library. ok is false when a rule of the person's
// decides for this PC (the page then says nothing about the offer).
func shimFallbackFor(cfg config.Config, c Client, list []entryInfo) (fb ShimFallback, ok bool) {
	if hasRule(cfg, c) {
		return ShimFallback{}, false
	}
	if cfg.PCOptions[c.MAC].NoShimFallback {
		return ShimFallback{State: fallbackOff}, true
	}
	switch wins := nativeWindows(list); len(wins) {
	case 0:
		return ShimFallback{State: fallbackNone}, true
	case 1:
		return ShimFallback{State: fallbackOffered, ISO: wins[0].ISO.ID, Title: wins[0].Title}, true
	default:
		return ShimFallback{State: fallbackChoose, Count: len(wins)}, true
	}
}

// libraryEntries are the ISOs of the library with what is known of each.
func (s *Server) libraryEntries(cfg config.Config) ([]entryInfo, error) {
	if s.entryList != nil {
		return s.entryList(cfg)
	}
	return s.entryInfos(cfg)
}

// annotateShimFallback fills in ShimFallback for the PCs of the list that are flagged. The ISOs are looked at only when there is such a PC.
func (s *Server) annotateShimFallback(list []Client) {
	var infos []entryInfo
	looked := false
	cfg := s.settings()
	for i := range list {
		if !list[i].ShimRefused {
			continue
		}
		if !looked {
			infos, _ = s.libraryEntries(cfg) // (a library that cannot be read has no ISO to offer)
			looked = true
		}
		if fb, ok := shimFallbackFor(cfg, list[i], infos); ok {
			list[i].ShimFallback = &fb
		}
	}
}

// refusingPC is a copy of the record of the PC with this MAC address when it is flagged now as refusing the shim.
func (s *Server) refusingPC(mac string) (Client, bool) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if p := s.clients[mac]; p != nil && p.refusesShim(s.now()) {
		return *p, true
	}
	return Client{}, false
}

// shimFallbackISO is the ISO to answer a flagged PC's network boot with: "" unless it has no rule, has not been switched off and exactly one ISO can be started this way.
func (s *Server) shimFallbackISO(c Client, infos []entryInfo) string {
	if fb, ok := shimFallbackFor(s.settings(), c, infos); ok && fb.State == fallbackOffered {
		return fb.ISO
	}
	return ""
}

// useUpShimFallback ends the flag of a PC that was just answered with the automatic offer, so that the offer is made once and not at every later network boot (a PC
// that has installed Windows must not install it again): if the PC refuses the shim again, it is flagged again.
func (s *Server) useUpShimFallback(mac string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if c := s.clients[mac]; c != nil {
		c.shimDone = time.Time{}
	}
}
