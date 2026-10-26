package httpd

import (
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"strings"
	"testing"
)

const entryChoice = "0123456789abcdef0123456789abcdef"

func TestManagedEntriesApplyToMenusRulesAndPreviews(t *testing.T) {
	ts, s, store, ids := managed(t)
	list, _ := s.scan()
	way := safeWay
	way.ID = entryChoice
	s.known = map[string]recognised{}
	s.waysKnown = map[string][]family.Way{}
	for _, iso := range list {
		s.known["generic|"+stamp(iso)] = recognised{boot: family.Boot{Family: "Ubuntu-style (casper)", Kind: "casper", Commands: []string{"kernel http://s/vmlinuz boot=casper quiet", "boot"}}, ok: true}
		s.waysKnown["ways|"+stamp(iso)] = []family.Way{way}
	}
	id := ids["alpha"]
	if err := store.Update(func(c *config.Config) error {
		c.ISOEntries = map[string]config.ISOEntrySettings{id: {Default: entryChoice, Entries: map[string]config.EntryOptions{entryChoice: {Title: "Graphics recovery", Args: "entryarg"}}}}
		c.Args = map[string]string{id: "isoarg"}
		c.BootPasswords = map[string]string{id: "test-hash"}
		c.PCOptions = map[string]config.PCOptions{"aa:bb:cc:dd:ee:01": {Args: "pcarg"}}
		c.Default = "iso:" + id
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:02", Action: "iso:" + id}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"efi", "pcbios"} {
		_, body := get(t, ts.URL+"/menu?plat="+platform+"&mac=aa:bb:cc:dd:ee:01", nil)
		if !strings.Contains(body, "nomodeset quiet isoarg entryarg pcarg") || !strings.Contains(body, "--timeout 7000") {
			t.Fatalf("main/default menu:\n%s", body)
		}
		_, body = get(t, ts.URL+"/menu?plat="+platform+"&mac=aa:bb:cc:dd:ee:01&ways="+id, nil)
		body = strings.ReplaceAll(body, "\xff", " ")
		if !strings.Contains(body, "Graphics recovery") || !strings.Contains(body, "The usual start") || strings.Count(body, "nomodeset") != 1 || !strings.Contains(body, "login") {
			t.Fatalf("alternatives and usual start/password:\n%s", body)
		}
	}
	_, direct := get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:02", nil)
	if !strings.Contains(direct, "nomodeset quiet isoarg entryarg") || strings.Contains(direct, "choose") {
		t.Fatalf("rule ignores default:\n%s", direct)
	}
	infos, err := s.ISOInfos()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.ID == id && (info.EntrySettings.Default != entryChoice || info.Ways[0].ID != entryChoice || !strings.Contains(strings.Join(info.Commands, "\n"), "entryarg")) {
			t.Fatalf("preview/settings: %+v", info)
		}
	}
	if strings.Contains(way.Commands[0], "entryarg") {
		t.Fatal("shared command mutated")
	}
	// A disappeared selection falls back until recognition finds it again.
	for _, iso := range list {
		s.waysKnown["ways|"+stamp(iso)] = nil
	}
	entries, _ := s.Entries()
	for _, entry := range entries {
		if strings.Contains(strings.Join(entry.Commands, "\n"), "nomodeset") {
			t.Fatal("missing payload still selected")
		}
	}
}

func TestHiddenWaysAreOmittedAndConfiguredTitlesCannotInjectIpxe(t *testing.T) {
	s := &Server{Base: "http://s"}
	way := safeWay
	way.ID = entryChoice
	iso := waysISO("0123456789", "Ubuntu", way)
	cfg := config.Config{ISOEntries: map[string]config.ISOEntrySettings{iso.ISO.ID: {Entries: map[string]config.EntryOptions{entryChoice: {Hidden: true}}}}}
	page := waysPage(s, cfg, []entryInfo{iso}, config.PCOptions{}, "efi", "list", 1)
	if strings.Contains(page, "Ubuntu") {
		t.Fatal("all hidden alternatives still listed")
	}
	settings := cfg.ISOEntries[iso.ISO.ID]
	settings.Entries[entryChoice] = config.EntryOptions{Title: "${bad} && shell", Args: "nomodeset"}
	page = waysPage(s, cfg, []entryInfo{iso}, config.PCOptions{}, "efi", iso.ISO.ID, 1)
	if strings.Contains(page, "${bad}") || strings.Contains(page, "&& shell") {
		t.Fatalf("title interpreted as iPXE:\n%s", page)
	}
}
