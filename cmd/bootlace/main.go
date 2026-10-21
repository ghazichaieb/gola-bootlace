// Gola Bootlace: a network boot server for ISO files.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
	"github.com/ghazichaieb/gola-bootlace/internal/tftp"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `Gola Bootlace: boots PCs over the network from the ISO files in a folder.

  bootlace serve [options]   start the server (answers PXE PCs, serves the boot menu and the ISO files)
  bootlace inspect <iso>...  say what is inside ISO files and how the server would start them
  bootlace -version          print the version

Run "bootlace serve -h" for the options.
`

// run is main without the process exit, so tests can call it.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "serve" {
		return serve(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "inspect" {
		return inspect(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("bootlace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}
	fmt.Fprint(stderr, usage)
	return 1
}

func serve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootlace serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	isoDir := fs.String("iso-dir", "isos", "the folder with the ISO files (looked at again for every menu)")
	ipFlag := fs.String("ip", "", "this server's address as the PCs reach it (default: the first private address of this computer)")
	httpPort := fs.Int("http-port", 8080, "port of the web server")
	mode := fs.String("dhcp", "proxy", `"proxy": answer PXE PCs only, next to the network's own DHCP server; "full": also hand out addresses (only on a network without a DHCP server); "off": no DHCP at all`)
	first := fs.String("range-start", "", "full mode: first address to hand out (default: .100 of this network)")
	last := fs.String("range-end", "", "full mode: last address to hand out (default: .200 of this network)")
	mask := fs.String("mask", "255.255.255.0", "full mode: the network mask")
	router := fs.String("router", "", "full mode: the router given to PCs (default: none)")
	dnsIP := fs.String("dns", "", "full mode: the DNS server given to PCs (default: none)")
	pxeOnly := fs.Bool("pxe-only", false, "full mode: hand out addresses to PCs that start a network boot only (a lab where this computer is also on the network)")
	menuDefault := fs.Int("menu-default", 0, "the menu entry (counting from 1) that starts by itself after -menu-timeout seconds (0: wait for a choice)")
	menuPick := fs.String("menu-pick", "", "the entry whose name contains this text starts by itself after -menu-timeout seconds (instead of -menu-default)")
	menuTimeout := fs.Int("menu-timeout", 0, "seconds before the default menu entry starts (0: wait for ever)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logf := func(format string, a ...any) {
		fmt.Fprintf(stdout, time.Now().Format("15:04:05")+"  "+format+"\n", a...)
	}
	ip := net.ParseIP(*ipFlag)
	if ip == nil {
		ip = privateIP()
	}
	if ip == nil {
		fmt.Fprintln(stderr, "bootlace: cannot find this computer's address: give it with -ip")
		return 1
	}
	base := fmt.Sprintf("http://%s:%d", ip, *httpPort)
	web := &httpd.Server{ISODir: *isoDir, Title: "Gola Bootlace", Base: base, Logf: logf, Menu: menu.Options{Default: *menuDefault, Timeout: *menuTimeout}, Pick: *menuPick}
	errs := make(chan error, 4)

	httpListener, err := net.Listen("tcp4", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: cannot open web port %d: %v\n", *httpPort, err)
		return 1
	}
	go func() { errs <- http.Serve(httpListener, web.Handler()) }()

	tftpConn, err := net.ListenPacket("udp4", ":69")
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: cannot open TFTP port 69: %v\n", err)
		return 1
	}
	go func() { errs <- (&tftp.Server{FS: assets.IPXE(), Logf: logf, LocalIP: ip}).Serve(tftpConn) }()

	if *mode != "off" {
		boot := dhcp.Boot{ServerIP: ip, Script: base + "/boot.ipxe", Files: map[dhcp.Arch]string{
			dhcp.ArchBIOS: "undionly.kpxe", dhcp.ArchX64EFI: "ipxe.efi",
		}}
		var responder dhcp.Responder = boot
		if *mode == "full" {
			m := net.IPMask(net.ParseIP(*mask).To4())
			if m == nil {
				fmt.Fprintf(stderr, "bootlace: %q is not a network mask\n", *mask)
				return 2
			}
			start, end := net.ParseIP(*first), net.ParseIP(*last)
			if start == nil || end == nil {
				network := ip.Mask(m).To4()
				start, end = net.IPv4(network[0], network[1], network[2], 100), net.IPv4(network[0], network[1], network[2], 200)
			}
			responder = &dhcp.Full{PXEOnly: *pxeOnly, Boot: boot, First: start, Last: end, Mask: m, Router: net.ParseIP(*router), DNS: net.ParseIP(*dnsIP), Lease: time.Hour}
			logf("DHCP: handing out %s to %s", start, end)
		} else if *mode != "proxy" {
			fmt.Fprintf(stderr, "bootlace: -dhcp must be proxy, full or off, not %q\n", *mode)
			return 2
		}
		for _, port := range []int{67, 4011} {
			conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: dhcpBindIP(ip), Port: port})
			if err != nil {
				fmt.Fprintf(stderr, "bootlace: cannot open DHCP port %d (another DHCP server on this computer?): %v\n", port, err)
				return 1
			}
			go func() { errs <- dhcp.Serve(conn, responder, logf) }()
		}
	}
	logf("Gola Bootlace %s: menu at %s/boot.ipxe, ISO folder %q, DHCP mode %s", version.String(), base, *isoDir, *mode)
	if err := <-errs; err != nil {
		fmt.Fprintf(stderr, "bootlace: %v\n", err)
		return 1
	}
	return 0
}

// dhcpBindIP is the address the DHCP sockets bind to. On Windows a socket bound to this server's own address still hears the broadcasts that arrive on
// that network, and its answers leave on it (a socket bound to every address sends a broadcast out of whichever network Windows prefers, and a PC
// with a second network card never gets the answer). Elsewhere the sockets hear every network.
func dhcpBindIP(server net.IP) net.IP {
	if runtime.GOOS == "windows" {
		return server
	}
	return nil
}

// privateIP is the first private IPv4 address of this computer (the network the PCs are on).
func privateIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.To4()
		}
	}
	return nil
}
