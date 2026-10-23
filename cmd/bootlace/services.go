// The services PCs talk to, as the supervisor starts and stops them (package service). The web port itself is not one of them: it stays open so that the web interface can
// start the others again.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
	"github.com/ghazichaieb/gola-bootlace/internal/smbshare"
	"github.com/ghazichaieb/gola-bootlace/internal/tftp"
)

// serverPlan is what the services need to know to start.
type serverPlan struct {
	ip                   net.IP
	web                  *httpd.Server
	lib                  *library.Library
	httpPort, smbPort    int
	nbdPort, iscsiPort   int
	smbUser, smbPassword string
	smbForward           string
	debugSMB             bool
	dhcp                 dhcp.Responder      // nil: no DHCP at all
	allow                func(net.Addr) bool // which addresses are answered (the local network, and what the settings add)
	logf                 func(format string, args ...any)
}

// addServices registers the services in the order they start (they stop in the opposite order, so that DHCP is the first to go and the last to come).
func addServices(sup *service.Supervisor, p serverPlan) {
	sup.Add(service.Def{ID: "menu", Title: "Boot menu and ISO files", Detail: fmt.Sprintf("web port %d", p.httpPort), Start: func() (service.Instance, error) {
		return service.Instance{Stop: func() {}, Sessions: p.web.Sessions}, nil
	}})
	sup.Add(service.Def{ID: "smb", Title: "Windows file share", Detail: fmt.Sprintf("TCP %d", p.smbPort), Start: p.startSMB})
	if p.iscsiPort != 0 {
		sup.Add(service.Def{ID: "iscsi", Title: "iSCSI target", Detail: fmt.Sprintf("TCP %d", p.iscsiPort), Start: func() (service.Instance, error) {
			return p.startTCP(p.iscsiPort, "iSCSI target (an ISO that is not started piece by piece is read from it as a CD-ROM)", p.web.ServeISCSI)
		}})
	}
	if p.nbdPort != 0 {
		sup.Add(service.Def{ID: "nbd", Title: "Network disk", Detail: fmt.Sprintf("TCP %d", p.nbdPort), Start: func() (service.Instance, error) {
			return p.startTCP(p.nbdPort, "network disk server (a live Linux reads its ISO from it instead of copying it into memory)", p.web.ServeNBD)
		}})
	}
	sup.Add(service.Def{ID: "tftp", Title: "TFTP", Detail: "UDP 69", Start: p.startTFTP})
	if p.dhcp != nil {
		sup.Add(service.Def{ID: "dhcp", Title: "PXE (proxy DHCP)", Detail: "UDP 67, 4011", Start: p.startDHCP})
	}
}

// startTCP serves a block-disk protocol (the network disk, iSCSI) on this server's address, and remembers its connections so that stopping closes them.
func (p serverPlan) startTCP(port int, what string, serve func(net.Listener) error) (service.Instance, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", p.ip, port))
	if err != nil {
		return service.Instance{}, fmt.Errorf("cannot open port %d: %w", port, err)
	}
	tl := service.Track(l)
	tl.Allow = p.allow
	p.logf("%s on %s:%d", what, p.ip, port)
	go func() {
		if err := serve(tl); err != nil && !errors.Is(err, net.ErrClosed) {
			p.logf("port %d: %v", port, err)
		}
	}()
	return service.Instance{Stop: tl.Shutdown, Sessions: tl.Sessions}, nil
}

func (p serverPlan) startSMB() (service.Instance, error) {
	dir := p.lib.Path()
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", p.ip, p.smbPort))
	if err != nil {
		return service.Instance{}, fmt.Errorf("cannot open port %d (is the computer's own file sharing using it? see -smb-port): %w", p.smbPort, err)
	}
	tl := service.Track(l)
	tl.Allow = p.allow
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := smbshare.Serve(ctx, smbshare.Options{Addr: l.Addr().String(), Listener: tl, Forward: p.smbForward, ISODir: dir, User: p.smbUser, Password: p.smbPassword, Logf: p.logf, Debug: p.debugSMB})
		if err != nil && ctx.Err() == nil {
			p.logf("Windows file share: %v", err)
		}
	}()
	p.logf(`Windows set-up share: \\%s\%s on port %d`, p.ip, smbshare.ShareName, p.smbPort)
	return service.Instance{Sessions: tl.Sessions, Stop: func() {
		cancel()
		tl.Shutdown()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}}, nil
}

func (p serverPlan) startTFTP() (service.Instance, error) {
	conn, err := net.ListenPacket("udp4", ":69")
	if err != nil {
		return service.Instance{}, fmt.Errorf("cannot open port 69: %w", err)
	}
	go func() {
		if err := (&tftp.Server{FS: assets.IPXE(), Logf: p.logf, LocalIP: p.ip, Lookup: p.web.TFTPLookup, Allow: p.allow}).Serve(conn); err != nil {
			p.logf("TFTP: %v", err)
		}
	}()
	return service.Instance{Stop: func() { _ = conn.Close() }}, nil
}

func (p serverPlan) startDHCP() (service.Instance, error) {
	var conns []*net.UDPConn
	closeAll := func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	for _, port := range []int{67, 4011} {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: dhcpBindIP(p.ip), Port: port})
		if err != nil {
			closeAll()
			return service.Instance{}, fmt.Errorf("cannot open DHCP port %d (another DHCP server on this computer?): %w", port, err)
		}
		conns = append(conns, conn)
		go func() {
			if err := dhcp.ServeFiltered(conn, p.dhcp, p.logf, p.allow); err != nil {
				p.logf("DHCP: %v", err)
			}
		}()
	}
	return service.Instance{Stop: closeAll}, nil
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
