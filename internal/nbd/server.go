// Package nbd is a read-only Network Block Device server: it shows an ISO file to a PC as a disk that is read over the network a piece at a time, so that the PC does
// not have to keep the ISO in memory (a live Linux that downloads the whole ISO needs RAM for all of it). The protocol is the "newstyle" one of
// https://github.com/NetworkBlockDevice/nbd/blob/master/doc/proto.md, with only what Linux's client needs: export names, reads, and no writes.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package nbd

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	magicInit    = 0x4e42444d41474943 // "NBDMAGIC"
	magicOpt     = 0x49484156454f5054 // "IHAVEOPT"
	magicReply   = 0x3e889045565a9
	magicRequest = 0x25609513
	magicResp    = 0x67446698

	flagFixedNewstyle = 1 << 0
	flagNoZeroes      = 1 << 1

	optExportName = 1
	optAbort      = 2
	optList       = 3
	optInfo       = 6
	optGo         = 7

	repAck       = 1
	repServer    = 2
	repInfo      = 3
	repErrUnsup  = 1<<31 | 1
	repErrUnknow = 1<<31 | 6

	cmdRead  = 0
	cmdWrite = 1
	cmdDisc  = 2
	cmdFlush = 3

	transHasFlags = 1 << 0
	transReadOnly = 1 << 1

	errPerm  = 1
	errIO    = 5
	errInval = 22

	maxRead = 32 << 20

	maxSessions   = 256              // clients at once
	handshakeTime = 60 * time.Second // the whole handshake; the connection then lives as long as the PC's disk does (no limit on a quiet one)
)

// Export is one thing that can be read: an ISO.
type Export struct {
	R    io.ReaderAt
	Size int64
	Done func() // called when the client goes away (closes the file)
}

// Server answers the clients that connect to a listener.
type Server struct {
	// Open finds the export of this name; false when there is none.
	Open func(name string) (Export, bool)
	// Names lists the export names (for NBD_OPT_LIST); may be nil.
	Names func() []string
	Logf  func(format string, args ...any)
}

// Serve accepts clients until the listener is closed.
func (s *Server) Serve(l net.Listener) error {
	slots := make(chan struct{}, maxSessions)
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		default: // (every client holds buffers and may hold reads of megabytes: a flood of connections must not take the server's memory)
			if s.Logf != nil {
				s.Logf("nbd: %s: refused, %d clients are connected already", c.RemoteAddr(), maxSessions)
			}
			c.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			defer c.Close()
			if err := s.handle(c); err != nil && !errors.Is(err, io.EOF) && s.Logf != nil {
				s.Logf("nbd: %s: %v", c.RemoteAddr(), err)
			}
		}()
	}
}

