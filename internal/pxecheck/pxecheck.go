// Package pxecheck looks for other servers that answer a PC's network boot: it sends a PXE DHCP discover, as a PC's firmware does, on the server's network and listens for a
// few seconds. Two servers answering the same PC (another Bootlace, iVentoy, a router's PXE option, a WDS server) is the most common reason a network boot fails in a way
// that looks like Bootlace's fault. It sends broadcast traffic, so it is only run when asked and once at start.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package pxecheck

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
)

// Options say how to look.
type Options struct {
	Local net.IP        // this server's address: the question leaves from it, so that it goes out of the right network card
	Mask  net.IPMask    // the mask of that network (the question goes to its broadcast address); nil: 255.255.255.255
	Full  bool          // this server hands out addresses itself: any other DHCP server is then a conflict, not only a PXE one
	Wait  time.Duration // how long to listen (default 4 s)

	// For tests: where to listen and where to send.
	ListenAddr string // default ":68"
	SendTo     string // default "<broadcast>:67"
}

// Answer is one server that answered.
type Answer struct {
	From       string `json:"from"`                 // the address the answer came from
	ServerID   string `json:"serverId,omitempty"`   // the server identifier (option 54)
	Offered    string `json:"offered,omitempty"`    // the address it offers the PC; empty for a proxy answer
	NextServer string `json:"nextServer,omitempty"` // where to boot from (next-server or option 66)
	BootFile   string `json:"bootFile,omitempty"`   // what to boot (file field or option 67)
	PXE        bool   `json:"pxe"`                  // it offers a boot file or says PXEClient: it answers network boots
	Self       bool   `json:"self"`                 // it is this server
	Conflict   bool   `json:"conflict"`             // it will compete with this server for the PCs
	Note       string `json:"note"`                 // what it is, in words
}

// Result is what one look found.
type Result struct {
	Time    time.Time `json:"time"`
	Answers []Answer  `json:"answers"`
	Error   string    `json:"error,omitempty"`
}

// Conflicts counts the answers that compete with this server.
func (r Result) Conflicts() int {
	n := 0
	for _, a := range r.Answers {
		if a.Conflict {
			n++
		}
	}
	return n
}

// Scan sends the question and collects the answers until Wait has passed or ctx ends.
func Scan(ctx context.Context, o Options) (Result, error) {
	res := Result{Time: time.Now(), Answers: []Answer{}}
	if o.Wait <= 0 {
		o.Wait = 4 * time.Second
	}
	listen := o.ListenAddr
	if listen == "" {
		listen = ":68"
	}
	lc := net.ListenConfig{Control: reuseAddr}
	pc, err := lc.ListenPacket(ctx, "udp4", listen)
	if err != nil {
		return res, fmt.Errorf("cannot listen on UDP port 68 (a DHCP client on this computer may hold it): %w", err)
	}
	defer pc.Close()

	send, err := net.ListenUDP("udp4", &net.UDPAddr{IP: o.Local})
	if err != nil {
		return res, err
	}
	defer send.Close()
	dest := o.SendTo
	if dest == "" {
		dest = "255.255.255.255:67"
		if o.Local.To4() != nil && len(o.Mask) == 4 {
			b := make(net.IP, 4)
			for i := range b {
				b[i] = o.Local.To4()[i] | ^o.Mask[i]
			}
			dest = net.JoinHostPort(b.String(), "67")
		}
	}
	to, err := net.ResolveUDPAddr("udp4", dest)
	if err != nil {
		return res, err
	}
	xid := make([]byte, 4)
	_, _ = rand.Read(xid)
	id := binary.BigEndian.Uint32(xid)
	if _, err := send.WriteToUDP(discover(id), to); err != nil {
		return res, fmt.Errorf("cannot send the question: %w", err)
	}

	deadline := time.Now().Add(o.Wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = pc.SetReadDeadline(deadline)
	go func() { // (ends the wait when the request is cancelled)
		<-ctx.Done()
		_ = pc.SetReadDeadline(time.Now())
	}()
	buf := make([]byte, 1500)
	seen := map[string]bool{}
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			break
		}
		p, err := dhcp.Parse(buf[:n])
		if err != nil || p.Op != 2 || p.XID != id || (p.MessageType() != dhcp.Offer && p.MessageType() != dhcp.ACK) {
			continue
		}
		host, _, _ := net.SplitHostPort(from.String())
		a := describe(p, host, o)
		if key := a.From + "|" + a.ServerID; !seen[key] {
			seen[key] = true
			res.Answers = append(res.Answers, a)
		}
	}
	sort.SliceStable(res.Answers, func(i, j int) bool { return res.Answers[i].Self && !res.Answers[j].Self })
	if errors.Is(ctx.Err(), context.Canceled) {
		return res, ctx.Err()
	}
	return res, nil
}

