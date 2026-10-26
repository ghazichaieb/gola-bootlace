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
	// UEFI firmware that boots over HTTP reports its own codes (IANA, RFC 4578 and later): 15 x86, 16 x64, 19 ARM64, 20 BIOS.
	ArchIA32HTTP  Arch = 15
	ArchX64HTTP   Arch = 16
	ArchARM64HTTP Arch = 19
	ArchBIOSHTTP  Arch = 20
	archUnknown   Arch = 0xffff
)

// Canonical folds the codes that mean the same thing (EFI x64 is reported as 7 by most firmware and 9 by some, and as 16 by firmware doing an HTTP boot) into one.
func (a Arch) Canonical() Arch {
	switch a {
	case ArchX64EFI2, ArchX64HTTP:
		return ArchX64EFI
	case ArchIA32HTTP:
		return ArchIA32EFI
	case ArchARM64HTTP:
		return ArchARM64
	case ArchBIOSHTTP:
		return ArchBIOS
	}
	return a
}

func (a Arch) String() string {
	switch a {
	case ArchBIOS:
		return "bios"
	case ArchIA32EFI:
		return "ia32-efi"
	case ArchX64EFI, ArchX64EFI2, ArchX64HTTP:
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

// IsHTTPBoot reports whether the packet comes from a UEFI firmware doing an HTTP boot ("HTTP Boot" in the firmware's boot menu): its vendor class starts with "HTTPClient". It can
// start from a web address instead of a TFTP file, which makes it the way for a PC whose firmware has no PXE but does have HTTP boot.
func IsHTTPBoot(p *Packet) bool {
	if p.Op != bootRequest {
		return false
	}
	if t := p.MessageType(); t != Discover && t != Request {
		return false
	}
	return strings.HasPrefix(string(p.Opts[OptVendorClass]), "HTTPClient")
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
	// Shim may name a Microsoft-signed shim as the boot file of a 64-bit UEFI PC that has UEFI Secure Boot on (and so refuses the unsigned iPXE): the shim then fetches the
	// iPXE that is signed with the owner's own key (grubx64.efi, next to it) by TFTP. It is asked after Handoff, and only for a PC that does not run iPXE yet. nil: not offered.
	Shim func(mac string) (string, bool)
	// Seen is told of every PXE client that asks, so that the web interface can list a PC that never gets as far as a menu (Secure Boot refusing iPXE).
	Seen func(mac string, efi, ipxe bool)
	// LoaderAddress gives the address of a PC that was sent to FreeBSD's loader. The loader makes a DHCP request of its own (with a vendor class of PXEClient but no
	// architecture, which a PC's firmware always sends) and needs an address and this server's name in one answer, which a proxy answer does not have.
	LoaderAddress func(mac string) (net.IP, bool)
	// Mask is the network mask of the network this server is on, told to that loader.
	Mask net.IPMask
	// HTTPBase, when set (http://<server>:<port>), is where a UEFI HTTP boot client is told to fetch the boot loader: <HTTPBase>/ipxe/<the file for its architecture>. Empty answers no HTTP boot client.
	HTTPBase string
}

// Reply builds the proxy answer to a PXE request: an offer to a discover, an acknowledgement to a request. It returns false when the client
// asks for something this server cannot boot (an architecture without a file), so that it stays silent instead of sending a client nowhere.
func (b Boot) Reply(req *Packet) (*Packet, bool) {
	if IsHTTPBoot(req) {
		return b.httpReply(req)
	}
	if !IsPXE(req) {
		return nil, false
	}
	if _, hasArch := req.Opts[OptClientArch]; !hasArch && b.LoaderAddress != nil {
		if ip, ok := b.LoaderAddress(req.MAC().String()); ok {
			return b.loaderReply(req, ip)
		}
	}
	file := b.Script
	if b.Seen != nil {
		b.Seen(req.MAC().String(), ClientArch(req).Canonical() != ArchBIOS, IsIPXE(req))
	}
	if !IsIPXE(req) {
		a := ClientArch(req).Canonical()
		if f, ok := b.handoff(req, a); ok {
			file = f
		} else if f, ok := b.shim(req, a); ok {
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

// httpReply answers a UEFI HTTP boot client: a proxy answer that names the boot loader by a web address (option 67, and the file field when it fits) with the vendor class the firmware
// looks for. The address of the PC is still given by the network's own DHCP server.
func (b Boot) httpReply(req *Packet) (*Packet, bool) {
	if b.HTTPBase == "" {
		return nil, false
	}
	a := ClientArch(req).Canonical()
	file, ok := b.Files[a]
	if !ok || a == ArchBIOS {
		return nil, false
	}
	if b.Seen != nil {
		b.Seen(req.MAC().String(), true, false)
	}
	url := b.HTTPBase + "/ipxe/" + file
	kind := byte(Offer)
	if req.MessageType() == Request {
		kind = ACK
	}
	r := &Packet{
		Op: bootReply, HType: req.HType, HLen: req.HLen, XID: req.XID, Secs: req.Secs, Flags: req.Flags,
		CIAddr: net.IPv4zero, YIAddr: net.IPv4zero, SIAddr: net.IPv4zero, GIAddr: req.GIAddr, CHAddr: req.CHAddr,
		Opts: map[byte][]byte{
			OptMessageType: {kind},
			OptServerID:    v4(b.ServerIP),
			OptVendorClass: []byte("HTTPClient"),
			OptBootFile:    []byte(url),
		},
	}
	if len(url) < 128 {
		r.File = url
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

// shim is the signed shim to send a 64-bit UEFI PC to, when the server offers one and this PC is to use it.
func (b Boot) shim(req *Packet, a Arch) (string, bool) {
	if b.Shim == nil || a != ArchX64EFI {
		return "", false
	}
	return b.Shim(req.MAC().String())
}

// loaderReply answers the DHCP request that FreeBSD's loader makes after the firmware has started it: the address the PC already had (the network's DHCP server gave it a
// moment ago) and this server as the server to read the kernel and the ISO from. A request meant for another DHCP server (the network's own) is left alone.
func (b Boot) loaderReply(req *Packet, ip net.IP) (*Packet, bool) {
	if id := req.Opts[OptServerID]; len(id) == 4 && !net.IP(id).Equal(b.ServerIP) {
		return nil, false
	}
	kind := byte(Offer)
	switch req.MessageType() {
	case Discover:
	case Request:
		kind = ACK
	default:
		return nil, false
	}
	mask := b.Mask
	if len(mask) != 4 {
		mask = net.CIDRMask(24, 32)
	}
	return &Packet{
		Op: bootReply, HType: req.HType, HLen: req.HLen, XID: req.XID, Secs: req.Secs, Flags: req.Flags,
		CIAddr: net.IPv4zero, YIAddr: ip, SIAddr: b.ServerIP, GIAddr: req.GIAddr, CHAddr: req.CHAddr,
		File: "boot/loader.efi",
		Opts: map[byte][]byte{
			OptMessageType: {kind},
			OptServerID:    v4(b.ServerIP),
			OptSubnetMask:  []byte(mask),
			OptLeaseTime:   {0, 0, 0x0e, 0x10}, // an hour: the PC only needs it until it has started FreeBSD
			// Without a root path that starts with tftp:// FreeBSD's loader reads its files by NFS (it asks this server's port 111 and waits for ever).
			OptRootPath: []byte("tftp://" + b.ServerIP.String() + "/"),
		},
	}, true
}
