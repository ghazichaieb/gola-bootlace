// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// withLibrary logs in and chooses a new folder as the library; it returns the folder.
func withLibrary(t *testing.T) (*rig, string) {
	t.Helper()
	r := newRig(t)
	r.login()
	dir := t.TempDir()
	if code, out := r.do("PUT", "/api/library", map[string]any{"path": dir, "create": true}, nil); code != 200 {
		t.Fatalf("library: %d %v", code, out)
	}
	return r, dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isoWith(n int) string {
	b := make([]byte, n)
	copy(b[0x8001:], "CD001")
	return string(b)
}

func itoa(n int) string { return strconv.Itoa(n) }

func decode(r io.Reader) map[string]any {
	var out map[string]any
	_ = json.NewDecoder(r).Decode(&out)
	return out
}

func TestFileManagementNeedsALogin(t *testing.T) {
	r := newRig(t)
	for _, c := range [][2]string{{"GET", "/api/library/files"}, {"POST", "/api/library/folder"}, {"POST", "/api/library/rename"}, {"POST", "/api/library/delete"},
		{"POST", "/api/library/import"}, {"POST", "/api/library/import/check"}, {"GET", "/api/library/jobs"}, {"PUT", "/api/library/upload"}, {"POST", "/api/library/upload/check"}, {"PUT", "/api/library/limit"}} {
		if code, _ := r.do(c[0], c[1], map[string]any{}, nil); code != 401 {
			t.Errorf("%s %s: %d", c[0], c[1], code)
		}
	}
}

func TestListMakeRenameAndDelete(t *testing.T) {
	r, dir := withLibrary(t)
	write(t, filepath.Join(dir, "a.iso"), "x")
	write(t, filepath.Join(dir, "notes.txt"), "keep")
	if code, out := r.do("GET", "/api/library/files", nil, nil); code != 200 || len(out["entries"].([]any)) != 2 {
		t.Fatalf("list: %d %v", code, out)
	}
	if code, _ := r.do("POST", "/api/library/folder", map[string]any{"path": "linux"}, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/library/folder", map[string]any{"path": "linux"}, nil); code != 409 {
		t.Fatalf("a folder that is there: %d", code)
	}
	if code, _ := r.do("POST", "/api/library/folder", map[string]any{"path": "../escape"}, nil); code != 400 {
		t.Fatalf("a folder outside: %d", code)
	}
	// what the settings say about an ISO follows it when it is renamed
	oldID, newID := isos.IDOf("a.iso"), isos.IDOf("linux/b.iso")
	_ = r.store.Update(func(c *config.Config) error {
		c.Titles[oldID], c.Hidden[oldID], c.Args[oldID], c.Default = "My A", true, "quiet", "iso:"+oldID
		c.Rules = append(c.Rules, config.Rule{MAC: "aa:bb:cc:dd:ee:01", Action: "iso:" + oldID})
		return nil
	})
	if code, out := r.do("POST", "/api/library/rename", map[string]any{"from": "a.iso", "to": "linux/b.iso"}, nil); code != 200 {
		t.Fatalf("rename: %d %v", code, out)
	}
	c := r.store.Get()
	if c.Titles[newID] != "My A" || !c.Hidden[newID] || c.Args[newID] != "quiet" || c.Default != "iso:"+newID || c.Rules[0].Action != "iso:"+newID {
		t.Fatalf("the settings did not follow the rename: %+v", c)
	}
	if _, ok := c.Titles[oldID]; ok {
		t.Fatal("the old id still has a title")
	}
	if code, _ := r.do("POST", "/api/library/rename", map[string]any{"from": "linux/b.iso", "to": "linux/b.txt"}, nil); code != 400 {
		t.Fatalf("a name that is not an ISO name: %d", code)
	}
	// delete: a file in use asks first; a folder with something in it is refused; a file that is not an ISO is not touched
	r.backend.inUse = map[string]bool{"linux/b.iso": true}
	if code, out := r.do("POST", "/api/library/delete", map[string]any{"path": "linux/b.iso"}, nil); code != 409 || out["inUse"] != true {
		t.Fatalf("delete while in use: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "linux", "b.iso")); err != nil {
		t.Fatal("deleted without being forced")
	}
	if code, _ := r.do("POST", "/api/library/delete", map[string]any{"path": "linux", "force": true}, nil); code != 400 {
		t.Fatalf("a folder with an ISO in it: %d", code)
	}
	if code, _ := r.do("POST", "/api/library/delete", map[string]any{"path": "notes.txt"}, nil); code != 400 {
		t.Fatalf("a file that is not an ISO: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("a file that is not an ISO was deleted")
	}
	if code, _ := r.do("POST", "/api/library/delete", map[string]any{"path": "linux/b.iso", "force": true}, nil); code != 200 {
		t.Fatalf("forced delete: %d", code)
	}
	if _, ok := r.store.Get().Titles[newID]; ok || r.store.Get().Default != "" {
		t.Fatalf("what belonged to the deleted ISO is still in the settings: %+v", r.store.Get())
	}
	if code, _ := r.do("POST", "/api/library/delete", map[string]any{"path": "linux"}, nil); code != 200 {
		t.Fatalf("an empty folder: %d", code)
	}
}

func upload(r *rig, query string, body string) (int, map[string]any) {
	req, _ := http.NewRequest("PUT", r.ts.URL+"/api/library/upload?"+query, bytes.NewReader([]byte(body)))
	req.Header.Set("X-Bootlace", "1")
	req.AddCookie(r.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode, decode(res.Body)
}

func TestUploadThroughTheApi(t *testing.T) {
	r, dir := withLibrary(t)
	body := isoWith(200 << 10)
	if code, out := r.do("POST", "/api/library/upload/check", map[string]any{"name": "up.iso", "size": len(body)}, nil); code != 200 || len(out["problems"].([]any)) != 0 || out["matches"] == nil {
		t.Fatalf("check: %d %v", code, out)
	}
	if code, out := upload(r, "name=up.iso&size="+itoa(len(body)), body); code != 200 || out["dest"] != "up.iso" {
		t.Fatalf("upload: %d %v", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "up.iso")); string(b) != body {
		t.Fatal("the file is not whole")
	}
	if code, out := upload(r, "name=up.iso&size="+itoa(len(body)), body); code != 400 || !strings.Contains(out["error"].(string), "already there") {
		t.Fatalf("a name that is taken: %d %v", code, out)
	}
	if code, out := upload(r, "name=web.iso&size=30", "<html>not an iso at all......"); code != 400 {
		t.Fatalf("not an ISO: %d %v", code, out)
	}
	if code, _ := upload(r, "name=x.iso&size=999", "short"); code != 400 {
		t.Fatalf("the size said is not the size sent: %d", code)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".bootlace", "incoming", "*.part")); len(m) != 0 {
		t.Fatalf("part files left: %v", m)
	}
	// a repeated upload is noticed before it is sent
	if code, out := r.do("POST", "/api/library/upload/check", map[string]any{"name": "up.iso", "size": len(body)}, nil); code != 200 || len(out["matches"].([]any)) == 0 {
		t.Fatalf("a repeat was not noticed: %d %v", code, out)
	}
}

// The server never fetches from a local address, whatever the web interface is told.
func TestImportRefusesLocalAddresses(t *testing.T) {
	r, _ := withLibrary(t)
	for _, u := range []string{"http://127.0.0.1:1/a.iso", "http://192.168.1.1/a.iso", "http://169.254.169.254/latest/a.iso", "http://localhost:9/a.iso", "file:///etc/passwd", "ftp://example.org/a.iso"} {
		if code, out := r.do("POST", "/api/library/import", map[string]any{"url": u}, nil); code != 400 {
			t.Errorf("%s: %d %v", u, code, out)
		}
		if code, out := r.do("POST", "/api/library/import/check", map[string]any{"url": u}, nil); code != 200 || len(out["problems"].([]any)) == 0 {
			t.Errorf("check %s: %d %v", u, code, out)
		}
	}
	if code, out := r.do("GET", "/api/library/jobs", nil, nil); code != 200 || len(out["jobs"].([]any)) != 0 {
		t.Fatalf("jobs: %d %v", code, out)
	}
}

func TestTheImportLimitIsASetting(t *testing.T) {
	r, _ := withLibrary(t)
	if code, _ := r.do("PUT", "/api/library/limit", map[string]any{"mbps": 20}, nil); code != 200 || r.store.Get().ImportLimitMBps != 20 {
		t.Fatal("the limit was not kept")
	}
	if code, _ := r.do("PUT", "/api/library/limit", map[string]any{"mbps": -1}, nil); code != 400 {
		t.Fatal("a negative limit")
	}
}

func cookieRequest(token string) *http.Request {
	return &http.Request{Header: http.Header{"Cookie": []string{cookieName + "=" + token}}}
}

// A login lasts while something is done, ends after a quiet half hour, and a page that only refreshes itself does not keep it.
func TestTheLoginSlides(t *testing.T) {
	r := newRig(t)
	r.login()
	a := r.admin
	token := r.cookie.Value
	a.mu.Lock()
	s := a.sessions[token]
	s.last = time.Now().Add(-idleFor + time.Minute)
	a.mu.Unlock()
	if code, _ := r.do("GET", "/api/status", nil, map[string]string{"X-Bootlace-Poll": "1"}); code != 200 {
		t.Fatalf("a poll inside the idle time: %d", code)
	}
	a.mu.Lock()
	if time.Since(s.last) < idleFor-2*time.Minute {
		t.Fatal("a poll counted as activity")
	}
	a.mu.Unlock()
	if code, _ := r.do("GET", "/api/status", nil, nil); code != 200 {
		t.Fatalf("a request inside the idle time: %d", code)
	}
	a.mu.Lock()
	if time.Since(s.last) > time.Minute {
		t.Fatal("a request did not extend the login")
	}
	s.last = time.Now().Add(-idleFor - time.Minute)
	a.mu.Unlock()
	if code, _ := r.do("GET", "/api/status", nil, map[string]string{"X-Bootlace-Poll": "1"}); code != 401 {
		t.Fatalf("a quiet login must end: %d", code)
	}
	// a long transfer keeps it alive
	r.login()
	token = r.cookie.Value
	a.mu.Lock()
	s = a.sessions[token]
	s.last, s.inflight = time.Now().Add(-idleFor-time.Hour), 1
	a.mu.Unlock()
	if !a.valid(cookieRequest(token)) {
		t.Fatal("a login with a transfer in progress ended")
	}
	// and nothing lasts longer than the longest time
	a.mu.Lock()
	s.created = time.Now().Add(-sessionFor - time.Minute)
	a.mu.Unlock()
	if a.valid(cookieRequest(token)) {
		t.Fatal("a login outlived the longest time")
	}
}
