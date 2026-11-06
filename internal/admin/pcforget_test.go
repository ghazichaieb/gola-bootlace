// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

package admin

import (
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
)

const forgotten = "aa:bb:cc:dd:ee:77"

// listedMACs is the MAC address of every row of the PCs page.
func listedMACs(r *rig) map[string]bool {
	_, out := r.do("GET", "/api/pcs", nil, nil)
	got := map[string]bool{}
	for _, p := range out["pcs"].([]any) {
		got[p.(map[string]any)["mac"].(string)] = true
	}
	return got
}

func TestAPCThatOnlyExistsAsARuleCanBeForgotten(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/rules", map[string]any{"mac": forgotten, "action": "local", "note": "by hand"}, nil); code != 200 {
		t.Fatal(code)
	}
	if !listedMACs(r)[forgotten] {
		t.Fatal("the rule's PC must be listed")
	}
	if code, _ := r.do("DELETE", "/api/pcs/"+forgotten, nil, nil); code != 200 {
		t.Fatalf("forgetting a PC that is only a rule: %d", code)
	}
	if listedMACs(r)[forgotten] || len(r.store.Get().Rules) != 0 {
		t.Fatalf("still there: %v %+v", listedMACs(r), r.store.Get().Rules)
	}
}

func TestForgettingAPCWithEveryKindOfSettingRemovesAllOfIt(t *testing.T) {
	r := newRig(t)
	r.login()
	r.backend.clients = append(r.backend.clients, httpd.Client{MAC: forgotten, IP: "10.0.0.9", Count: 3, LastSeen: time.Now(), History: []httpd.HistoryEvent{{Time: time.Now(), What: "menu"}}})
	other := "aa:bb:cc:dd:ee:78"
	err := r.store.Update(func(c *config.Config) error {
		c.Rules = []config.Rule{{MAC: forgotten, Action: "local"}, {MAC: other, Action: "local"}}
		c.PCNames = map[string]string{forgotten: "Till", other: "Back office"}
		c.PCOptions = map[string]config.PCOptions{
			forgotten: {Args: "nomodeset", Debug: config.DebugMode{Verbose: true}, Answer: config.NoAnswer, SecureBoot: true, NoShimFallback: true},
			other:     {Args: "quiet"},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := r.do("DELETE", "/api/pcs/"+forgotten, nil, nil); code != 200 {
		t.Fatal(code)
	}
	c := r.store.Get()
	if len(c.Rules) != 1 || c.Rules[0].MAC != other || c.PCNames[forgotten] != "" || c.PCNames[other] == "" {
		t.Fatalf("rules %+v names %v", c.Rules, c.PCNames)
	}
	if _, ok := c.PCOptions[forgotten]; ok || c.PCOptions[other].Args != "quiet" {
		t.Fatalf("options %+v", c.PCOptions)
	}
	if got := listedMACs(r); got[forgotten] || !got[other] || !got["aa:bb:cc:dd:ee:01"] {
		t.Fatalf("listed: %v", got)
	}
	// seen again later, it is a new PC: no name, no rule, no options, no history
	r.backend.clients = append(r.backend.clients, httpd.Client{MAC: forgotten, LastSeen: time.Now()})
	_, out := r.do("GET", "/api/pcs?q="+forgotten, nil, nil)
	p := out["pcs"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "rule", "options", "history"} {
		if _, has := p[k]; has {
			t.Fatalf("%s survived: %v", k, p)
		}
	}
}

func TestForgettingAPCThatWasNeverAddedIsNotFound(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("DELETE", "/api/pcs/"+forgotten, nil, nil); code != 404 {
		t.Fatalf("an unknown PC: %d", code)
	}
}

func TestForgettingAPCThatIsOnlyNamedOrOnlyHasOptionsWorks(t *testing.T) {
	r := newRig(t)
	r.login()
	r.do("PUT", "/api/pcs/"+forgotten+"/name", map[string]string{"name": "Spare"}, nil)
	if code, _ := r.do("DELETE", "/api/pcs/"+forgotten, nil, nil); code != 200 || len(r.store.Get().PCNames) != 0 {
		t.Fatal(code, r.store.Get().PCNames)
	}
	r.do("PUT", "/api/pcs/"+forgotten+"/options", map[string]any{"args": "nomodeset"}, nil)
	if code, _ := r.do("DELETE", "/api/pcs/"+forgotten, nil, nil); code != 200 || len(r.store.Get().PCOptions) != 0 {
		t.Fatal(code, r.store.Get().PCOptions)
	}
}
