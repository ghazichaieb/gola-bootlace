package admin

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/backup"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// raw sends a body that is not JSON (a zip file) and returns the status and the bytes.
func (r *rig) raw(method, path string, body []byte) (int, []byte) {
	r.t.Helper()
	req, _ := http.NewRequest(method, r.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("X-Bootlace", "1")
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(r.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(res.Body)
	return res.StatusCode, b.Bytes()
}

func TestABackupCanBeDownloadedInspectedAndRestored(t *testing.T) {
	r := newRig(t)
	r.login()
	if err := r.store.Update(func(c *config.Config) error {
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:01", Action: "local"}}
		c.PCNames = map[string]string{"aa:bb:cc:dd:ee:01": "Desk"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code, plain := r.raw("GET", "/api/backup/download", nil)
	if code != 200 || len(plain) == 0 {
		t.Fatal(code)
	}
	c, err := backup.ReadZip(plain)
	if err != nil || c.Config.AdminHash != "" || len(c.Config.Rules) != 1 || len(c.PCs) == 0 {
		t.Fatalf("%+v %v", c, err)
	}
	_, withSecrets := r.raw("GET", "/api/backup/download?secrets=1", nil)
	if c, err = backup.ReadZip(withSecrets); err != nil || c.Config.AdminHash == "" || !c.Manifest.Secrets {
		t.Fatalf("%+v %v", c, err)
	}

	// change things, then inspect and restore the first backup
	if err := r.store.Update(func(c *config.Config) error { c.Rules = nil; c.PCNames = nil; c.MenuTimeout = 77; return nil }); err != nil {
		t.Fatal(err)
	}
	r.backend.clients = nil
	hash := r.store.Get().AdminHash
	code, body := r.raw("POST", "/api/backup/inspect", plain)
	if code != 200 || !bytes.Contains(body, []byte(`"rules":1`)) || !bytes.Contains(body, []byte(`"current"`)) {
		t.Fatalf("%d %s", code, body)
	}
	if r.store.Get().MenuTimeout != 77 {
		t.Fatal("inspecting changed something")
	}
	if code, body = r.raw("POST", "/api/backup/restore", plain); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	got := r.store.Get()
	if len(got.Rules) != 1 || got.PCNames["aa:bb:cc:dd:ee:01"] != "Desk" || got.MenuTimeout == 77 || got.AdminHash != hash {
		t.Fatalf("%+v", got)
	}
	if len(r.backend.clients) != 1 || r.backend.rescans == 0 {
		t.Fatalf("clients %v, rescans %d", r.backend.clients, r.backend.rescans)
	}

	// a file that is not a backup changes nothing
	before := r.store.Get().MenuTimeout
	if code, body = r.raw("POST", "/api/backup/restore", []byte("not a zip")); code != 400 || r.store.Get().MenuTimeout != before {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ = r.raw("POST", "/api/backup/restore", nil); code != 400 {
		t.Fatal(code)
	}
}

func TestTheLibrarysCopyOfTheSettingsIsOfferedAndTaken(t *testing.T) {
	r := newRig(t)
	r.login()
	if _, out := r.do("GET", "/api/backup/library", nil, nil); out["available"] != false {
		t.Fatalf("without a Sync: %v", out)
	}
	dir := t.TempDir()
	id, err := library.Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(dir, id)
	// another container wrote this copy
	other, _, _ := config.Open(filepath.Join(t.TempDir(), "other.json"), config.Config{})
	_ = other.Update(func(c *config.Config) error {
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:09", Action: "local"}}
		return nil
	})
	(&backup.Sync{Store: other, Lib: lib, Version: "0.1.5"}).Check()

	r.admin.o.Sync = &backup.Sync{Store: r.store, Lib: lib, Version: "0.1.5"}
	code, out := r.do("GET", "/api/backup/library", nil, nil)
	lc, _ := out["library"].(map[string]any)
	if code != 200 || out["pending"] != true || lc["rules"].(float64) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = r.do("POST", "/api/backup/library", map[string]any{"action": "nonsense"}, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _ = r.do("POST", "/api/backup/library", map[string]any{"action": "restore"}, nil); code != 200 {
		t.Fatal(code)
	}
	if len(r.store.Get().Rules) != 1 || r.store.Get().AdminHash == "" {
		t.Fatalf("%+v", r.store.Get())
	}
	if _, out = r.do("GET", "/api/backup/library", nil, nil); out["pending"] != false {
		t.Fatalf("nothing left to decide: %v", out)
	}
}
