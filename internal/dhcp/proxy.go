// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

package dhcp

import (
	"fmt"
	"net"
	"strings"
)

// Arch is the client system architecture a PXE firmware reports in option 93 (RFC 4578).
type Arch uint16

const (
	ArchBIOS    Arch = 0
	ArchIA32EFI Arch = 6
	ArchX64EFI  Arch = 7
	ArchX64EFI2 Arch = 9 // also seen from some firmware
	ArchARM64   Arch = 11
	archUnknown Arch = 0xffff
)

// Canonical folds the codes that mean the same thing (EFI x64 is reported as 7 by most firmware and 9 by some) into one.
func (a Arch) Canonical() Arch {
	if a == ArchX64EFI2 {
		return ArchX64EFI
	}
	return a
}

func (a Arch) String() string {
	switch a {
	case ArchBIOS:
		return "bios"
	case ArchIA32EFI:
		return "ia32-efi"
	case ArchX64EFI, ArchX64EFI2:
		return "x64-efi"
	case ArchARM64:
		return "arm64-efi"
	}
	return fmt.Sprintf("arch-%d", uint16(a))
}

// IsPXE reports whether the packet comes from a PXE client asking to boot: a DHCP discover or request whose vendor class starts with
// "PXEClient". Anything else (an ordinary computer asking for an address) must be left to the real DHCP server.
func IsPXE(p *Packet) bool {
	if p.Op != bootRequest {
		return false
	}
	if t := p.MessageType(); t != Discover && t != Request {
		return false
	}
	return strings.HasPrefix(string(p.Opts[OptVendorClass]), "PXEClient")
}

// ClientArch is the architecture the client reports; BIOS when it says nothing (very old firmware).
func ClientArch(p *Packet) Arch {
	if v := p.Opts[OptClientArch]; len(v) >= 2 {
		return Arch(uint16(v[0])<<8 | uint16(v[1]))
	}
	return ArchBIOS
}

// IsIPXE reports whether the client already runs iPXE (it says so in its user class, option 77), which means it can be handed a script
// over HTTP instead of a boot loader file.
func IsIPXE(p *Packet) bool {
	return strings.Contains(string(p.Opts[OptUserClass]), "iPXE")
}

// Boot says what to tell PXE clients.
type Boot struct {
	ServerIP net.IP          // this server, as the client reaches it
	Files    map[Arch]string // boot loader file name (served by TFTP) for each architecture
	Script   string          // what a client that already runs iPXE should load, normally http://<server>:<port>/boot.ipxe
	// Handoff may name another boot file for one PC (by MAC) that is starting for the second time because iPXE sent it away to run FreeBSD's loader.
	Handoff func(mac string, efi bool) (string, bool)
	// Seen is told of every PXE client that asks, so that the web interface can list a PC that never gets as far as a menu (Secure Boot refusing iPXE).
	Seen func(mac string, efi, ipxe bool)
}

// Reply builds the proxy answer to a PXE request: an offer to a discover, an acknowledgement to a request. It returns false when the client
// asks for something this server cannot boot (an architecture without a file), so that it stays silent instead of sending a client nowhere.
func (b Boot) Reply(req *Packet) (*Packet, bool) {
	if !IsPXE(req) {
		return nil, false
	}
	file := b.Script
	if b.Seen != nil {
		b.Seen(req.MAC().String(), ClientArch(req).Canonical() != ArchBIOS, IsIPXE(req))
	}
	if !IsIPXE(req) {
		a := ClientArch(req).Canonical()
		if f, ok := b.handoff(req, a); ok {
			file = f
		} else if file, ok = b.Files[a]; !ok {
			return nil, false
		}
	}
	kind := byte(Offer)
	if req.MessageType() == Request {
		kind = ACK
	}
	r := &Packet{
		Op: bootReply, HType: req.HType, HLen: req.HLen, XID: req.XID, Secs: req.Secs, Flags: req.Flags,
		CIAddr: net.IPv4zero, YIAddr: net.IPv4zero, SIAddr: b.ServerIP, GIAddr: req.GIAddr, CHAddr: req.CHAddr,
		File: file,
		Opts: map[byte][]byte{
			OptMessageType: {kind},
			OptServerID:    v4(b.ServerIP),
			OptVendorClass: []byte("PXEClient"),
			// PXE sub-option 6 (discovery control) = 8: "the boot file is named in this answer, do not search for boot servers".
			OptVendorOpts: {6, 1, 8, 255},
		},
	}
	return r, true
}

// Destination is where a reply goes: the client's own address when it has one, a relay when there is one, otherwise a broadcast.
func Destination(req *Packet, port int) *net.UDPAddr {
	switch {
	case !req.GIAddr.Equal(net.IPv4zero) && req.GIAddr.To4() != nil:
		return &net.UDPAddr{IP: req.GIAddr, Port: 67}
	case !req.CIAddr.Equal(net.IPv4zero) && req.CIAddr.To4() != nil:
		return &net.UDPAddr{IP: req.CIAddr, Port: port}
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: port}
}

func (b Boot) handoff(req *Packet, a Arch) (string, bool) {
	if b.Handoff == nil {
		return "", false
	}
	return b.Handoff(req.MAC().String(), a != ArchBIOS)
}
