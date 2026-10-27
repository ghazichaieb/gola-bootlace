package httpd

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func dhcpListServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, _, err := config.Open(filepath.Join(dir, "c.json"), config.Config{PCDays: config.DefaultPCDays})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Config: store, PCFile: filepath.Join(dir, "pcs.json")}
}

func listed(s *Server, mac string) *Client {
	for _, c := range s.Clients() {
		if c.MAC == mac {
			c := c
			return &c
		}
	}
	return nil
}

// A PC that only asked for a boot file is in the list the web interface reads, with the facts the page needs to label it.
func TestAPCThatOnlyAskedForABootFileIsInTheList(t *testing.T) {
	s := dhcpListServer(t)
	const mac = "d8:43:ae:d6:2f:05"
	s.NoteDHCP("D8:43:AE:D6:2F:05", true, false)
	c := listed(s, mac)
	if c == nil {
		t.Fatal("the PC is not listed")
	}
	if c.Count != 0 || c.Platform != "efi" || c.LastSeen.IsZero() || time.Since(c.LastSeen) > time.Minute || c.LastAction != "asked for a boot file" {
		t.Fatalf("%+v", c)
	}
}

// The list is kept in a file: a PC that asked before a restart is still there afterwards, and one that asks after the restart is added to what was read.
func TestPCsThatAskedForABootFileSurviveARestart(t *testing.T) {
	s := dhcpListServer(t)
	s.NoteDHCP("d8:43:ae:d6:2f:05", true, false)
	s.FlushClients()
	again := &Server{Config: s.Config, PCFile: s.PCFile}
	if err := again.LoadClients(); err != nil {
		t.Fatal(err)
	}
	again.NoteDHCP("d8:43:ae:d6:2f:06", true, false)
	if listed(again, "d8:43:ae:d6:2f:05") == nil || listed(again, "d8:43:ae:d6:2f:06") == nil {
		t.Fatalf("after the restart: %+v", again.Clients())
	}
}

// A full list gives way to the PC that asks now (the one seen longest ago goes) unless every place is held by a PC somebody configured.
func TestANewPCIsListedWhenTheListIsFullOfUnconfiguredPCs(t *testing.T) {
	s := dhcpListServer(t)
	for i := 0; i < maxClients; i++ {
		s.NoteDHCP(fmt.Sprintf("02:00:00:00:00:%02x", i), true, false)
	}
	s.NoteDHCP("d8:43:ae:d6:2f:05", true, false)
	if listed(s, "d8:43:ae:d6:2f:05") == nil {
		t.Fatal("the PC that asked last is not listed")
	}
	if n := len(s.Clients()); n != maxClients {
		t.Fatalf("%d PCs", n)
	}
}
