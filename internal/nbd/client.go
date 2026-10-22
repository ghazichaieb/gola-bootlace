// The client half of the handshake, shared by the helper that runs in a PC's initramfs (cmd/bootlace-nbd) and the tests.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package nbd

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// Handshake does the newstyle handshake on c for the named export and returns its size and the transmission flags; the connection is then ready for requests.
func Handshake(c net.Conn, name string) (size int64, flags uint16, err error) {
	var hello [18]byte
	if _, err = io.ReadFull(c, hello[:]); err != nil {
		return 0, 0, err
	}
	if binary.BigEndian.Uint64(hello[0:]) != magicInit || binary.BigEndian.Uint64(hello[8:]) != magicOpt {
		return 0, 0, errors.New("not an NBD server")
	}
	srv := binary.BigEndian.Uint16(hello[16:])
	if srv&flagFixedNewstyle == 0 {
		return 0, 0, errors.New("the server is too old (no fixed newstyle handshake)")
	}
	var cf uint32 = flagFixedNewstyle
	if srv&flagNoZeroes != 0 {
		cf |= flagNoZeroes
	}
	b := make([]byte, 4+16+len(name))
	binary.BigEndian.PutUint32(b[0:], cf)
	binary.BigEndian.PutUint64(b[4:], magicOpt)
	binary.BigEndian.PutUint32(b[12:], optExportName)
	binary.BigEndian.PutUint32(b[16:], uint32(len(name)))
	copy(b[20:], name)
	if _, err = c.Write(b); err != nil {
		return 0, 0, err
	}
	var rep [10]byte
	if _, err = io.ReadFull(c, rep[:]); err != nil {
		return 0, 0, err // an unknown export closes the connection
	}
	if cf&flagNoZeroes == 0 {
		var zeroes [124]byte
		if _, err = io.ReadFull(c, zeroes[:]); err != nil {
			return 0, 0, err
		}
	}
	return int64(binary.BigEndian.Uint64(rep[0:])), binary.BigEndian.Uint16(rep[8:]), nil
}

// Read asks for n bytes at offset on a connection that has finished its handshake (the tests use it; the kernel does this itself in the PC).
func Read(c net.Conn, handle uint64, offset uint64, n uint32) ([]byte, error) {
	var q [28]byte
	binary.BigEndian.PutUint32(q[0:], magicRequest)
	binary.BigEndian.PutUint16(q[6:], cmdRead)
	binary.BigEndian.PutUint64(q[8:], handle)
	binary.BigEndian.PutUint64(q[16:], offset)
	binary.BigEndian.PutUint32(q[24:], n)
	if _, err := c.Write(q[:]); err != nil {
		return nil, err
	}
	var h [16]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(h[0:]) != magicResp || binary.BigEndian.Uint64(h[8:]) != handle {
		return nil, errors.New("bad reply")
	}
	if e := binary.BigEndian.Uint32(h[4:]); e != 0 {
		return nil, errors.New("the server refused the read")
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(c, buf)
	return buf, err
}
