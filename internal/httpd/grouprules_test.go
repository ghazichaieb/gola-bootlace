package httpd

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func groupServer(t *testing.T, mutate func(*config.Config)) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Rescue.iso"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error { mutate(c); return nil }); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{ISODir: dir, Base: "http://10.0.0.1:8080", Config: store}).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestARuleForAKindOfPcSendsEveryPcOfThatKindAndMacRulesWinOverIt(t *testing.T) {
	ts := groupServer(t, func(c *config.Config) {
		c.GroupRules = []config.GroupRule{
			{Kind: "model", Match: "thinkcentre", Action: "local", Note: "office desktops"},
			{Kind: "platform", Match: "pcbios", Action: "menu"},
		}
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:02", Action: "menu"}} // this one PC always gets the menu, whatever its group says
	})
	ask := func(mac, man, prod, plat string) string {
		_, body := get(t, ts.URL+"/menu?mac="+mac+"&ip=10.0.0.9&plat="+plat+"&man="+man+"&prod="+prod, nil)
		return body
	}
	if body := ask("aa:bb:cc:dd:ee:01", "LENOVO", "ThinkCentre%20M70q", "efi"); !strings.Contains(body, "own disk") || strings.Contains(body, "choose ") {
		t.Fatalf("a ThinkCentre goes to its own disk without a menu:\n%s", body)
	}
	if body := ask("aa:bb:cc:dd:ee:03", "LENOVO", "ThinkPad%20T14", "efi"); !strings.Contains(body, "choose ") {
		t.Fatalf("another model gets the menu:\n%s", body)
	}
	if body := ask("aa:bb:cc:dd:ee:02", "LENOVO", "ThinkCentre%20M70q", "efi"); !strings.Contains(body, "choose ") {
		t.Fatalf("a rule for this PC wins over its group:\n%s", body)
	}
	// "show the menu" for a platform stops the search: a BIOS ThinkCentre is not sent to its disk because the model rule comes first... it is listed first, so it does
	if body := ask("aa:bb:cc:dd:ee:04", "LENOVO", "ThinkCentre%20M70q", "pcbios"); !strings.Contains(body, "own disk") {
		t.Fatalf("the first rule that fits decides:\n%s", body)
	}
	if body := ask("aa:bb:cc:dd:ee:05", "ACME", "Box", "pcbios"); !strings.Contains(body, "choose ") {
		t.Fatalf("a BIOS PC of another model matches the menu rule:\n%s", body)
	}
}

func TestGroupRulesAreCheckedAndMatchWithoutRegardToCase(t *testing.T) {
	g := config.GroupRule{Kind: "manufacturer", Match: "dell"}
	if !g.Matches("Dell Inc.", "OptiPlex", "efi") || g.Matches("HP", "Dell-like", "efi") {
		t.Error("manufacturer matches the manufacturer text only")
	}
	if !(config.GroupRule{Kind: "model", Match: "optiplex 7"}).Matches("Dell Inc.", "OptiPlex 7090", "efi") {
		t.Error("model matches manufacturer and model text")
	}
	if (config.GroupRule{Kind: "model", Match: ""}).Matches("a", "b", "efi") {
		t.Error("an empty text matches nothing")
	}
	store, _, _ := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	for name, g := range map[string]config.GroupRule{
		"unknown kind":       {Kind: "colour", Match: "red", Action: "local"},
		"empty text":         {Kind: "model", Match: "  ", Action: "local"},
		"bad platform":       {Kind: "platform", Match: "arm", Action: "local"},
		"bad action":         {Kind: "model", Match: "x", Action: "iso:nope"},
		"line break":         {Kind: "model", Match: "a\nb", Action: "local"},
		"line break in note": {Kind: "model", Match: "a", Action: "local", Note: "x\ny"},
	} {
		if err := store.Update(func(c *config.Config) error { c.GroupRules = []config.GroupRule{g}; return nil }); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if err := store.Update(func(c *config.Config) error {
		c.GroupRules = []config.GroupRule{{Kind: "model", Match: "X1", Action: "local"}, {Kind: "model", Match: "x1", Action: "menu"}}
		return nil
	}); err == nil {
		t.Error("two rules for the same text (case aside) should be refused")
	}
}
