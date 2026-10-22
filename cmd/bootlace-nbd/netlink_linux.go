//go:build linux

// The network block disk through the kernel's netlink interface (what nbd-client's -persist uses): the kernel keeps the device when the connection dies, holds what is read
// from it for a while, and tells this program, which connects again and gives the kernel the new connection. That keeps a live system going when its address changes after it
// has started (most live systems ask the DHCP server again once they are up, as a new client, and may get another address, which breaks the open connection).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/ghazichaieb/gola-bootlace/internal/nbd"
)

// generic netlink and the nbd family (linux/genetlink.h, linux/nbd-netlink.h)
const (
	nlmsgErrorType   = 2
	nlmRequest       = 1
	nlmAck           = 4
	nlaFNested       = 0x8000
	genlIDCtrl       = 0x10
	ctrlCmdGetFamily = 3

	ctrlAttrFamilyID    = 1
	ctrlAttrFamilyName  = 2
	ctrlAttrMcastGroups = 7
	ctrlAttrMcastName   = 1
	ctrlAttrMcastID     = 2

	nbdCmdConnect     = 1
	nbdCmdDisconnect  = 2
	nbdCmdReconfigure = 3
	nbdCmdLinkDead    = 4

	nbdAttrIndex       = 1
	nbdAttrSizeBytes   = 2
	nbdAttrBlockSize   = 3
	nbdAttrTimeout     = 4
	nbdAttrServerFlags = 5
	nbdAttrClientFlags = 6
	nbdAttrSockets     = 7
	nbdAttrDeadConn    = 8
	nbdSockItem        = 1
	nbdSockFD          = 1

	solNetlink    = 270
	netlinkAddMem = 1
)

func align4(n int) int { return (n + 3) &^ 3 }

// attr is one netlink attribute.
func attr(typ uint16, data []byte) []byte {
	b := make([]byte, align4(4+len(data)))
	binary.LittleEndian.PutUint16(b[0:], uint16(4+len(data)))
	binary.LittleEndian.PutUint16(b[2:], typ)
	copy(b[4:], data)
	return b
}

func attrU32(typ uint16, v uint32) []byte {
	d := make([]byte, 4)
	binary.LittleEndian.PutUint32(d, v)
	return attr(typ, d)
}

func attrU64(typ uint16, v uint64) []byte {
	d := make([]byte, 8)
	binary.LittleEndian.PutUint64(d, v)
	return attr(typ, d)
}

// attrs reads the attributes of a message body.
func attrs(b []byte) map[uint16][]byte {
	out := map[uint16][]byte{}
	for len(b) >= 4 {
		l := int(binary.LittleEndian.Uint16(b[0:]))
		t := binary.LittleEndian.Uint16(b[2:]) &^ 0xc000
		if l < 4 || l > len(b) {
			break
		}
		out[t] = b[4:l]
		b = b[align4(l):]
	}
	return out
}

type genl struct {
	fd  int
	seq uint32
}

func openGenl() (*genl, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_GENERIC)
	if err != nil {
		return nil, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &genl{fd: fd}, nil
}

