// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryAndServicesNeedALogin(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/api/library", "/api/library/browse", "/api/library/inspect", "/api/services"} {
		if code, _ := r.do("GET", p, nil, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
	for _, p := range []string{"/api/services/start", "/api/services/stop"} {
		if code, _ := r.do("POST", p, map[string]any{}, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
}

func TestChoosingTheLibrary(t *testing.T) {
	r := newRig(t)
	r.login()
	dir := t.TempDir()

	if code, out := r.do("GET", "/api/library", nil, nil); code != 200 || out["library"].(map[string]any)["available"] != false {
		t.Fatalf("no library yet: %d %v", code, out)
	}
	// a folder that is not a library is only made one when the person says so, and nothing is written before that
	if code, out := r.do("PUT", "/api/library", map[string]any{"path": dir}, nil); code != 409 || out["needsCreate"] != true {
		t.Fatalf("without create: %d %v", code, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("the folder was written to before the person agreed")
	}
	if code, out := r.do("PUT", "/api/library", map[string]any{"path": dir, "create": true}, nil); code != 200 || out["library"].(map[string]any)["available"] != true {
		t.Fatalf("create: %d %v", code, out)
	}
	if c := r.store.Get(); c.Library.Path != dir || c.Library.ID == "" || r.lib.Path() != dir || r.backend.rescans != 1 {
		t.Fatalf("not kept: %+v lib=%q rescans=%d", c.Library, r.lib.Path(), r.backend.rescans)
	}
	for _, bad := range []string{"", string(filepath.Separator), filepath.Join(dir, "nowhere")} {
		if code, _ := r.do("PUT", "/api/library", map[string]any{"path": bad, "create": true}, nil); code != 400 {
			t.Errorf("%q was accepted: %d", bad, code)
		}
	}
	if r.lib.Path() != dir {
		t.Fatal("a refused folder changed the library")
	}
}

func TestBrowsingListsFoldersOnly(t *testing.T) {
	r := newRig(t)
	r.login()
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "a.iso"), []byte("x"), 0o644)
	code, out := r.do("GET", "/api/library/browse?path="+dir, nil, nil)
	dirs, _ := out["dirs"].([]any)
	if code != 200 || len(dirs) != 1 || dirs[0].(map[string]any)["name"] != "sub" {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("GET", "/api/library/browse?path="+filepath.Join(dir, "a.iso"), nil, nil); code != 400 {
		t.Fatalf("a file: %d", code)
	}
}

func TestStartStopAndTheWarningAboutPCsInUse(t *testing.T) {
	r := newRig(t)
	r.login()
	// no library: the services want to run but wait for one
	if code, out := r.do("POST", "/api/services/start", map[string]any{}, nil); code != 200 || out["warning"] == nil {
		t.Fatalf("start with no library: %d %v", code, out)
	}
	if r.probe.starts != 0 {
		t.Fatal("a service started without a library")
	}
	dir := t.TempDir()
	if code, _ := r.do("PUT", "/api/library", map[string]any{"path": dir, "create": true}, nil); code != 200 {
		t.Fatal(code)
	}
	r.sup.Reconcile()
	if r.probe.starts == 0 {
		t.Fatal("the services did not start when the library was chosen")
	}
	r.probe.sessions = []string{"10.0.0.7"}
	if code, out := r.do("POST", "/api/services/stop", map[string]any{}, nil); code != 409 || out["sessions"] == nil {
		t.Fatalf("stop while a PC is connected: %d %v", code, out)
	}
	if r.probe.stops != 0 {
		t.Fatal("stopped without being forced")
	}
	if code, _ := r.do("POST", "/api/services/stop", map[string]any{"force": true}, nil); code != 200 || r.probe.stops != 1 {
		t.Fatalf("forced stop: %d stops=%d", code, r.probe.stops)
	}
	// changing the library while PCs are connected needs the same agreement
	_, _ = r.do("POST", "/api/services/start", map[string]any{}, nil)
	other := t.TempDir()
	if code, out := r.do("PUT", "/api/library", map[string]any{"path": other, "create": true}, nil); code != 409 || out["sessions"] == nil {
		t.Fatalf("library change while in use: %d %v", code, out)
	}
	if r.lib.Path() != dir {
		t.Fatal("the library changed without being forced")
	}
	if code, _ := r.do("PUT", "/api/library", map[string]any{"path": other, "create": true, "force": true}, nil); code != 200 || r.lib.Path() != other {
		t.Fatalf("forced change: %d lib=%q", code, r.lib.Path())
	}
	if !r.sup.Report().Wanted || r.probe.starts < 3 {
		t.Fatalf("the services were not restarted on the new library (starts=%d)", r.probe.starts)
	}
}

func TestAutoStartIsASetting(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, out := r.do("GET", "/api/services", nil, nil); code != 200 || out["autoStart"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("PUT", "/api/services/autostart", map[string]any{"autoStart": false}, nil); code != 200 || !r.store.Get().NoAutoStart {
		t.Fatal("not kept")
	}
}

func TestAccessSettings(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do("GET", "/api/access", nil, nil); code != 401 {
		t.Fatalf("without a login: %d", code)
	}
	r.login()
	if code, out := r.do("GET", "/api/access", nil, nil); code != 200 || out["warning"] != nil {
		t.Fatalf("a local server address must not warn: %d %v", code, out)
	}
	if code, _ := r.do("PUT", "/api/access", map[string]any{"networks": []string{"100.64.0.0/10", "203.0.113.7"}}, nil); code != 200 {
		t.Fatalf("add networks: %d", code)
	}
	if c := r.store.Get(); len(c.Access.Networks) != 2 || c.Access.AllowPublic {
		t.Fatalf("not kept: %+v", c.Access)
	}
	if code, _ := r.do("PUT", "/api/access", map[string]any{"networks": []string{"0.0.0.0/0"}}, nil); code != 400 {
		t.Fatalf("a network that is most of the internet: %d", code)
	}
	// answering any address has to be confirmed, and a refused change changes nothing
	if code, out := r.do("PUT", "/api/access", map[string]any{"allowPublic": true}, nil); code != 409 || out["needsConfirm"] != true {
		t.Fatalf("without confirm: %d %v", code, out)
	}
	if r.store.Get().Access.AllowPublic {
		t.Fatal("applied without a confirmation")
	}
	if code, _ := r.do("PUT", "/api/access", map[string]any{"allowPublic": true, "confirm": true}, nil); code != 200 || !r.store.Get().Access.AllowPublic {
		t.Fatalf("confirmed: %d", code)
	}
}
