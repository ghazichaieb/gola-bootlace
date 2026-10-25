// Package iscsi is a read-only iSCSI target: it shows an ISO file to a PC as a CD-ROM that is read over the network a piece at a time, so that nothing has to be copied
// into the PC's memory first (iPXE's `sanboot` with an https:// address downloads the whole file). It speaks the part of RFC 7143 that an initiator needs to log in
// (no authentication, one connection, no digests) and read: INQUIRY, REPORT LUNS, READ CAPACITY and READ.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package iscsi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// BlockSize is the size of a block of an ISO (and of a CD-ROM); DiskBlockSize that of a hard disk image.
const (
	BlockSize     = 2048
	DiskBlockSize = 512
)

// IQNPrefix starts the name of every target; the ISO's id follows.
const IQNPrefix = "iqn.2026-10.org.gola:bootlace:"

// Disc is what is shown: a file, read at any place.
type Disc struct {
	R    io.ReaderAt
	Size int64
	Done func()
	Disk bool // shown as a hard disk (512-byte blocks) instead of a CD-ROM
}

// blockSize is the block size the initiator sees.
func (d Disc) blockSize() int64 {
	if d.Disk {
		return DiskBlockSize
	}
	return BlockSize
}

// deviceType is the SCSI peripheral device type: 0 direct-access (disk), 5 CD-ROM.
func (d Disc) deviceType() byte {
	if d.Disk {
		return 0x00
	}
	return 0x05
}

// Target answers the initiators that connect to a listener.
type Target struct {
	// Open finds the disc of a target name's id (the part after IQNPrefix); false when there is none.
	Open func(id string) (Disc, bool)
	// IDs lists the ids that can be opened (for discovery); may be nil.
	IDs func() []string
	// Address is how initiators reach this target, for discovery ("host:port,1").
	Address string
	Logf    func(format string, args ...any)
}

// the opcodes
const (
	opNopOut     = 0x00
	opSCSICmd    = 0x01
	opLoginReq   = 0x03
	opTextReq    = 0x04
	opLogoutReq  = 0x06
	opNopIn      = 0x20
	opSCSIResp   = 0x21
	opLoginResp  = 0x23
	opTextResp   = 0x24
	opDataIn     = 0x25
	opLogoutResp = 0x26
	opReject     = 0x3f
)

// Limits against an initiator that asks for too much of the server's memory or time: sessions open at once, how long a login or a quiet session may take, and the most one read command
// may ask for (initiators read a few hundred KB at a time, 1 to 4 MB at the most).
const (
	maxSessions = 256
	loginTime   = 30 * time.Second
	idleTime    = 6 * time.Hour // (a PC may sit at the first screen of an installer for hours with its disk on the network; initiators ping, but not all of them)
	maxRead     = 8 << 20
)

// Serve accepts initiators until the listener is closed.
func (t *Target) Serve(l net.Listener) error {
	slots := make(chan struct{}, maxSessions)
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		default: // (every session holds a few hundred KB of buffers and may hold a read of megabytes: a flood of connections must not take the server's memory)
			if t.Logf != nil {
				t.Logf("iscsi: %s: refused, %d sessions are open already", c.RemoteAddr(), maxSessions)
			}
			c.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			defer c.Close()
			if err := t.session(c); err != nil && !errors.Is(err, io.EOF) && t.Logf != nil {
				t.Logf("iscsi: %s: %v", c.RemoteAddr(), err)
			}
		}()
	}
}

type pdu struct {
	bhs  [48]byte
	data []byte
}

func (p *pdu) opcode() byte  { return p.bhs[0] & 0x3f }
func (p *pdu) flags() byte   { return p.bhs[1] }
func (p *pdu) itt() uint32   { return binary.BigEndian.Uint32(p.bhs[16:]) }
func (p *pdu) cmdSN() uint32 { return binary.BigEndian.Uint32(p.bhs[24:]) }

func readPDU(r io.Reader) (*pdu, error) {
	p := &pdu{}
	if _, err := io.ReadFull(r, p.bhs[:]); err != nil {
		return nil, err
	}
	ahs := int(p.bhs[4]) * 4
	dsl := int(p.bhs[5])<<16 | int(p.bhs[6])<<8 | int(p.bhs[7])
	if dsl > 1<<20 {
		return nil, errors.New("a segment that large is refused")
	}
	if ahs > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(ahs)); err != nil {
			return nil, err
		}
	}
	padded := (dsl + 3) &^ 3
	buf := make([]byte, padded)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	p.data = buf[:dsl]
	return p, nil
}