// request sends one generic netlink message and waits for its answer (the data of a reply, or the acknowledgement).
func (g *genl) request(family uint16, cmd, version byte, body []byte) ([]byte, error) {
	g.seq++
	msg := make([]byte, 16+4+len(body))
	binary.LittleEndian.PutUint32(msg[0:], uint32(len(msg)))
	binary.LittleEndian.PutUint16(msg[4:], family)
	binary.LittleEndian.PutUint16(msg[6:], nlmRequest|nlmAck)
	binary.LittleEndian.PutUint32(msg[8:], g.seq)
	msg[16], msg[17] = cmd, version
	copy(msg[20:], body)
	if err := syscall.Sendto(g.fd, msg, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	var data []byte
	buf := make([]byte, 1<<16)
	for {
		n, _, err := syscall.Recvfrom(g.fd, buf, 0)
		if err != nil {
			return nil, err
		}
		b := buf[:n]
		for len(b) >= 16 {
			l := int(binary.LittleEndian.Uint32(b[0:]))
			if l < 16 || l > len(b) {
				return nil, errors.New("short netlink message")
			}
			typ := binary.LittleEndian.Uint16(b[4:])
			if binary.LittleEndian.Uint32(b[8:]) == g.seq {
				if typ == nlmsgErrorType {
					if e := int32(binary.LittleEndian.Uint32(b[16:])); e != 0 {
						return nil, syscall.Errno(-e)
					}
					return data, nil
				}
				if l > 20 {
					data = append([]byte(nil), b[20:l]...)
				}
			}
			b = b[align4(l):]
		}
	}
}

// family finds the nbd family's number and the group its events go to.
func (g *genl) family() (id uint16, group uint32, err error) {
	name := append([]byte("nbd"), 0)
	data, err := g.request(genlIDCtrl, ctrlCmdGetFamily, 1, attr(ctrlAttrFamilyName, name))
	if err != nil {
		return 0, 0, err
	}
	a := attrs(data)
	if v, ok := a[ctrlAttrFamilyID]; ok && len(v) >= 2 {
		id = binary.LittleEndian.Uint16(v)
	}
	if id == 0 {
		return 0, 0, errors.New("the kernel has no nbd netlink interface")
	}
	for _, grp := range attrs(a[ctrlAttrMcastGroups]) {
		ga := attrs(grp)
		if string(ga[ctrlAttrMcastName]) == "nbd_mc_group\x00" && len(ga[ctrlAttrMcastID]) >= 4 {
			group = binary.LittleEndian.Uint32(ga[ctrlAttrMcastID])
		}
	}
	return id, group, nil
}

func sockets(fd int) []byte {
	item := attr(nbdSockItem|nlaFNested, attrU32(nbdSockFD, uint32(fd)))
	return attr(nbdAttrSockets|nlaFNested, item)
}

// serveNetlink attaches the export to /dev/nbdN through netlink and keeps it connected for ever: when the kernel says the connection is dead, a new one is made and handed
// over. It returns an error only when netlink cannot be used at all (then the caller falls back to the older way).
func serveNetlink(host, port, name string, index int, c net.Conn, size uint64, flags uint64) error {
	g, err := openGenl()
	if err != nil {
		return err
	}
	fam, group, err := g.family()
	if err != nil {
		return err
	}
	tcpFile := func(c net.Conn) (*os.File, error) { return c.(*net.TCPConn).File() }
	f, err := tcpFile(c)
	if err != nil {
		return err
	}
	const block = 2048
	body := append([]byte{}, attrU32(nbdAttrIndex, uint32(index))...)
	body = append(body, attrU64(nbdAttrSizeBytes, size-size%block)...)
	body = append(body, attrU64(nbdAttrBlockSize, block)...)
	body = append(body, attrU64(nbdAttrTimeout, 30)...) // a request that is not answered in 30 s: the connection is taken as dead
	body = append(body, attrU64(nbdAttrServerFlags, flags)...)
	body = append(body, attrU64(nbdAttrClientFlags, 0)...)
	body = append(body, attrU64(nbdAttrDeadConn, 3600)...) // what is asked of the disk waits up to an hour for a new connection
	body = append(body, sockets(int(f.Fd()))...)
	if _, err := g.request(fam, nbdCmdConnect, 1, body); err != nil {
		f.Close()
		return fmt.Errorf("connect: %w", err)
	}
	f.Close()

	// the events: the kernel sends "link dead" to a group
	ev, err := openGenl()
	if err != nil {
		return nil // attached, just cannot reconnect
	}
	if group != 0 {
		if err := syscall.SetsockoptInt(ev.fd, solNetlink, netlinkAddMem, int(group)); err != nil {
			fmt.Fprintln(os.Stderr, "bootlace-nbd: cannot listen for lost connections:", err)
		}
	}
	buf := make([]byte, 1<<16)
	for {
		n, _, err := syscall.Recvfrom(ev.fd, buf, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			time.Sleep(time.Second)
			continue
		}
		b := buf[:n]
		for len(b) >= 20 {
			l := int(binary.LittleEndian.Uint32(b[0:]))
			if l < 20 || l > len(b) {
				break
			}
			if binary.LittleEndian.Uint16(b[4:]) == fam && b[16] == nbdCmdLinkDead {
				if v, ok := attrs(b[20:l])[nbdAttrIndex]; ok && len(v) >= 4 && int(binary.LittleEndian.Uint32(v)) == index {
					reconnect(g, fam, host, port, name, index, size)
				}
			}
			b = b[align4(l):]
		}
	}
}

// reconnect makes a new connection and gives it to the kernel, trying again until it works.
func reconnect(g *genl, fam uint16, host, port, name string, index int, size uint64) {
	for delay := time.Second; ; {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
		if err == nil {
			newSize, _, herr := nbd.Handshake(c, name)
			if herr == nil && uint64(newSize) == size {
				if f, ferr := c.(*net.TCPConn).File(); ferr == nil {
					body := append([]byte{}, attrU32(nbdAttrIndex, uint32(index))...)
					body = append(body, attrU64(nbdAttrDeadConn, 3600)...)
					body = append(body, sockets(int(f.Fd()))...)
					_, rerr := g.request(fam, nbdCmdReconfigure, 1, body)
					f.Close()
					if rerr == nil {
						fmt.Fprintln(os.Stderr, "bootlace-nbd: connected again")
						return
					}
					fmt.Fprintln(os.Stderr, "bootlace-nbd: reconfigure:", rerr)
				}
			}
			c.Close()
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay += time.Second
		}
	}
}

// disconnectNetlink is "nbd-client -d".
func disconnectNetlink(index int) error {
	g, err := openGenl()
	if err != nil {
		return err
	}
	fam, _, err := g.family()
	if err != nil {
		return err
	}
	_, err = g.request(fam, nbdCmdDisconnect, 1, attrU32(nbdAttrIndex, uint32(index)))
	return err
}

var _ = unsafe.Pointer(nil)
