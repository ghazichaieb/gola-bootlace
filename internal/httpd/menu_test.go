package httpd

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// managed makes a server with two ISO files and a settings file.
func managed(t *testing.T) (*httptest.Server, *Server, *config.Store, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"alpha.iso", "beta.iso"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := isos.Scan(dir)
	ids := map[string]string{}
	for _, i := range list {
		ids[i.Title] = i.ID
	}
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{Title: "Test menu", MenuTimeout: 7})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ISODir: dir, Title: "x", Base: "http://10.0.0.1:8080", Config: store}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s, store, ids
}

func TestTheMenuFollowsTheSettings(t *testing.T) {
	ts, _, store, ids := managed(t)
	_, body := get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:01&ip=10.0.0.5", nil)
	if !strings.Contains(body, "menu Test menu") || !strings.Contains(body, "--timeout 7000") || !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") || !strings.Contains(body, "set menu-default shell") {
		t.Fatalf("defaults: %s", body)
	}
	if err := store.Update(func(c *config.Config) error {
		c.Hidden[ids["alpha"]] = true
		c.Titles[ids["beta"]] = "Beta renamed"
		c.Default = "iso:" + ids["beta"]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, body = get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:01&ip=10.0.0.5", nil)
	if strings.Contains(body, "alpha") || !strings.Contains(body, "Beta renamed") || !strings.Contains(body, "set menu-default e0-beta-renamed") {
		t.Fatalf("hidden, renamed, default: %s", body)
	}
	_ = store.Update(func(c *config.Config) error { c.Default = "local"; return nil })
	if _, body = get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:01", nil); !strings.Contains(body, "set menu-default local") {
		t.Fatalf("local default: %s", body)
	}
}

func TestARuleSendsThatPcAwayFromTheMenu(t *testing.T) {
	ts, s, store, ids := managed(t)
	_ = store.Update(func(c *config.Config) error {
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:02", Action: "local", Note: "lab PC"}, {MAC: "aa:bb:cc:dd:ee:03", Action: "iso:" + ids["alpha"]}}
		return nil
	})
	if _, body := get(t, ts.URL+"/menu?mac=AA-BB-CC-DD-EE-02&ip=10.0.0.6", nil); !strings.Contains(body, "exit\n") || strings.Contains(body, "choose") || !strings.Contains(body, "lab PC") {
		t.Fatalf("local rule: %s", body)
	}
	if _, body := get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:03&ip=10.0.0.7", nil); !strings.Contains(body, "sanboot") || strings.Contains(body, "choose") {
		t.Fatalf("iso rule: %s", body)
	}
	if _, body := get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:04&ip=10.0.0.8", nil); !strings.Contains(body, "choose") {
		t.Fatalf("a PC without a rule gets the menu: %s", body)
	}
	cl := s.Clients()
	if len(cl) != 3 {
		t.Fatalf("%+v", cl)
	}
	byMAC := map[string]Client{}
	for _, c := range cl {
		byMAC[c.MAC] = c
	}
	if byMAC["aa:bb:cc:dd:ee:02"].LastAction != "rule: own disk" || byMAC["aa:bb:cc:dd:ee:04"].IP != "10.0.0.8" {
		t.Fatalf("%+v", byMAC)
	}
}

func TestClientDetailsFromIpxeAreKeptAndCleaned(t *testing.T) {
	ts, s, _, _ := managed(t)
	get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:05&ip=10.0.0.9&plat=efi&man=Dell%20Inc.&prod=XPS%2013%209350&serial=ABC%0A123", nil)
	c := s.Clients()[0]
	if c.Manufacturer != "Dell Inc." || c.Product != "XPS 13 9350" || c.Serial != "ABC123" || c.Platform != "efi" || c.Count != 1 {
		t.Fatalf("%+v", c)
	}
	if _, body := get(t, ts.URL+"/menu?ip=10.0.0.1", nil); !strings.Contains(body, "choose") { // an old script without identity still gets a menu
		t.Fatal(body)
	}
}

func TestIsoInfosShowNamesAndHiding(t *testing.T) {
	_, s, store, ids := managed(t)
	_ = store.Update(func(c *config.Config) error { c.Hidden[ids["alpha"]] = true; c.Titles[ids["alpha"]] = "A"; return nil })
	infos, err := s.ISOInfos()
	if err != nil || len(infos) != 2 {
		t.Fatal(err, infos)
	}
	if infos[0].Title != "A" || infos[0].Original != "alpha" || !infos[0].Hidden || infos[0].Method != "disk" || infos[1].Hidden {
		t.Fatalf("%+v", infos)
	}
}
