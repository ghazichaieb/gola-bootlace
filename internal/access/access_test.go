// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package access

import (
	"net"
	"strings"
	"testing"
)

func TestLocalAddressesAreAnsweredByDefault(t *testing.T) {
	p := New(nil)
	for _, s := range []string{"10.1.2.3", "172.16.0.9", "172.31.255.254", "192.168.1.2", "127.0.0.1", "169.254.7.7", "::1", "fe80::1", "fd12:3456::1", "0.0.0.0", "::ffff:192.168.1.5"} {
		if !p.Allowed(net.ParseIP(s)) {
			t.Errorf("%s was refused", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "172.15.255.255", "100.64.0.1", "169.253.0.1", "2001:db8::1", "2606:4700::1", "::ffff:8.8.8.8"} {
		if p.Allowed(net.ParseIP(s)) {
			t.Errorf("%s was answered", s)
		}
	}
	if p.Allowed(nil) {
		t.Error("no address at all was answered")
	}
}

func TestExtraNetworksAndFromAnywhere(t *testing.T) {
	p := New(nil)
	if err := p.Set([]string{"100.64.0.0/10", "203.0.113.7", " "}, false); err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"100.100.1.1": true, "203.0.113.7": true, "203.0.113.8": false, "8.8.8.8": false} {
		if got := p.Allowed(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
	if err := p.Set(nil, true); err != nil || !p.Allowed(net.ParseIP("8.8.8.8")) {
		t.Fatal("from anywhere did not open the server")
	}
	if err := p.Set(nil, false); err != nil || p.Allowed(net.ParseIP("8.8.8.8")) {
		t.Fatal("switching it off did not close the server")
	}
}

func TestAWideNetworkIsNotAWayToSwitchTheProtectionOff(t *testing.T) {
	p := New(nil)
	for _, bad := range []string{"0.0.0.0/0", "::/0", "64.0.0.0/2", "8.0.0.0/7", "2000::/3", "nonsense", "1.2.3.4/33", "999.1.1.1"} {
		if err := p.Set([]string{bad}, false); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if err := p.Set([]string{"8.0.0.0/8", "2a00::/16"}, false); err != nil {
		t.Errorf("a /8 and a /16 are allowed: %v", err)
	}
	// a bad list must change nothing
	_ = p.Set([]string{"192.0.2.0/24"}, false)
	if err := p.Set([]string{"0.0.0.0/0"}, true); err == nil {
		t.Fatal("accepted")
	}
	if st := p.Stats(); len(st.Networks) != 1 || st.AllowPublic {
		t.Fatalf("a refused change was applied: %+v", st)
	}
}

func TestRefusalsAreCountedAndLoggedOncePerAddress(t *testing.T) {
	var lines []string
	p := New(func(f string, a ...any) { lines = append(lines, f) })
	for i := 0; i < 5; i++ {
		if p.AllowedAddr(&net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 1000 + i}) {
			t.Fatal("answered")
		}
	}
	p.AllowedAddr(&net.UDPAddr{IP: net.ParseIP("9.9.9.9")})
	if !p.AllowedAddr(&net.TCPAddr{IP: net.ParseIP("192.168.1.2")}) {
		t.Fatal("a local address was refused")
	}
	st := p.Stats()
	if st.Refused != 6 || len(st.Recent) != 2 {
		t.Fatalf("%+v", st)
	}
	if len(lines) != 2 {
		t.Fatalf("a flood must not fill the log: %d lines", len(lines))
	}
}

func TestServerAddress(t *testing.T) {
	if ServerAddress(net.ParseIP("192.168.1.1")) != nil || ServerAddress(nil) != nil {
		t.Error("a local address was flagged")
	}
	if err := ServerAddress(net.ParseIP("93.184.216.34")); err == nil || !strings.Contains(err.Error(), "public") {
		t.Errorf("a public address was not flagged: %v", err)
	}
}
