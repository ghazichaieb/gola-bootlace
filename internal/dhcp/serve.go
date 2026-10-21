// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

package dhcp

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"
)

// Responder answers a request, or says it has nothing to say.
type Responder interface {
	Respond(req *Packet) (*Packet, bool)
}

// Respond makes Boot a Responder: the proxy answer of Reply.
func (b Boot) Respond(req *Packet) (*Packet, bool) { return b.Reply(req) }

// Serve reads requests from conn until it is closed and sends what the responder says. It runs on any port: 67 (the DHCP server port, where
// discover messages are broadcast) and 4011 (where a PXE client asks the boot server directly) are two calls with two sockets.
func Serve(conn *net.UDPConn, r Responder, logf func(format string, args ...any)) error {
	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		req, err := Parse(buf[:n])
		if err != nil {
			continue // not a DHCP packet
		}
		reply, ok := r.Respond(req)
		if !ok {
			continue
		}
		dst := replyAddr(req, src)
		if _, err := conn.WriteToUDP(reply.Marshal(), dst); err != nil && logf != nil {
			logf("dhcp: cannot answer %s: %v", req.MAC(), err)
		}
		if logf != nil {
			logf("dhcp: %s %s from %s answered with %q to %s", typeName(req.MessageType()), req.MAC(), src.IP, reply.File, dst.IP)
		}
	}
}

// replyAddr is where the answer goes: a relay when there is one, the client's own address when it asked from one without the broadcast flag,
// otherwise the broadcast address on the client port.
func replyAddr(req *Packet, src *net.UDPAddr) *net.UDPAddr {
	if g := req.GIAddr.To4(); g != nil && !g.Equal(net.IPv4zero) {
		return &net.UDPAddr{IP: g, Port: 67}
	}
	if ip := src.IP.To4(); ip != nil && !ip.Equal(net.IPv4zero) && req.Flags&flagBroadcast == 0 {
		return &net.UDPAddr{IP: ip, Port: 68}
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
}

func typeName(t byte) string {
	switch t {
	case Discover:
		return "discover"
	case Request:
		return "request"
	}
	return "message"
}

// Full is a small DHCP server for a network that has none (a lab, a test bench): it hands out addresses from a range and boots PXE clients too.
// Never start it on a network that already has a DHCP server: use the proxy answer there.
type Full struct {
	Boot
	First, Last net.IP // the range of addresses
	Mask        net.IPMask
	Router      net.IP // optional
	DNS         net.IP // optional
	Lease       time.Duration
	PXEOnly     bool // answer PCs that start a network boot only (and the same PCs afterwards, when their system asks for an address)

	mu     sync.Mutex
	leases map[string]uint32 // client hardware address -> address
}

const nak = 6

// Respond answers discover and request messages for this server.
func (f *Full) Respond(req *Packet) (*Packet, bool) {
	if req.Op != bootRequest {
		return nil, false
	}
	t := req.MessageType()
	if t != Discover && t != Request {
		return nil, false
	}
	if f.PXEOnly && !IsPXE(req) && !f.hasLease(req) { // a PC that booted from the network keeps being answered when its system asks for the address again
		return nil, false
	}
	if id := req.Opts[OptServerID]; len(id) == 4 && !net.IP(id).Equal(f.ServerIP) {
		return nil, false // the client chose another server
	}
	ip, ok := f.address(req)
	reply := &Packet{Op: bootReply, HType: req.HType, HLen: req.HLen, XID: req.XID, Secs: req.Secs, Flags: req.Flags, GIAddr: req.GIAddr, CHAddr: req.CHAddr,
		CIAddr: net.IPv4zero, YIAddr: net.IPv4zero, SIAddr: net.IPv4zero, Opts: map[byte][]byte{OptServerID: v4(f.ServerIP)}}
	if !ok {
		if t == Request {
			reply.Opts[OptMessageType] = []byte{nak}
			return reply, true
		}
		return nil, false // no address left: say nothing
	}
	reply.YIAddr = ip
	reply.Opts[OptMessageType] = []byte{Offer}
	if t == Request {
		reply.Opts[OptMessageType] = []byte{ACK}
	}
	reply.Opts[1] = []byte(f.Mask)
	if r := f.Router.To4(); r != nil {
		reply.Opts[3] = r
	}
	if d := f.DNS.To4(); d != nil {
		reply.Opts[6] = d
	}
	reply.Opts[51] = binary.BigEndian.AppendUint32(nil, uint32(f.Lease/time.Second))
	if boot, ok := f.Boot.Reply(req); ok { // a PXE client also gets its boot file
		reply.File, reply.SIAddr = boot.File, f.ServerIP
		reply.Opts[OptVendorClass], reply.Opts[OptVendorOpts] = boot.Opts[OptVendorClass], boot.Opts[OptVendorOpts]
		reply.Opts[66] = []byte(f.ServerIP.String())
		reply.Opts[67] = []byte(boot.File)
	}
	return reply, true
}

func (f *Full) hasLease(req *Packet) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.leases[req.MAC().String()]
	return ok
}

// address is the address for this client: the one it has, the one it asks for (when free and in range) or the first free one.
func (f *Full) address(req *Packet) (net.IP, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leases == nil {
		f.leases = map[string]uint32{}
	}
	mac := req.MAC().String()
	first, last := ipNum(f.First), ipNum(f.Last)
	taken := map[uint32]string{}
	for m, n := range f.leases {
		taken[n] = m
	}
	free := func(n uint32) bool {
		owner, used := taken[n]
		return n >= first && n <= last && (!used || owner == mac)
	}
	if n, ok := f.leases[mac]; ok && free(n) {
		return numIP(n), true
	}
	want := req.Opts[50]
	if len(want) != 4 {
		want = req.CIAddr.To4()
	}
	if len(want) == 4 {
		if n := ipNum(net.IP(want)); free(n) && n != 0 {
			f.leases[mac] = n
			return numIP(n), true
		}
		if req.MessageType() == Request {
			return nil, false // asked for an address that is not available: refuse, the client starts again
		}
	}
	for n := first; n <= last; n++ {
		if _, used := taken[n]; !used {
			f.leases[mac] = n
			return numIP(n), true
		}
	}
	return nil, false
}

func ipNum(ip net.IP) uint32 {
	if b := ip.To4(); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

func numIP(n uint32) net.IP { return net.IP(binary.BigEndian.AppendUint32(nil, n)) }
