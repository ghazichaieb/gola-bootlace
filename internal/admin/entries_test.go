package admin

import (
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"testing"
)

func TestEntryManagementAPIValidatesAndPreservesSettings(t *testing.T) {
	r := newRig(t)
	r.login()
	const id = "0123456789"
	const way = "0123456789abcdef0123456789abcdef"
	r.backend.isos[0].Ways = []httpd.WayInfo{{ID: way, Title: "Safe graphics"}}
	settings := config.ISOEntrySettings{Default: way, Entries: map[string]config.EntryOptions{way: {Title: "Recovery", Args: "nomodeset"}}}
	if code, out := r.do("PUT", "/api/isos/"+id, map[string]any{"entrySettings": settings}, nil); code != 200 {
		t.Fatalf("save: %d %v", code, out)
	}
	if got := r.store.Get().ISOEntries[id]; got.Default != way || got.Entries[way].Title != "Recovery" {
		t.Fatalf("not saved: %+v", got)
	}
	for _, bad := range []config.ISOEntrySettings{
		{Default: way, Entries: map[string]config.EntryOptions{way: {Hidden: true}}},
		{Entries: map[string]config.EntryOptions{"fedcba9876543210fedcba9876543210": {Title: "Unknown"}}},
		{Entries: map[string]config.EntryOptions{way: {Args: "quiet\nboot"}}},
	} {
		if code, _ := r.do("PUT", "/api/isos/"+id, map[string]any{"entrySettings": bad}, nil); code != 400 {
			t.Fatalf("bad input accepted: %d %+v", code, bad)
		}
	}
	if r.store.Get().ISOEntries[id].Entries[way].Title != "Recovery" {
		t.Fatal("rejected update changed settings")
	}
	r.backend.isos[0].Ways = nil
	if code, out := r.do("PUT", "/api/isos/"+id, map[string]any{"entrySettings": settings}, nil); code != 200 {
		t.Fatalf("unchanged unavailable setting: %d %v", code, out)
	}
	r.admin.followSettings(map[string]string{id: "abcdef0123"}, nil)
	if r.store.Get().ISOEntries["abcdef0123"].Default != way {
		t.Fatal("settings did not follow rename")
	}
	r.admin.followSettings(nil, []string{"abcdef0123"})
	if len(r.store.Get().ISOEntries) != 0 {
		t.Fatal("deleted ISO retained settings")
	}
}
