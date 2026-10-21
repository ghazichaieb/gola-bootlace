// Package handoff remembers which ISO a PC chose when the way to start it needs the PC's own firmware, not iPXE: FreeBSD's loader cannot use the
// network card while iPXE holds it. iPXE says "this PC (by MAC and address) wants this ISO" and restarts the PC; the DHCP answer of the next network
// boot then names FreeBSD's loader as the boot file, and the TFTP server answers that PC from the chosen ISO.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package handoff

import (
	"strings"
	"sync"
	"time"
)

const (
	pendingFor = 15 * time.Minute // how long the next network boot of a PC is sent to the loader
	chosenFor  = 2 * time.Hour    // how long that PC's TFTP requests are answered from the ISO
)

// Registry is safe for use from several goroutines.
type Registry struct {
	Now func() time.Time // for tests

	mu      sync.Mutex
	pending map[string]entry // by MAC: the next network boot goes to the loader
	chosen  map[string]entry // by IP address: the ISO that PC reads by TFTP
}

type entry struct {
	iso, ip string
	until   time.Time
}

func (r *Registry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func norm(mac string) string { return strings.ToLower(strings.ReplaceAll(mac, "-", ":")) }

// Set records that the PC with this MAC address and IP address wants the ISO.
func (r *Registry) Set(mac, ip, iso string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending, r.chosen = map[string]entry{}, map[string]entry{}
	}
	now := r.now()
	r.pending[norm(mac)] = entry{iso: iso, ip: ip, until: now.Add(pendingFor)}
	r.chosen[ip] = entry{iso: iso, ip: ip, until: now.Add(chosenFor)}
}

// BootFile is the boot file to name in the DHCP answer for this PC, when its next network boot is meant for FreeBSD's loader.
func (r *Registry) BootFile(mac string, efi bool) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.pending[norm(mac)]
	if !ok || r.now().After(e.until) {
		return "", false
	}
	if efi {
		return "boot/loader.efi", true
	}
	return "boot/pxeboot", true
}

// ISO is the ISO the PC at this address chose.
func (r *Registry) ISO(ip string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.chosen[ip]
	if !ok || r.now().After(e.until) {
		return "", false
	}
	return e.iso, true
}

// Fetched is called when the PC at this address has fetched the loader: its next network boot is an ordinary one again.
func (r *Registry) Fetched(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for mac, e := range r.pending {
		if e.ip == ip {
			delete(r.pending, mac)
		}
	}
}
