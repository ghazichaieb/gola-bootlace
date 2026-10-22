//go:build linux

// bootlace-nbd runs inside a PC's initramfs (Bootlace puts it there for a live Linux): it connects to Bootlace's NBD server and hands the connection to the kernel's
// network block device, so that the ISO appears as /dev/nbd0 and is read over the network a piece at a time instead of being copied into memory.
// It stays running for as long as the disk is in use, and makes the connection again when it is lost (see netlink_linux.go).
// Named nbd-client it takes that program's command line (Arch's start-up disk calls it that way), and "insmod" loads kernel modules (Proxmox's start-up disk has no insmod).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

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

// insmod loads kernel modules from files, in the order given. A module that is loaded already or that does not suit this PC is skipped.
func insmod(files []string) {
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bootlace-nbd insmod:", err)
			continue
		}
		params, _ := syscall.BytePtrFromString("")
		_, _, e := syscall.Syscall(syscall.SYS_INIT_MODULE, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(params)))
		if e != 0 && e != syscall.EEXIST {
			fmt.Fprintf(os.Stderr, "bootlace-nbd insmod: %s: %v\n", name, e)
		}
	}
}

// deviceIndex is the number of /dev/nbdN.
func deviceIndex(dev string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(dev), "nbd"))
	if err != nil {
		return 0
	}
	return n
}

func main() {
	self := strings.TrimPrefix(filepath.Base(os.Args[0]), "@")
	if self == "nbd-client" {
		os.Exit(nbdClient(os.Args[1:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "insmod" {
		insmod(os.Args[2:])
		return
	}
	host := flag.String("host", "", "the Bootlace server")
	port := flag.Int("port", 10809, "its NBD port")
	name := flag.String("name", "", "the export (the ISO's id)")
	dev := flag.String("dev", "/dev/nbd0", "the network block device")
	block := flag.Int("block", 2048, "the block size of the disk: 2048 for an ISO, 512 for a disk image's partition")
	flag.Parse()
	if *host == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "bootlace-nbd: -host and -name are needed")
		os.Exit(2)
	}
	os.Exit(serve(*host, strconv.Itoa(*port), *name, *dev, *block))
}

// nbdClient is the command line of nbd-client: "nbd-client host [port] [-N name] [options] /dev/nbdN" and "nbd-client -d /dev/nbdN". It attaches the disk, leaves a copy of this
// program running in the background to keep it connected, and returns when the device is there (as nbd-client does).
func nbdClient(args []string) int {
	var host, port, name, dev string
	port, block := "10809", "2048"
	disconnect := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d" || a == "-disconnect":
			disconnect = true
		case a == "-N" || a == "-name":
			if i+1 < len(args) {
				i++
				name = args[i]
			}
		case a == "-b" || a == "-block-size":
			if i+1 < len(args) {
				i++
				block = args[i]
			}
		case a == "-timeout" || a == "-C" || a == "-connections" || a == "-t":
			i++ // their values are not needed here
		case strings.HasPrefix(a, "-"):
			// -persist, -systemd-mark, -nofork ...: this program always stays connected
		case strings.HasPrefix(a, "/dev/"):
			dev = a
		case host == "":
			host = a
		default:
			port = a
		}
	}
	if dev == "" {
		fmt.Fprintln(os.Stderr, "nbd-client: no device given")
		return 1
	}
	if disconnect {
		_ = disconnectNetlink(deviceIndex(dev))
		return 0
	}
	if host == "" {
		fmt.Fprintln(os.Stderr, "nbd-client: no server given")
		return 1
	}
	cmd := exec.Command("/proc/self/exe", "-host", host, "-port", port, "-name", name, "-dev", dev, "-block", block)
	cmd.Args[0] = "bootlace-nbd"
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "nbd-client:", err)
		return 1
	}
	sizeFile := fmt.Sprintf("/sys/block/nbd%d/size", deviceIndex(dev))
	for i := 0; i < 160; i++ {
		if b, err := os.ReadFile(sizeFile); err == nil && strings.TrimSpace(string(b)) != "0" {
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "nbd-client: the network disk did not come up")
	return 1
}

// serve attaches the export and stays attached; it returns only when that is over or failed.
func serve(host, port, name, dev string, block int) int {
	c, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		return 1
	}
	size, flags, err := nbd.Handshake(c, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		return 1
	}
	// the usual way: through netlink, which can connect again when the connection is lost
	err = serveNetlink(host, port, name, deviceIndex(dev), c, uint64(size), uint64(flags), uint64(block))
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, "bootlace-nbd: netlink not used:", err)
	return serveIoctl(c, dev, size, flags, block)
}

// serveIoctl is the older way (it cannot connect again).
func serveIoctl(c net.Conn, dev string, size int64, flags uint16, blockSize int) int {
	block := uintptr(blockSize)
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		return 1
	}
	sock, err := c.(*net.TCPConn).File()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd:", err)
		return 1
	}
	fd := f.Fd()
	for _, step := range []struct {
		name     string
		req, arg uintptr
	}{
		{"SET_BLKSIZE", setBlksize, block},
		{"SET_SIZE_BLOCKS", setSizeBlocks, uintptr(size) / block},
		{"CLEAR_SOCK", clearSock, 0},
		{"SET_SOCK", setSock, sock.Fd()},
		{"SET_FLAGS", setFlags, uintptr(flags)},
		{"SET_TIMEOUT", setTimeout, 60},
	} {
		if err := ioctl(fd, step.req, step.arg); err != nil {
			fmt.Fprintf(os.Stderr, "bootlace-nbd: %s: %v\n", step.name, err)
			return 1
		}
	}
	// this call returns only when the disk is let go
	if err := ioctl(fd, doIt, 0); err != nil {
		fmt.Fprintln(os.Stderr, "bootlace-nbd: DO_IT:", err)
	}
	_ = ioctl(fd, clearSock, 0)
	return 0
}
