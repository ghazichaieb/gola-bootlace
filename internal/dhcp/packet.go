// Package dhcp reads DHCP packets from PXE clients and builds the proxy answers Bootlace gives them.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
//
// A proxy DHCP server does not hand out addresses: the network's own DHCP server does that. It only answers PXE clients with "boot this file
// from that server", so Bootlace can be added to a network without touching the router.
package dhcp

import (
	"encoding/binary"
	"errors"
	"net"
)

// Message types (option 53).
const (
	Discover = 1
	Offer    = 2
	Request  = 3
	ACK      = 5
)

// Option numbers used here.
const (
	OptSubnetMask   = 1
	OptRootPath     = 17
	OptLeaseTime    = 51
	OptMessageType  = 53
	OptServerID     = 54
	OptBootFile     = 67
	OptVendorClass  = 60
	OptClientArch   = 93
	OptUserClass    = 77
	OptVendorOpts   = 43
	OptEnd          = 255
	optPad          = 0
	headerLen       = 236
	minPacketLen    = headerLen + 4
	magicCookie     = 0x63825363
	bootRequest     = 1
	bootReply       = 2
	flagBroadcast   = 0x8000
	maxReplyPayload = 576
)

// Packet is a DHCP packet: the fixed BOOTP header and the options.
type Packet struct {
	Op     byte
	HType  byte
	HLen   byte
	XID    uint32
	Secs   uint16
	Flags  uint16
	CIAddr net.IP
	YIAddr net.IP
	SIAddr net.IP
	GIAddr net.IP
	CHAddr [16]byte
	SName  string
	File   string
	Opts   map[byte][]byte
}

// Parse decodes a packet received from the network.
func Parse(b []byte) (*Packet, error) {
	if len(b) < minPacketLen {
		return nil, errors.New("dhcp: packet too short")
	}
	if binary.BigEndian.Uint32(b[headerLen:]) != magicCookie {
		return nil, errors.New("dhcp: missing magic cookie (this is plain BOOTP, not DHCP)")
	}
	p := &Packet{
		Op: b[0], HType: b[1], HLen: b[2],
		XID:    binary.BigEndian.Uint32(b[4:]),
		Secs:   binary.BigEndian.Uint16(b[8:]),
		Flags:  binary.BigEndian.Uint16(b[10:]),
		CIAddr: net.IP(append([]byte(nil), b[12:16]...)),
		YIAddr: net.IP(append([]byte(nil), b[16:20]...)),
		SIAddr: net.IP(append([]byte(nil), b[20:24]...)),
		GIAddr: net.IP(append([]byte(nil), b[24:28]...)),
		SName:  cString(b[44:108]),
		File:   cString(b[108:236]),
		Opts:   map[byte][]byte{},
	}
	copy(p.CHAddr[:], b[28:44])
	for i := minPacketLen; i < len(b); {
		code := b[i]
		if code == optPad {
			i++
			continue
		}
		if code == OptEnd {
			break
		}
		if i+1 >= len(b) || i+2+int(b[i+1]) > len(b) {
			return nil, errors.New("dhcp: option runs past the end of the packet")
		}
		n := int(b[i+1])
		p.Opts[code] = append([]byte(nil), b[i+2:i+2+n]...)
		i += 2 + n
	}
	return p, nil
}

// Marshal encodes the packet. Options are written in a fixed order (message type first) so the output is the same every time.
func (p *Packet) Marshal() []byte {
	b := make([]byte, headerLen, maxReplyPayload)
	b[0], b[1], b[2] = p.Op, p.HType, p.HLen
	binary.BigEndian.PutUint32(b[4:], p.XID)
	binary.BigEndian.PutUint16(b[8:], p.Secs)
	binary.BigEndian.PutUint16(b[10:], p.Flags)
	copy(b[12:16], v4(p.CIAddr))
	copy(b[16:20], v4(p.YIAddr))
	copy(b[20:24], v4(p.SIAddr))
	copy(b[24:28], v4(p.GIAddr))
	copy(b[28:44], p.CHAddr[:])
	copy(b[44:108], p.SName)
	copy(b[108:236], p.File)
	b = binary.BigEndian.AppendUint32(b, magicCookie)
	order := []byte{OptMessageType, OptServerID, OptVendorClass, OptVendorOpts}
	written := map[byte]bool{}
	for _, code := range order {
		if v, ok := p.Opts[code]; ok {
			b = append(b, code, byte(len(v)))
			b = append(b, v...)
			written[code] = true
		}
	}
	for code := 1; code < 255; code++ {
		if v, ok := p.Opts[byte(code)]; ok && !written[byte(code)] {
			b = append(b, byte(code), byte(len(v)))
			b = append(b, v...)
		}
	}
	return append(b, OptEnd)
}

// MessageType is the DHCP message type (option 53), or 0 when the packet has none.
func (p *Packet) MessageType() byte {
	if v := p.Opts[OptMessageType]; len(v) == 1 {
		return v[0]
	}
	return 0
}

// MAC is the client's hardware address.
func (p *Packet) MAC() net.HardwareAddr {
	n := int(p.HLen)
	if n > 16 {
		n = 16
	}
	return net.HardwareAddr(append([]byte(nil), p.CHAddr[:n]...))
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func v4(ip net.IP) []byte {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return []byte{0, 0, 0, 0}
}
