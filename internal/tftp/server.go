// Package tftp is a small read-only TFTP server: PXE firmware fetches its first boot file (iPXE) with it. Writing is refused.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package tftp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	opRRQ   = 1
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6

	errNotFound  = 1
	errAccess    = 2
	errIllegalOp = 4

	defaultBlock = 512
	minBlock     = 8
	maxBlock     = 65464
	maxWindow    = 16
)

// Server answers read requests from a file system (the folder with the boot files).
type Server struct {
	FS      fs.FS
	Timeout time.Duration // how long to wait for the client's answer before sending again (default 2 s)
	Retries int           // how many times to send again before giving up (default 5)
	Logf    func(format string, args ...any)
	// Lookup is asked first for every file a client wants: it can answer from something that depends on who is asking (FreeBSD's loader reads its
	// kernel and files by TFTP, from the ISO that client chose). When it has no answer the files of FS are served.
	Lookup func(client net.IP, name string) (fs.File, bool)
	// LocalIP is this server's address as the clients reach it. Answers leave from it: on a computer with several networks the operating system
	// might otherwise answer from another network's address, which a PC's firmware rejects.
	LocalIP net.IP
	// Allow, when set, is asked about every client: a request from an address it refuses is not answered.
	Allow func(net.Addr) bool
	// Window, when set, says how many blocks to send ahead of the acknowledgements to a client that did not ask for a window size itself (RFC 7440) and is known to
	// receive them in order and to acknowledge each one: FreeBSD's loader, which otherwise needs a whole round trip through the firmware's network driver for every
	// 1428 bytes. 1 or less sends one block and waits, as the protocol says.
	Window func(client net.IP, name string) int
	// MaxBlock, when set, is asked for every transfer and gives the largest block size to agree to (0: whatever the client asks for). Some switches and firmware only
	// work with small blocks.
	MaxBlock func() int
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Serve reads requests from conn until it is closed. Every transfer runs on its own socket, as the protocol wants.
func (s *Server) Serve(conn net.PacketConn) error {
	buf := make([]byte, 1500)
	for {
		n, client, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if s.Allow != nil && !s.Allow(client) {
			continue
		}
		request := append([]byte(nil), buf[:n]...)
		if n >= 2 && binary.BigEndian.Uint16(request) == opRRQ {
			s.logf("tftp: read request from %v: %q", client, strings.ReplaceAll(string(request[2:]), "\x00", " "))
		}
		go s.handle(conn.LocalAddr(), client, request)
	}
}

func (s *Server) handle(local net.Addr, client net.Addr, packet []byte) {
	if len(packet) < 4 {
		return
	}
	host, _, _ := net.SplitHostPort(local.String())
	if s.LocalIP != nil {
		host = s.LocalIP.String()
	}
	conn, err := net.ListenPacket("udp", net.JoinHostPort(host, "0"))
	if err != nil {
		s.logf("tftp: cannot open a transfer socket: %v", err)
		return
	}
	defer conn.Close()
	switch binary.BigEndian.Uint16(packet) {
	case opRRQ:
		if err := s.read(conn, client, packet[2:]); err != nil {
			s.logf("tftp: %v: %v", client, err)
		}
	case opWRQ:
		sendError(conn, client, errAccess, "this server is read-only")
	default:
		sendError(conn, client, errIllegalOp, "expected a read request")
	}
}

// request is what a read request says: file, mode and options (names in lower case).
type request struct {
	file    string
	mode    string
	options map[string]string
}

func parseRequest(b []byte) (request, error) {
	fields := strings.Split(string(b), "\x00")
	if len(fields) < 3 || fields[0] == "" {
		return request{}, errors.New("malformed request")
	}
	r := request{file: fields[0], mode: strings.ToLower(fields[1]), options: map[string]string{}}
	rest := fields[2:]
	for i := 0; i+1 < len(rest); i += 2 {
		r.options[strings.ToLower(rest[i])] = rest[i+1]
	}
	return r, nil
}

func (s *Server) read(conn net.PacketConn, client net.Addr, raw []byte) error {
	req, err := parseRequest(raw)
	if err != nil {
		sendError(conn, client, errIllegalOp, err.Error())
		return err
	}
	name := path.Clean("/" + strings.ReplaceAll(req.file, "\\", "/"))[1:]
	if name == "" || !fs.ValidPath(name) {
		sendError(conn, client, errNotFound, "file not found")
		return fmt.Errorf("refused the path %q", req.file)
	}
	var f fs.File
	if s.Lookup != nil {
		if udp, ok := client.(*net.UDPAddr); ok {
			if lf, ok := s.Lookup(udp.IP, name); ok {
				f = lf
			}
		}
	}
	if f == nil {
		var err error
		if f, err = s.FS.Open(name); err != nil {
			sendError(conn, client, errNotFound, "file not found")
			return fmt.Errorf("%s: not found", name)
		}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		sendError(conn, client, errNotFound, "file not found")
		return fmt.Errorf("%s: not a file", name)
	}
	s.logf("tftp: %v asks for %s (options %v), %d bytes", client, name, req.options, info.Size())
	block, oack := defaultBlock, []string{}
	if v, ok := req.options["blksize"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			block = min(max(n, minBlock), maxBlock)
			if s.MaxBlock != nil {
				if limit := s.MaxBlock(); limit >= minBlock {
					block = min(block, limit)
				}
			}
			oack = append(oack, "blksize", strconv.Itoa(block))
		}
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if _, ok := req.options["tsize"]; ok {
		oack = append(oack, "tsize", strconv.FormatInt(info.Size(), 10))
	}
	retries := s.Retries
	if retries <= 0 {
		retries = 5
	}
	t := &transfer{conn: conn, client: client, timeout: timeout, retries: retries}
	if len(oack) > 0 { // options: the client confirms them with ACK 0 before the data starts
		if err := t.send(oackPacket(oack), 0); err != nil {
			return err
		}
	}
	window := 1
	if _, asked := req.options["windowsize"]; !asked && s.Window != nil {
		if udp, ok := client.(*net.UDPAddr); ok {
			window = s.Window(udp.IP, name)
		}
	}
	started := time.Now()
	if window > 1 {
		err = t.dataWindow(f, block, min(window, maxWindow))
	} else {
		err = t.data(f, block)
	}
	if took := time.Since(started); err == nil && info.Size() >= 4<<20 && took > 0 { // how fast a large file went: the way to see whether a setting helps
		s.logf("tftp: %v got %s: %d bytes in %.1f s (%.1f MB/s, %d blocks ahead)", client, name, info.Size(), took.Seconds(), float64(info.Size())/1e6/took.Seconds(), max(window, 1))
	}
	return err
}

type transfer struct {
	conn    net.PacketConn
	client  net.Addr
	timeout time.Duration
	retries int
}

// send writes a packet and waits for the ACK of `block`, writing again when the answer does not come.
func (t *transfer) send(packet []byte, block uint16) error {
	buf := make([]byte, 1500)
	for attempt := 0; attempt <= t.retries; attempt++ {
		if _, err := t.conn.WriteTo(packet, t.client); err != nil {
			return err
		}
		deadline := time.Now().Add(t.timeout)
		for time.Now().Before(deadline) {
			_ = t.conn.SetReadDeadline(deadline)
			n, from, err := t.conn.ReadFrom(buf)
			if err != nil {
				break // timeout: send again
			}
			if from.String() != t.client.String() {
				sendError(t.conn, from, 5, "unknown transfer ID") // somebody else: tell them, keep waiting for the real client
				continue
			}
			if n >= 4 && binary.BigEndian.Uint16(buf) == opACK && binary.BigEndian.Uint16(buf[2:]) == block {
				return nil
			}
			if n >= 2 && binary.BigEndian.Uint16(buf) == opERROR {
				return errors.New("the client stopped the transfer")
			}
		}
	}
	return errors.New("the client stopped answering")
}

// data sends the file in blocks of `size` bytes; the last block is shorter than the others (empty when the size is an exact multiple).
func (t *transfer) data(r io.Reader, size int) error {
	chunk := make([]byte, size)
	var block uint16 = 1
	for {
		n, err := io.ReadFull(r, chunk)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return err
		}
		packet := make([]byte, 4+n)
		binary.BigEndian.PutUint16(packet, opDATA)
		binary.BigEndian.PutUint16(packet[2:], block)
		copy(packet[4:], chunk[:n])
		if err := t.send(packet, block); err != nil {
			return err
		}
		if n < size {
			return nil
		}
		block++ // wraps from 65535 to 0, as clients expect for files longer than 65535 blocks
	}
}