func (s *Server) handle(c net.Conn) error {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	c.SetDeadline(time.Now().Add(handshakeTime)) //nolint:errcheck // (a client that does not finish the handshake is dropped; transmit lifts the limit)
	r := bufio.NewReaderSize(c, 64<<10)
	var hello [18]byte
	binary.BigEndian.PutUint64(hello[0:], magicInit)
	binary.BigEndian.PutUint64(hello[8:], magicOpt)
	binary.BigEndian.PutUint16(hello[16:], flagFixedNewstyle|flagNoZeroes)
	if _, err := c.Write(hello[:]); err != nil {
		return err
	}
	var cf uint32
	if err := binary.Read(r, binary.BigEndian, &cf); err != nil {
		return err
	}
	noZeroes := cf&flagNoZeroes != 0
	reply := func(opt, typ uint32, data []byte) error {
		b := make([]byte, 20+len(data))
		binary.BigEndian.PutUint64(b[0:], magicReply)
		binary.BigEndian.PutUint32(b[8:], opt)
		binary.BigEndian.PutUint32(b[12:], typ)
		binary.BigEndian.PutUint32(b[16:], uint32(len(data)))
		copy(b[20:], data)
		_, err := c.Write(b)
		return err
	}
	var exp Export
	for {
		var h struct {
			Magic uint64
			Opt   uint32
			Len   uint32
		}
		if err := binary.Read(r, binary.BigEndian, &h); err != nil {
			return err
		}
		if h.Magic != magicOpt || h.Len > 1<<20 {
			return errors.New("bad option header")
		}
		data := make([]byte, h.Len)
		if _, err := io.ReadFull(r, data); err != nil {
			return err
		}
		switch h.Opt {
		case optExportName:
			e, ok := s.Open(string(data))
			if !ok {
				return errors.New("unknown export " + string(data))
			}
			exp = e
			var b [10 + 124]byte
			binary.BigEndian.PutUint64(b[0:], uint64(e.Size))
			binary.BigEndian.PutUint16(b[8:], transHasFlags|transReadOnly)
			n := 10
			if !noZeroes {
				n = len(b)
			}
			if _, err := c.Write(b[:n]); err != nil {
				e.done()
				return err
			}
			return s.transmit(c, r, exp)
		case optGo, optInfo:
			if len(data) < 4 {
				return errors.New("short GO option")
			}
			n := binary.BigEndian.Uint32(data)
			if int(n) > len(data)-4 {
				return errors.New("bad GO option")
			}
			e, ok := s.Open(string(data[4 : 4+n]))
			if !ok {
				if err := reply(h.Opt, repErrUnknow, nil); err != nil {
					return err
				}
				continue
			}
			var info [12]byte
			binary.BigEndian.PutUint16(info[0:], 0) // NBD_INFO_EXPORT
			binary.BigEndian.PutUint64(info[2:], uint64(e.Size))
			binary.BigEndian.PutUint16(info[10:], transHasFlags|transReadOnly)
			if err := reply(h.Opt, repInfo, info[:]); err != nil {
				e.done()
				return err
			}
			if err := reply(h.Opt, repAck, nil); err != nil {
				e.done()
				return err
			}
			if h.Opt == optGo {
				return s.transmit(c, r, e)
			}
			e.done()
		case optList:
			if s.Names != nil {
				for _, name := range s.Names() {
					b := make([]byte, 4+len(name))
					binary.BigEndian.PutUint32(b, uint32(len(name)))
					copy(b[4:], name)
					if err := reply(h.Opt, repServer, b); err != nil {
						return err
					}
				}
			}
			if err := reply(h.Opt, repAck, nil); err != nil {
				return err
			}
		case optAbort:
			_ = reply(h.Opt, repAck, nil)
			return nil
		default:
			if err := reply(h.Opt, repErrUnsup, nil); err != nil {
				return err
			}
		}
	}
}

func (e Export) done() {
	if e.Done != nil {
		e.Done()
	}
}

// transmit answers requests: they may be many at once, and the answers may go in any order.
func (s *Server) transmit(c net.Conn, r *bufio.Reader, e Export) error {
	defer e.done()
	c.SetDeadline(time.Time{}) //nolint:errcheck // (the handshake is over: a quiet disk is a disk nobody reads at the moment)
	var wmu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 64)
	defer wg.Wait()
	respond := func(handle uint64, errno uint32, data []byte) {
		b := make([]byte, 16, 16+len(data))
		binary.BigEndian.PutUint32(b[0:], magicResp)
		binary.BigEndian.PutUint32(b[4:], errno)
		binary.BigEndian.PutUint64(b[8:], handle)
		b = append(b, data...)
		wmu.Lock()
		_, _ = c.Write(b)
		wmu.Unlock()
	}
	for {
		var q struct {
			Magic  uint32
			Flags  uint16
			Type   uint16
			Handle uint64
			Offset uint64
			Length uint32
		}
		if err := binary.Read(r, binary.BigEndian, &q); err != nil {
			return err
		}
		if q.Magic != magicRequest {
			return errors.New("bad request header")
		}
		switch q.Type {
		case cmdRead:
			if e.Size < 0 || q.Offset > uint64(e.Size) || uint64(q.Length) > uint64(e.Size)-q.Offset || q.Length > maxRead {
				respond(q.Handle, errInval, nil)
				continue
			}
			slots <- struct{}{}
			wg.Add(1)
			go func(handle, off uint64, n uint32) {
				defer func() { <-slots; wg.Done() }()
				buf := make([]byte, n)
				if got, err := e.R.ReadAt(buf, int64(off)); got != len(buf) || (err != nil && !errors.Is(err, io.EOF)) {
					respond(handle, errIO, nil)
					return
				}
				respond(handle, 0, buf)
			}(q.Handle, q.Offset, q.Length)
		case cmdWrite:
			if _, err := io.CopyN(io.Discard, r, int64(q.Length)); err != nil {
				return err
			}
			respond(q.Handle, errPerm, nil)
		case cmdFlush:
			respond(q.Handle, 0, nil)
		case cmdDisc:
			return nil
		default:
			respond(q.Handle, errInval, nil)
		}
	}
}
