// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"bytes"
	"flag"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func fakeEnv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestOptionsCanBeSetInTheEnvironmentAndTheCommandLineWins(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	port := fs.Int("http-port", 8080, "")
	mode := fs.String("dhcp", "proxy", "")
	off := fs.Bool("no-admin", false, "")
	local := fs.Bool("menu-local", false, "")
	if err := applyEnv(fs, fakeEnv(map[string]string{"BOOTLACE_HTTP_PORT": "9000", "BOOTLACE_DHCP": "off", "BOOTLACE_NO_ADMIN": "true", "BOOTLACE_MENU_LOCAL": ""})); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse([]string{"-dhcp", "full"}); err != nil {
		t.Fatal(err)
	}
	if *port != 9000 || *mode != "full" || !*off || *local {
		t.Fatalf("port %d mode %q no-admin %v menu-local %v (the command line must win; an empty variable is not a value)", *port, *mode, *off, *local)
	}
	if got := envName("smb-forward"); got != "BOOTLACE_SMB_FORWARD" {
		t.Fatal(got)
	}
}

func TestABadValueInTheEnvironmentNamesTheVariable(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Int("http-port", 8080, "")
	err := applyEnv(fs, fakeEnv(map[string]string{"BOOTLACE_HTTP_PORT": "eighty"}))
	if err == nil || !strings.Contains(err.Error(), "BOOTLACE_HTTP_PORT") {
		t.Fatalf("%v", err)
	}
	var out, errOut bytes.Buffer
	t.Setenv("BOOTLACE_HTTP_PORT", "eighty")
	if code := run([]string{"serve", "-ip", "10.0.0.1"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "BOOTLACE_HTTP_PORT") {
		t.Fatalf("a bad variable must stop the start: %d %q", code, errOut.String())
	}
}

func TestHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte("ok\n"))
		}
	}))
	defer ok.Close()
	port := ok.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatal(port)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"health", "-port", port}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "ok") {
		t.Fatalf("a server that is well: %d %q %q", code, out.String(), errOut.String())
	}
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	badPort := bad.Listener.Addr().String()
	badPort = badPort[strings.LastIndex(badPort, ":")+1:]
	if code := run([]string{"health", "-port", badPort}, &out, &errOut); code != 1 {
		t.Fatalf("a server that answers an error: %d", code)
	}
	ok.Close()
	if code := run([]string{"health", "-port", port}, &out, &errOut); code != 1 {
		t.Fatalf("no server: %d", code)
	}
}

func TestVirtualInterfacesAreNotTheNetworkOfThePCs(t *testing.T) {
	for _, n := range []string{"docker0", "br-1a2b3c", "veth12ab", "virbr0", "cni0", "vEthernet (WSL)"} {
		if !virtualInterface(n) {
			t.Errorf("%s was taken for a real network", n)
		}
	}
	for _, n := range []string{"eth0", "enp3s0", "ens18", "Ethernet", "wlan0"} {
		if virtualInterface(n) {
			t.Errorf("%s was taken for a virtual network", n)
		}
	}
}
