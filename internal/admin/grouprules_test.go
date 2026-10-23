package admin

import "testing"

func TestGroupRulesAreAddedReplacedListedAndRemoved(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/grouprules", map[string]any{"kind": "colour", "match": "red", "action": "local"}, nil); code != 400 {
		t.Fatalf("an unknown kind: %d", code)
	}
	if code, _ := r.do("POST", "/api/grouprules", map[string]any{"kind": "model", "match": "ThinkCentre", "action": "local", "note": "office"}, nil); code != 200 {
		t.Fatal(code)
	}
	// the same text again (other case) replaces the rule, it does not add a second
	if code, _ := r.do("POST", "/api/grouprules", map[string]any{"kind": "model", "match": "thinkcentre", "action": "menu"}, nil); code != 200 {
		t.Fatal(code)
	}
	_, out := r.do("GET", "/api/grouprules", nil, nil)
	rules := out["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["action"] != "menu" {
		t.Fatalf("%v", out)
	}
	if code, _ := r.do("DELETE", "/api/grouprules?kind=model&match=THINKCENTRE", nil, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("DELETE", "/api/grouprules?kind=model&match=THINKCENTRE", nil, nil); code != 404 {
		t.Fatalf("already gone: %d", code)
	}
	if _, out = r.do("GET", "/api/grouprules", nil, nil); len(out["rules"].([]any)) != 0 {
		t.Fatalf("%v", out)
	}
}
