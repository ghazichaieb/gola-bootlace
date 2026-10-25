package admin

import "testing"

func TestAnIsoCanBeStartedVerboseOrWithASerialConsole(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"debug": map[string]bool{"verbose": true, "serial": true}}, nil); code != 200 {
		t.Fatal(code)
	}
	if d := r.store.Get().Debug["0123456789"]; !d.Verbose || !d.Serial {
		t.Fatalf("%+v", d)
	}
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"debug": map[string]bool{"verbose": false, "serial": false}}, nil); code != 200 {
		t.Fatal(code)
	}
	if got := r.store.Get().Debug; len(got) != 0 {
		t.Fatalf("both switches off must leave nothing behind: %v", got)
	}
	if code, _ := r.do("PUT", "/api/isos/ffffffffff", map[string]any{"debug": map[string]bool{"verbose": true}}, nil); code != 404 {
		t.Fatalf("an ISO that is not there: %d", code)
	}
}

func TestTheDebugShellSwitchIsSetForAnISOAndForAPC(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"debug": map[string]bool{"shell": true}}, nil); code != 200 {
		t.Fatal(code)
	}
	if d := r.store.Get().Debug["0123456789"]; !d.Shell || d.Verbose || d.Serial {
		t.Fatalf("only the shell switch was sent: %+v", d)
	}
	if code, _ := r.do("PUT", "/api/pcs/AA-BB-CC-DD-EE-09/options", map[string]any{"debug": map[string]bool{"shell": true}}, nil); code != 200 {
		t.Fatal(code)
	}
	if o := r.store.Get().PCOptions["aa:bb:cc:dd:ee:09"]; !o.Debug.Shell || o.Args != "" {
		t.Fatalf("a PC with only the shell switch must be kept: %+v", o)
	}
	if code, _ := r.do("PUT", "/api/pcs/AA-BB-CC-DD-EE-09/options", map[string]any{"debug": map[string]bool{"shell": false}}, nil); code != 200 {
		t.Fatal(code)
	}
	if got := r.store.Get().PCOptions; len(got) != 0 {
		t.Fatalf("every switch off must leave nothing behind: %v", got)
	}
}