func (t *Target) session(c net.Conn) error {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	r := bufio.NewReaderSize(c, 64<<10)
	w := bufio.NewWriterSize(c, 256<<10)
	s := &conn{t: t, r: r, w: w, maxRecv: 8192}
	defer func() {
		if s.disc.Done != nil {
			s.disc.Done()
		}
	}()
	c.SetReadDeadline(time.Now().Add(loginTime)) //nolint:errcheck // (a login that does not finish is dropped)
	if err := s.login(); err != nil {
		return err
	}
	for {
		c.SetReadDeadline(time.Now().Add(idleTime)) //nolint:errcheck // (a session that says nothing for this long is dropped)
		p, err := readPDU(r)
		if err != nil {
			return err
		}
		switch p.opcode() {
		case opSCSICmd:
			if err := s.command(p); err != nil {
				return err
			}
		case opNopOut:
			if err := s.nop(p); err != nil {
				return err
			}
		case opTextReq:
			if err := s.text(p); err != nil {
				return err
			}
		case opLogoutReq:
			s.noteCmd(p)
			b := s.header(opLogoutResp, 0x80, p.itt())
			s.finish(b, nil)
			_ = w.Flush()
			return nil
		default:
			// anything else (task management, SNACK ...) is not supported: say so and go on
			b := s.header(opReject, 0x80, 0xffffffff)
			b[2] = 0x04 // command not supported
			s.finish(b, p.bhs[:])
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
}

// conn is the state of one session.
type conn struct {
	t       *Target
	r       *bufio.Reader
	w       *bufio.Writer
	disc    Disc
	statSN  uint32
	expCmd  uint32
	maxCmd  uint32
	maxRecv int // the initiator's MaxRecvDataSegmentLength: the biggest piece of data it takes in one PDU
	tsih    uint16
	isid    [6]byte
}

// header starts a response BHS with the common fields.
func (s *conn) header(op, flags byte, itt uint32) []byte {
	b := make([]byte, 48)
	b[0] = op
	b[1] = flags
	binary.BigEndian.PutUint32(b[16:], itt)
	return b
}

// finish fills the sequence numbers, adds the data segment and queues the PDU. The sequence numbers are those of "now": every response carries the next StatSN
// (except Data-In without status, which carries zero).
func (s *conn) finish(b []byte, data []byte) {
	binary.BigEndian.PutUint32(b[24:], s.statSN)
	binary.BigEndian.PutUint32(b[28:], s.expCmd)
	binary.BigEndian.PutUint32(b[32:], s.maxCmd)
	s.statSN++
	s.send(b, data)
}

func (s *conn) send(b []byte, data []byte) {
	b[5], b[6], b[7] = byte(len(data)>>16), byte(len(data)>>8), byte(len(data))
	_, _ = s.w.Write(b)
	if len(data) > 0 {
		_, _ = s.w.Write(data)
		if pad := (4 - len(data)%4) % 4; pad > 0 {
			_, _ = s.w.Write(make([]byte, pad))
		}
	}
}

// noteCmd keeps the command window moving: every command that is not "immediate" takes one place, and the window stays 64 wide.
func (s *conn) noteCmd(p *pdu) {
	if p.bhs[0]&0x40 == 0 { // not immediate
		s.expCmd = p.cmdSN() + 1
	}
	s.maxCmd = s.expCmd + 64
}

func parseKV(data []byte) [][2]string {
	var out [][2]string
	for _, kv := range bytes.Split(data, []byte{0}) {
		if len(kv) == 0 {
			continue
		}
		k, v, _ := strings.Cut(string(kv), "=")
		out = append(out, [2]string{k, v})
	}
	return out
}

func (s *conn) login() error {
	discovery := false
	for {
		p, err := readPDU(s.r)
		if err != nil {
			return err
		}
		if p.opcode() != opLoginReq {
			return errors.New("the first PDU was not a login")
		}
		if s.tsih == 0 {
			copy(s.isid[:], p.bhs[8:14])
			s.tsih = 1
			s.expCmd = p.cmdSN()
			s.maxCmd = s.expCmd + 64
		}
		csg, nsg, transit := (p.flags()>>2)&3, p.flags()&3, p.flags()&0x80 != 0
		var reply bytes.Buffer
		kv := func(k, v string) { fmt.Fprintf(&reply, "%s=%s\x00", k, v) }
		status := byte(0)
		for _, e := range parseKV(p.data) {
			switch e[0] {
			case "SessionType":
				if e[1] == "Discovery" { // an initiator (dracut's iscsiadm) asks which targets there are before it logs in to one
					discovery = true
				}
			case "InitiatorName", "InitiatorAlias":
			case "TargetName":
				id, ok := strings.CutPrefix(e[1], IQNPrefix)
				if !ok {
					status = 0x03 // initiator error: not found
					break
				}
				d, ok := s.t.Open(id)
				if !ok {
					status = 0x03
					break
				}
				if s.disc.Done != nil {
					s.disc.Done()
				}
				s.disc = d
			case "AuthMethod":
				kv("AuthMethod", "None")
			case "MaxRecvDataSegmentLength": // declarative: the biggest piece the initiator takes
				if n, err := strconv.Atoi(e[1]); err == nil && n >= 512 {
					s.maxRecv = n
					if s.maxRecv > 256<<10 {
						s.maxRecv = 256 << 10
					}
				}
				kv("MaxRecvDataSegmentLength", "65536")
			case "HeaderDigest", "DataDigest":
				kv(e[0], "None")
			case "MaxConnections":
				kv(e[0], "1")
			case "InitialR2T":
				kv(e[0], "Yes")
			case "ImmediateData":
				kv(e[0], "No")
			case "MaxBurstLength":
				kv(e[0], "262144")
			case "FirstBurstLength":
				kv(e[0], "65536")
			case "DefaultTime2Wait":
				kv(e[0], "2")
			case "DefaultTime2Retain":
				kv(e[0], "0")
			case "MaxOutstandingR2T":
				kv(e[0], "1")
			case "DataPDUInOrder", "DataSequenceInOrder":
				kv(e[0], "Yes")
			case "ErrorRecoveryLevel":
				kv(e[0], "0")
			default:
				kv(e[0], "NotUnderstood")
			}
		}
		if csg == 0 && status == 0 {
			kv("TargetPortalGroupTag", "1")
		}
		b := s.header(opLoginResp, p.flags()&0xcf, p.itt())
		if transit && status == 0 {
			b[1] = 0x80 | byte(csg)<<2 | nsg
		} else {
			b[1] = byte(csg)<<2 | (p.flags() & 3) // not leaving the stage yet
			if transit {
				b[1] |= 0x80
			}
		}
		b[2], b[3] = 0, 0 // version max, active
		copy(b[8:14], s.isid[:])
		binary.BigEndian.PutUint16(b[14:], s.tsih)
		b[36], b[37] = 0, 0
		if status != 0 {
			b[36], b[37] = 0x02, status // initiator error, "target not found"
			b[1] &^= 0x80
		}
		binary.BigEndian.PutUint32(b[24:], s.statSN)
		binary.BigEndian.PutUint32(b[28:], s.expCmd)
		binary.BigEndian.PutUint32(b[32:], s.maxCmd)
		s.statSN++
		s.send(b, reply.Bytes())
		if err := s.w.Flush(); err != nil {
			return err
		}
		if status != 0 {
			return errors.New("login for an unknown target refused")
		}
		if transit && nsg == 3 {
			if s.disc.R == nil && !discovery {
				return errors.New("logged in without a target")
			}
			if s.t.Logf != nil {
				if discovery {
					s.t.Logf("iscsi: a PC asks which targets there are")
				} else {
					s.t.Logf("iscsi: a PC reads an ISO as a CD-ROM")
				}
			}
			return nil
		}
	}
}

func (s *conn) nop(p *pdu) error {
	s.noteCmd(p)
	b := s.header(opNopIn, 0x80, p.itt())
	binary.BigEndian.PutUint32(b[20:], 0xffffffff)
	s.finish(b, p.data)
	return nil
}

func (s *conn) text(p *pdu) error {
	s.noteCmd(p)
	var out bytes.Buffer
	for _, e := range parseKV(p.data) {
		if e[0] == "SendTargets" && s.t.IDs != nil {
			for _, id := range s.t.IDs() {
				fmt.Fprintf(&out, "TargetName=%s%s\x00TargetAddress=%s\x00", IQNPrefix, id, s.t.Address)
			}
		}
	}
	b := s.header(opTextResp, 0x80, p.itt())
	binary.BigEndian.PutUint32(b[20:], 0xffffffff)
	s.finish(b, out.Bytes())
	return nil
}

// SCSI status and sense
const (
	statusGood           = 0x00
	statusCheckCondition = 0x02
)

type sense struct{ key, asc, ascq byte }

var (
	senseInvalidOpcode = sense{0x05, 0x20, 0x00}
	senseInvalidField  = sense{0x05, 0x24, 0x00}
	senseOutOfRange    = sense{0x05, 0x21, 0x00}
	senseWriteProtect  = sense{0x07, 0x27, 0x00}
	senseMediumError   = sense{0x03, 0x11, 0x00}
)

func (s *conn) respond(itt uint32, status byte, sn *sense, expected, sent uint32) {
	b := s.header(opSCSIResp, 0x80, itt)
	b[3] = status
	if expected > sent {
		b[1] |= 0x02 // underflow
		binary.BigEndian.PutUint32(b[44:], expected-sent)
	} else if sent > expected {
		b[1] |= 0x04 // overflow
		binary.BigEndian.PutUint32(b[44:], sent-expected)
	}
	var data []byte
	if sn != nil {
		data = make([]byte, 2+18)
		binary.BigEndian.PutUint16(data, 18)
		data[2] = 0x70
		data[4] = sn.key
		data[9] = 10
		data[14], data[15] = sn.asc, sn.ascq
	}
	s.finish(b, data)
}

// dataIn sends bytes to the initiator in pieces it can take, then the status.
func (s *conn) dataIn(itt uint32, data []byte, expected uint32) {
	max := s.maxRecv
	var dsn uint32
	for off := 0; off < len(data); off += max {
		end := off + max
		if end > len(data) {
			end = len(data)
		}
		b := s.header(opDataIn, 0, itt)
		binary.BigEndian.PutUint32(b[20:], 0xffffffff)
		binary.BigEndian.PutUint32(b[28:], s.expCmd)
		binary.BigEndian.PutUint32(b[32:], s.maxCmd)
		binary.BigEndian.PutUint32(b[36:], dsn)
		binary.BigEndian.PutUint32(b[40:], uint32(off))
		if end == len(data) {
			b[1] = 0x80 // final
		}
		s.send(b, data[off:end])
		dsn++
	}
	s.respond(itt, statusGood, nil, expected, uint32(len(data)))
}

var inquiryVendor = []byte("GOLA    Bootlace ISO    0001")

func (s *conn) command(p *pdu) error {
	s.noteCmd(p)
	itt := p.itt()
	expected := binary.BigEndian.Uint32(p.bhs[20:])
	cdb := p.bhs[32:48]
	bs := s.disc.blockSize()
	dt := s.disc.deviceType()
	blocks := uint64(s.disc.Size / bs)
	switch cdb[0] {
	case 0x00, 0x1b, 0x1e, 0x35: // test unit ready, start/stop, prevent/allow medium removal, synchronize cache
		s.respond(itt, statusGood, nil, 0, 0)
	case 0x03: // request sense: nothing wrong
		d := make([]byte, 18)
		d[0], d[7] = 0x70, 10
		s.dataIn(itt, cut(d, int(cdb[4])), expected)
	case 0x12: // inquiry
		alloc := int(binary.BigEndian.Uint16(cdb[3:]))
		if cdb[1]&1 == 0 {
			d := make([]byte, 36)
			d[0], d[1], d[2], d[3], d[4] = dt, 0x80, 0x05, 0x02, 31 // CD-ROM, removable, SPC-3
			if s.disc.Disk {
				d[1] = 0x00 // a disk is not removable
			}
			copy(d[8:], inquiryVendor)
			s.dataIn(itt, cut(d, alloc), expected)
			break
		}
		switch cdb[2] {
		case 0x00:
			s.dataIn(itt, cut([]byte{dt, 0x00, 0x00, 0x03, 0x00, 0x80, 0x83}, alloc), expected)
		case 0x80:
			serial := []byte("BOOTLACE0001")
			d := append([]byte{dt, 0x80, 0x00, byte(len(serial))}, serial...)
			s.dataIn(itt, cut(d, alloc), expected)
		case 0x83:
			id := []byte("Gola Bootlace")
			d := append([]byte{dt, 0x83, 0x00, byte(4 + len(id)), 0x02, 0x08, 0x00, byte(len(id))}, id...) // an ASCII name (type 8: SCSI name string)
			s.dataIn(itt, cut(d, alloc), expected)
		default:
			s.respond(itt, statusCheckCondition, &senseInvalidField, expected, 0)
		}
	case 0xa0: // report luns
		d := make([]byte, 16)
		binary.BigEndian.PutUint32(d, 8)
		s.dataIn(itt, cut(d, int(binary.BigEndian.Uint32(cdb[6:]))), expected)
	case 0x25: // read capacity (10)
		d := make([]byte, 8)
		last := blocks - 1
		if last > 0xffffffff {
			last = 0xffffffff
		}
		binary.BigEndian.PutUint32(d, uint32(last))
		binary.BigEndian.PutUint32(d[4:], uint32(bs))
		s.dataIn(itt, d, expected)
	case 0x9e: // service action in: read capacity (16)
		if cdb[1]&0x1f != 0x10 {
			s.respond(itt, statusCheckCondition, &senseInvalidField, expected, 0)
			break
		}
		d := make([]byte, 32)
		binary.BigEndian.PutUint64(d, blocks-1)
		binary.BigEndian.PutUint32(d[8:], uint32(bs))
		s.dataIn(itt, cut(d, int(binary.BigEndian.Uint32(cdb[10:]))), expected)
	case 0x1a: // mode sense (6): no pages, write protected
		s.dataIn(itt, cut([]byte{3, 0, 0x80, 0}, int(cdb[4])), expected)
	case 0x5a: // mode sense (10)
		s.dataIn(itt, cut([]byte{0, 6, 0, 0x80, 0, 0, 0, 0}, int(binary.BigEndian.Uint16(cdb[7:]))), expected)
	case 0x28, 0xa8, 0x88: // read (10), (12), (16)
		var lba uint64
		var n uint32
		switch cdb[0] {
		case 0x28:
			lba, n = uint64(binary.BigEndian.Uint32(cdb[2:])), uint32(binary.BigEndian.Uint16(cdb[7:]))
		case 0xa8:
			lba, n = uint64(binary.BigEndian.Uint32(cdb[2:])), binary.BigEndian.Uint32(cdb[6:])
		default:
			lba, n = binary.BigEndian.Uint64(cdb[2:]), binary.BigEndian.Uint32(cdb[10:])
		}
		if lba > blocks || uint64(n) > blocks-lba {
			s.respond(itt, statusCheckCondition, &senseOutOfRange, expected, 0)
			break
		}
		if uint64(n)*uint64(bs) > maxRead { // (the buffer for the whole read is made at once: without a limit one command could ask for the size of the disk)
			s.respond(itt, statusCheckCondition, &senseInvalidField, expected, 0)
			break
		}
		buf := make([]byte, int64(n)*bs)
		if got, err := s.disc.R.ReadAt(buf, int64(lba)*bs); got != len(buf) || (err != nil && !errors.Is(err, io.EOF)) {
			s.respond(itt, statusCheckCondition, &senseMediumError, expected, 0)
			break
		}
		s.dataIn(itt, buf, expected)
	case 0x0a, 0x2a, 0xaa, 0x8a: // writes
		s.respond(itt, statusCheckCondition, &senseWriteProtect, expected, 0)
		if p.flags()&0x20 != 0 && len(p.data) < int(expected) {
			// the data that would follow is not read: the connection cannot go on in step, so end it
			_ = s.w.Flush()
			return errors.New("a write was refused (the target is read-only)")
		}
	default:
		s.respond(itt, statusCheckCondition, &senseInvalidOpcode, expected, 0)
	}
	return nil
}

func cut(d []byte, n int) []byte {
	if n >= 0 && n < len(d) {
		return d[:n]
	}
	return d
}