func oackPacket(pairs []string) []byte {
	b := []byte{0, opOACK}
	for _, s := range pairs {
		b = append(append(b, s...), 0)
	}
	return b
}

func sendError(conn net.PacketConn, to net.Addr, code uint16, message string) {
	b := []byte{0, opERROR, byte(code >> 8), byte(code)}
	b = append(append(b, message...), 0)
	_, _ = conn.WriteTo(b, to)
}

// dataWindow is data with a number of blocks sent before the first is acknowledged. The client's answers move the window along; a timeout, or an answer for a block
// that was acknowledged before (the client is missing the one after it), sends the blocks that are not acknowledged again, oldest first.
func (t *transfer) dataWindow(r io.Reader, size, window int) error {
	type sent struct {
		block uint16
		data  []byte
	}
	chunk := make([]byte, size)
	buf := make([]byte, 1500)
	var inflight []sent
	var next uint16 = 1
	eof := false
	tries := 0
	var lastAgain time.Time
	again := func() error {
		lastAgain = time.Now()
		for _, p := range inflight {
			if _, err := t.conn.WriteTo(p.data, t.client); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		for !eof && len(inflight) < window {
			n, err := io.ReadFull(r, chunk)
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
				return err
			}
			packet := make([]byte, 4+n)
			binary.BigEndian.PutUint16(packet, opDATA)
			binary.BigEndian.PutUint16(packet[2:], next)
			copy(packet[4:], chunk[:n])
			if n < size {
				eof = true
			}
			inflight = append(inflight, sent{next, packet})
			next++ // wraps from 65535 to 0, as clients expect for files longer than 65535 blocks
			if _, err := t.conn.WriteTo(packet, t.client); err != nil {
				return err
			}
		}
		if len(inflight) == 0 {
			return nil
		}
		_ = t.conn.SetReadDeadline(time.Now().Add(t.timeout))
		n, from, err := t.conn.ReadFrom(buf)
		if err != nil { // no answer in time: all that is not acknowledged goes out again
			if tries++; tries > t.retries {
				return errors.New("the client stopped answering")
			}
			if err := again(); err != nil {
				return err
			}
			continue
		}
		if from.String() != t.client.String() {
			sendError(t.conn, from, 5, "unknown transfer ID")
			continue
		}
		if n >= 2 && binary.BigEndian.Uint16(buf) == opERROR {
			return errors.New("the client stopped the transfer")
		}
		if n < 4 || binary.BigEndian.Uint16(buf) != opACK {
			continue
		}
		ack := binary.BigEndian.Uint16(buf[2:])
		found := -1
		for i, p := range inflight {
			if p.block == ack {
				found = i
				break
			}
		}
		if found >= 0 {
			inflight = inflight[found+1:]
			tries = 0
			continue
		}
		// an answer for a block that was acknowledged already: the client waits for the block after it, which did not arrive. Not on every such answer, or the
		// blocks that are already on their way would be sent again and again.
		if time.Since(lastAgain) > 50*time.Millisecond {
			if err := again(); err != nil {
				return err
			}
		}
	}
}
