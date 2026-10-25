package httpd

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func clientRequest(s *Server, mac string) Client {
	r := httptest.NewRequest("GET", "/menu?mac="+mac+"&host=claimed-name&asset=claimed-asset", nil)
	r.RemoteAddr = "10.0.0.9:1234"
	return s.noteClient(r)
}

func TestConfiguredClientsSurviveMACFlood(t *testing.T) {
	store, _, err := config.Open(filepath.Join(t.TempDir(), "settings.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mac := func(n int) string { return fmt.Sprintf("02:00:00:00:%02x:%02x", n>>8, n&255) }
	if err := store.Update(func(c *config.Config) error {
		c.PCNames = map[string]string{mac(0): "Workshop PC"}
		c.Rules = []config.Rule{{MAC: mac(1), Action: "local"}}
		c.PCOptions = map[string]config.PCOptions{mac(2): {Args: "nomodeset"}, mac(3): {Answer: config.NoAnswer}, mac(4): {Debug: config.DebugMode{Serial: true}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Config: store}
	for i := 0; i < maxClients; i++ {
		clientRequest(s, mac(i))
	}
	for i := 0; i < 5; i++ {
		s.clients[mac(i)].LastSeen = time.Now().Add(-time.Hour)
		s.clients[mac(i)].History = append(s.clients[mac(i)].History, HistoryEvent{What: "important old history"})
	}
	for i := maxClients; i < maxClients*3; i++ {
		clientRequest(s, mac(i))
	}
	if len(s.clients) != maxClients {
		t.Fatalf("list grew to %d", len(s.clients))
	}
	for i := 0; i < 5; i++ {
		c := s.clients[mac(i)]
		if c == nil || c.History[len(c.History)-1].What != "important old history" {
			t.Fatalf("configured PC %d or its history was evicted", i)
		}
	}
	if s.clients[mac(5)] != nil {
		t.Fatal("untrusted hostname/asset pinned an unconfigured PC")
	}
	// Removing administrative configuration makes the old records evictable again.
	if err := store.Update(func(c *config.Config) error { c.PCNames = nil; c.Rules = nil; c.PCOptions = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := maxClients * 3; i < maxClients*3+5; i++ {
		clientRequest(s, mac(i))
	}
	for i := 0; i < 5; i++ {
		if s.clients[mac(i)] != nil {
			t.Fatalf("unconfigured PC %d remained pinned", i)
		}
	}
}

func TestFullPinnedClientListStillServesNewRequests(t *testing.T) {
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
		clientRequest(s, mac)
	}
	got := clientRequest(s, "02:00:00:00:01:ff")
	if got.MAC != "02:00:00:00:01:ff" || got.Count != 1 {
		t.Fatalf("request lost its identity: %+v", got)
	}
	if len(s.clients) != maxClients || s.clients[got.MAC] != nil {
		t.Fatal("full list exceeded its bound or admitted a new record")
	}
	for mac := range names {
		if s.clients[mac] == nil {
			t.Fatal("pinned record lost")
		}
	}
	existing := clientRequest(s, "02:00:00:00:00:00")
	if existing.Count != 2 {
		t.Fatal("existing pinned client stopped updating")
	}
}