// mac is the hardware address the question carries: one of the range reserved for local use, so that it is never a real card's.
var mac = [6]byte{0x02, 0x42, 0x42, 0x42, 0x42, 0x42}

// discover is the DHCP discover of a UEFI x64 PC's firmware that wants to network boot.
func discover(xid uint32) []byte {
	p := &dhcp.Packet{Op: 1, HType: 1, HLen: 6, XID: xid, Flags: 0x8000, CIAddr: net.IPv4zero, YIAddr: net.IPv4zero, SIAddr: net.IPv4zero, GIAddr: net.IPv4zero, Opts: map[byte][]byte{}}
	copy(p.CHAddr[:], mac[:])
	p.Opts[dhcp.OptMessageType] = []byte{dhcp.Discover}
	p.Opts[dhcp.OptVendorClass] = []byte("PXEClient:Arch:00007:UNDI:003016")
	p.Opts[dhcp.OptClientArch] = []byte{0, 7}
	p.Opts[55] = []byte{1, 3, 6, 15, 43, 54, 60, 66, 67, 97}
	p.Opts[61] = append([]byte{1}, mac[:]...)
	p.Opts[77] = []byte("Bootlace-check")
	return p.Marshal()
}

func describe(p *dhcp.Packet, from string, o Options) Answer {
	a := Answer{From: from}
	if id := p.Opts[dhcp.OptServerID]; len(id) == 4 {
		a.ServerID = net.IP(id).String()
	}
	if y := p.YIAddr.To4(); y != nil && !y.IsUnspecified() {
		a.Offered = y.String()
	}
	file := p.File
	if v, ok := p.Opts[67]; ok && len(v) > 0 {
		file = strings.TrimRight(string(v), "\x00")
	}
	a.BootFile = file
	if v, ok := p.Opts[66]; ok && len(v) > 0 {
		a.NextServer = strings.TrimRight(string(v), "\x00")
	} else if s := p.SIAddr.To4(); s != nil && !s.IsUnspecified() {
		a.NextServer = s.String()
	}
	a.PXE = file != "" || strings.HasPrefix(string(p.Opts[dhcp.OptVendorClass]), "PXEClient")
	if o.Local != nil { // (the server identifier is what names a server; the address the answer came from only when there is none)
		if a.ServerID != "" {
			a.Self = a.ServerID == o.Local.String()
		} else {
			a.Self = from == o.Local.String()
		}
	}
	switch {
	case a.Self:
		a.Note = "this server"
	case a.PXE && a.Offered == "":
		a.Conflict, a.Note = true, "another server that answers network boots (a proxy DHCP, like iVentoy, another Bootlace or a WDS server)"
	case a.PXE:
		a.Conflict, a.Note = true, "a DHCP server that gives a boot file to PCs (the router's or server's PXE option)"
	case o.Full:
		a.Conflict, a.Note = true, "another DHCP server on this network, while this server hands out addresses itself"
	default:
		a.Note = "the network's own DHCP server (normal next to the proxy mode)"
	}
	return a
}

// Checker runs the look, keeps the last result and writes what it found to the log. One look at a time.
type Checker struct {
	Options func() Options // read at every look (the DHCP mode and the address do not change while the program runs, but a test sets them)
	Logf    func(format string, args ...any)

	run  sync.Mutex
	mu   sync.Mutex
	last *Result
}

// ErrBusy is the answer of Run when a look is already going on.
var ErrBusy = errors.New("a look for other servers is already going on")

// Run looks now and keeps the result.
func (c *Checker) Run(ctx context.Context) (Result, error) {
	if !c.run.TryLock() {
		return Result{}, ErrBusy
	}
	defer c.run.Unlock()
	res, err := Scan(ctx, c.Options())
	if err != nil {
		res.Error = err.Error()
	}
	c.mu.Lock()
	c.last = &res
	c.mu.Unlock()
	if c.Logf != nil {
		switch {
		case err != nil:
			c.Logf("check for other PXE or DHCP servers: %v", err)
		case res.Conflicts() > 0:
			for _, a := range res.Answers {
				if a.Conflict {
					c.Logf("warning: %s (%s) answers network boots too: %s", a.From, a.ServerID, a.Note)
				}
			}
		default:
			c.Logf("check for other PXE or DHCP servers: %d answer(s), no other server that competes with this one", len(res.Answers))
		}
	}
	return res, err
}

// Last is the result of the last look, nil if there was none.
func (c *Checker) Last() *Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}
