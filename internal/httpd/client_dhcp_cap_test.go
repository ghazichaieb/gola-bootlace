package httpd

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// The DHCP listener adds PCs to the list too: a flood of requests with made-up MAC addresses must neither grow the list past its bound nor push out a configured PC.
func TestADHCPFloodCannotGrowTheListOrEvictAConfiguredPC(t *testing.T) {
	store, _, err := config.Open(filepath.Join(t.TempDir(), "settings.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mac := func(n int) string { return fmt.Sprintf("02:00:00:00:%02x:%02x", n>>8, n&255) }
	if err := store.Update(func(c *config.Config) error { c.PCNames = map[string]string{mac(0): "Workshop PC"}; return nil }); err != nil {
		t.Fatal(err)
	}
	s := &Server{Config: store}
	for i := 0; i < maxClients*5; i++ {
		s.NoteDHCP(mac(i), true, false)
	}
	if len(s.clients) != maxClients {
		t.Fatalf("the list holds %d PCs, the bound is %d", len(s.clients), maxClients)
	}
	if s.clients[mac(0)] == nil {
		t.Fatal("the configured PC was pushed out by the flood")
	}
}

func TestADHCPFloodAgainstAFullyConfiguredListAddsNothing(t *testing.T) {
	store, _, err := config.Open(filepath.Join(t.TempDir(), "settings.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for i := 0; i < maxClients; i++ {
		names[fmt.Sprintf("02:00:00:00:00:%02x", i)] = "Saved PC"
	}
	if err := store.Update(func(c *config.Config) error { c.PCNames = names; return nil }); err != nil {
		t.Fatal(err)
	}
	s := &Server{Config: store}
	for mac := range names {
		s.NoteDHCP(mac, false, false)
	}
	for i := 0; i < 1000; i++ {
		s.NoteDHCP(fmt.Sprintf("02:00:00:01:%02x:%02x", i>>8, i&255), true, false)
	}
	if len(s.clients) != maxClients {
		t.Fatalf("the list grew to %d", len(s.clients))
	}
	for mac := range names {
		if s.clients[mac] == nil {
			t.Fatal("a configured PC was lost")
		}
	}
}
