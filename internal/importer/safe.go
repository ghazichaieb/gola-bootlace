// Package importer downloads an ISO or a disk image from a web address into the library. The server fetches it itself, so the address is the one thing in the
// web interface that makes the server connect somewhere: a connection to anything on a local network is refused at the moment it is made (so that a redirect, a
// name that has several addresses, or one that changes its answer cannot get around the check), and only http and https are spoken.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package importer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func cidrs(list ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, s := range list {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// notPublic are the address ranges that are never fetched from: this network, private, shared (carrier-grade NAT), loopback, link-local (the cloud metadata
// address 169.254.169.254 is in it), the ranges kept for documents and tests, multicast and the reserved top of the space, and the IPv6 equivalents.
var notPublic = cidrs(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "100::/64", "2001::/32", "2001:db8::/32", "64:ff9b:1::/48", "fc00::/7", "fe80::/10", "ff00::/8",
)

var (
	nat64 = cidrs("64:ff9b::/96")[0] // carries an IPv4 address in its last 4 bytes
	sixTo = cidrs("2002::/16")[0]    // carries one in bytes 2 to 5
)

// PublicIP says whether an address is one that the server may connect to.
func PublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range notPublic {
		if n.Contains(ip) {
			return false
		}
	}
	if len(ip) == net.IPv6len {
		switch {
		case nat64.Contains(ip):
			return PublicIP(net.IP(ip[12:16]))
		case sixTo.Contains(ip):
			return PublicIP(net.IP(ip[2:6]))
		}
	}
	return true
}

// ParseURL checks an address that was typed: http or https, a host, no user name or password in it.
func ParseURL(raw string) (*url.URL, error) { return parseURL(raw, PublicIP) }

// parseURL is ParseURL with the test that says which addresses are fine.
func parseURL(raw string, allow func(net.IP) bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return nil, errors.New("give the address of the file (https://...)")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("that is not a web address (https://...)")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https addresses are fetched")
	}
	if u.User != nil {
		return nil, errors.New("an address with a user name or password in it is not accepted: a file that needs a login cannot be fetched here")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !allow(ip) {
		return nil, errors.New("that address is on a local network: files are not fetched from there (copy the file into the library folder instead)")
	}
	return u, nil
}

// errLocal is what a connection to a local address ends with.
var errLocal = errors.New("that address is on a local network: files are not fetched from there (copy the file into the library folder instead)")

// newClient makes the web client. allow says which addresses may be connected to (PublicIP, except in tests).
func newClient(allow func(net.IP) bool) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tr := &http.Transport{
		Proxy: nil, // never through a proxy that might reach somewhere else
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("%s has no address", host)
			}
			for _, a := range ips { // every address must be fine: a name that also points inside is not trusted
				if !allow(a.IP) {
					return nil, errLocal
				}
			}
			var last error
			for _, a := range ips {
				c, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port)) // (the address that was checked, not the name again)
				if err == nil {
					return c, nil
				}
				last = err
			}
			return nil, last
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("a redirect to something that is not a web address")
		}
		return nil
	}}
}
