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
	iso, ip, kind string
	until         time.Time
}

// The kinds of hand-off: what the PC's own firmware is sent to fetch.
const (
	FreeBSD = "freebsd" // FreeBSD's loader (boot/loader.efi, boot/pxeboot)
	Windows = "windows" // Windows' own boot manager, signed by Microsoft: works with UEFI Secure Boot on
	Ubuntu  = "ubuntu"  // Ubuntu's shim and GRUB, signed by Microsoft and Canonical, then Ubuntu's own signed kernel: also works with Secure Boot on
)

func (r *Registry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func norm(mac string) string { return strings.ToLower(strings.ReplaceAll(mac, "-", ":")) }

// Set records that the PC with this MAC address and IP address wants the ISO started by FreeBSD's loader.
func (r *Registry) Set(mac, ip, iso string) { r.SetKind(mac, ip, iso, FreeBSD) }

// SetKind records that the PC with this MAC address and IP address wants the ISO started by the firmware as the kind says.
func (r *Registry) SetKind(mac, ip, iso, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending, r.chosen = map[string]entry{}, map[string]entry{}
	}
	now := r.now()
	r.pending[norm(mac)] = entry{iso: iso, ip: ip, kind: kind, until: now.Add(pendingFor)}
	r.chosen[ip] = entry{iso: iso, ip: ip, kind: kind, until: now.Add(chosenFor)}
}

// BootFile is the boot file to name in the DHCP answer for this PC, when its next network boot is meant for FreeBSD's loader.
func (r *Registry) BootFile(mac string, efi bool) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.pending[norm(mac)]
	if !ok || r.now().After(e.until) {
		return "", false
	}
	if e.kind == Ubuntu {
		if efi {
			return "ubuntu/" + e.iso + "/bootx64.efi", true
		}
		return "", false
	}
	if e.kind == Windows {
		if efi { // the ISO is in the name: the boot manager's first request tells which ISO this PC (whose address is not known yet) is after
			return "win/" + e.iso + "/bootmgfw.efi", true
		}
		return "", false // no BIOS way yet
	}
	if efi {
		return "boot/loader.efi", true
	}
	return "boot/pxeboot", true
}

// Pending records that the next network boot of the PC with this MAC address is to be answered with the boot file of the kind, for the ISO. Its address is
// not known: the boot file's name carries the ISO, and Bind notes the address when the PC asks for it.
func (r *Registry) Pending(mac, iso, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending, r.chosen = map[string]entry{}, map[string]entry{}
	}
	r.pending[norm(mac)] = entry{iso: iso, kind: kind, until: r.now().Add(pendingFor)}
}

// Bind says that the PC at this address fetched the boot file for the ISO: it is answered from the ISO from now on, and no PC is sent there again.
func (r *Registry) Bind(ip, iso, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending, r.chosen = map[string]entry{}, map[string]entry{}
	}
	r.chosen[ip] = entry{iso: iso, ip: ip, kind: kind, until: r.now().Add(chosenFor)}
	for mac, e := range r.pending {
		if e.iso == iso && e.kind == kind && e.ip == "" {
			delete(r.pending, mac)
		}
	}
}

// Kind says what the PC at this address was sent to fetch.
func (r *Registry) Kind(ip string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.chosen[ip]; ok {
		return e.kind
	}
	return ""
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
