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
