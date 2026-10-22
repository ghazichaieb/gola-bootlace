//go:build linux

// bootlace-nbd runs inside a PC's initramfs (Bootlace puts it there for a live Linux): it connects to Bootlace's NBD server and hands the connection to the kernel's
// network block device, so that the ISO appears as /dev/nbd0 and is read over the network a piece at a time instead of being copied into memory.
// It stays running for as long as the disk is in use.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/ghazichaieb/gola-bootlace/internal/nbd"
)

// the ioctls of the kernel's nbd driver (linux/nbd.h): _IO(0xab, n)
const (
	setSock       = 0xab00
	setBlksize    = 0xab01
	doIt          = 0xab03
	clearSock     = 0xab04
	setSizeBlocks = 0xab07
	setTimeout    = 0xab09
	setFlags      = 0xab0a
)

func ioctl(fd uintptr, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

func main() {
	host := flag.String("host", "", "the Bootlace server")
	port := flag.Int("port", 10809, "its NBD port")
	name := flag.String("name", "", "the export (the ISO's id)")
	dev := flag.String("dev", "/dev/nbd0", "the network block device")
	flag.Parse()
	if *host == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "bootlace-nbd: -host and -name are needed")
		os.Exit(2)
	}
	c, err := net.Dial("tcp", net.JoinHostPort(*host, fmt.Sprint(*port)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		os.Exit(1)
	}
	size, flags, err := nbd.Handshake(c, *name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		os.Exit(1)
	}
	const block = 2048 // an ISO's block size
	f, err := os.OpenFile(*dev, os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		os.Exit(1)
	}
	sock, err := c.(*net.TCPConn).File()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		os.Exit(1)
	}
	fd := f.Fd()
	for _, step := range []struct {
		name     string
		req, arg uintptr
	}{
		{"SET_BLKSIZE", setBlksize, block},
		{"SET_SIZE_BLOCKS", setSizeBlocks, uintptr(size / block)},
		{"CLEAR_SOCK", clearSock, 0},
		{"SET_SOCK", setSock, sock.Fd()},
		{"SET_FLAGS", setFlags, uintptr(flags)},
		{"SET_TIMEOUT", setTimeout, 60},
	} {
		if err := ioctl(fd, step.req, step.arg); err != nil {
			fmt.Fprintf(os.Stderr, "bootlace-nbd: %s: %v\n", step.name, err)
			os.Exit(1)
		}
	}
	// this call returns only when the disk is let go
	if err := ioctl(fd, doIt, 0); err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd: DO_IT:", err)
	}
	_ = ioctl(fd, clearSock, 0)
}
