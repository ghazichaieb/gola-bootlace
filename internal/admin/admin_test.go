package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
)

type fakeBackend struct {
	isos    []httpd.ISOInfo
	clients []httpd.Client
	rescans int
}

func (f *fakeBackend) ISOInfos() ([]httpd.ISOInfo, error) { return f.isos, nil }
func (f *fakeBackend) Clients() []httpd.Client            { return f.clients }
func (f *fakeBackend) Rescan()                            { f.rescans++ }
func (f *fakeBackend) StartedAt() time.Time               { return time.Now().Add(-time.Minute) }

type rig struct {
	t       *testing.T
	ts      *httptest.Server
	store   *config.Store
	backend *fakeBackend
	log     *logbuf.Buffer
	cookie  *http.Cookie
}

func newRig(t *testing.T) *rig {
	t.Helper()
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{Title: "Gola Bootlace"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePassword(store, "correct horse"); err != nil {
		t.Fatal(err)
	}
	b := &fakeBackend{
		isos:    []httpd.ISOInfo{{ID: "0123456789", Name: "a.iso", Title: "A", Original: "a", Size: 5, Method: "disk"}},
		clients: []httpd.Client{{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.5", LastSeen: time.Now()}},
	}
	lg := logbuf.New(100)
	lg.Add("first line")
	a := New(Options{Store: store, Log: lg, Backend: b, ISODir: "/isos", Facts: [][2]string{{"Server address", "10.0.0.1"}}})
	mux := http.NewServeMux()
	for p, h := range a.Mount() {
		mux.Handle(p, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &rig{t: t, ts: ts, store: store, backend: b, log: lg}
}

func (r *rig) do(method, path string, body any, headers map[string]string) (int, map[string]any) {
	r.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, r.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		req.Header.Set("X-Bootlace", "1")
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	if r.cookie != nil {
		req.AddCookie(r.cookie)
	}
	res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			r.cookie = c
		}
	}
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (r *rig) login() {
	r.t.Helper()
	if code, out := r.do("POST", "/api/login", map[string]string{"password": "correct horse"}, nil); code != 200 || out["authenticated"] != true {
		r.t.Fatalf("login: %d %v", code, out)
	}
}

func TestNothingWorksWithoutALogin(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/api/status", "/api/isos", "/api/settings", "/api/pcs", "/api/log"} {
		if code, _ := r.do("GET", p, nil, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
	if code, out := r.do("GET", "/api/session", nil, nil); code != 200 || out["authenticated"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("POST", "/api/login", map[string]string{"password": "wrong"}, nil); code != 401 {
		t.Fatalf("a wrong password: %d", code)
	}
}

func TestAChangeNeedsTheHeaderThatOtherSitesCannotAdd(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"title": "x", "menuTimeout": 3, "default": ""}, map[string]string{"X-Bootlace": ""}); code != 403 {
		t.Fatalf("without the header: %d", code)
	}
	if code, _ := r.do("POST", "/api/login", map[string]string{"password": "correct horse"}, map[string]string{"X-Bootlace": ""}); code != 403 {
		t.Fatalf("login without the header: %d", code)
	}
}

func TestSettingsAndIsosAreChangedThroughTheApi(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, out := r.do("PUT", "/api/settings", map[string]any{"title": "My menu", "menuTimeout": 12, "default": "iso:0123456789"}, nil); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if c := r.store.Get(); c.Title != "My menu" || c.MenuTimeout != 12 || c.Default != "iso:0123456789" {
		t.Fatalf("%+v", c)
	}
	if code, out := r.do("PUT", "/api/settings", map[string]any{"title": "x", "menuTimeout": -5, "default": ""}, nil); code != 400 || out["error"] == nil {
		t.Fatalf("a bad timeout: %d %v", code, out)
	}
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"hidden": true, "title": "Renamed"}, nil); code != 200 {
		t.Fatal(code)
	}
	if c := r.store.Get(); !c.Hidden["0123456789"] || c.Titles["0123456789"] != "Renamed" {
		t.Fatalf("%+v", c)
	}
	if code, _ := r.do("PUT", "/api/isos/ffffffffff", map[string]any{"hidden": true}, nil); code != 404 {
		t.Fatalf("an unknown ISO: %d", code)
	}
	if code, out := r.do("GET", "/api/isos", nil, nil); code != 200 || out["default"] != "iso:0123456789" {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("POST", "/api/rescan", map[string]any{}, nil); code != 200 || r.backend.rescans != 1 {
		t.Fatal(code, r.backend.rescans)
	}
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"title": "x", "bogus": 1}, nil); code != 400 {
		t.Fatalf("an unknown field: %d", code)
	}
}

func TestRulesAreAddedListedWithPcsAndRemoved(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/rules", map[string]any{"mac": "AA-BB-CC-DD-EE-01", "action": "local", "note": "desk"}, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/rules", map[string]any{"mac": "11:22:33:44:55:66", "action": "iso:0123456789"}, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/rules", map[string]any{"mac": "zz", "action": "local"}, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/rules", map[string]any{"mac": "11:22:33:44:55:66", "action": "bogus"}, nil); code != 400 {
		t.Fatal(code)
	}
	_, out := r.do("GET", "/api/pcs", nil, nil)
	pcs := out["pcs"].([]any)
	if len(pcs) != 2 || pcs[0].(map[string]any)["rule"].(map[string]any)["action"] != "local" || pcs[1].(map[string]any)["mac"] != "11:22:33:44:55:66" {
		t.Fatalf("%v", pcs)
	}
	if code, _ := r.do("DELETE", "/api/rules/AA-BB-CC-DD-EE-01", nil, nil); code != 200 {
		t.Fatal(code)
	}
	if len(r.store.Get().Rules) != 1 {
		t.Fatalf("%+v", r.store.Get().Rules)
	}
}

func TestStatusAndLogArePassedOn(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, out := r.do("GET", "/api/status", nil, nil); code != 200 || out["isoCount"].(float64) != 1 || out["pcs"].(float64) != 1 || len(out["facts"].([]any)) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	r.log.Add("second line")
	_, out := r.do("GET", "/api/log?after=1", nil, nil)
	lines := out["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["text"] != "second line" || out["last"].(float64) != 2 {
		t.Fatalf("%v", out)
	}
}

func TestThePasswordCanBeChangedAndOldOnesStopWorking(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/password", map[string]any{"current": "nope", "new": "a new long one"}, nil); code != 403 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/password", map[string]any{"current": "correct horse", "new": "short"}, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _ := r.do("POST", "/api/password", map[string]any{"current": "correct horse", "new": "a new long one"}, nil); code != 200 {
		t.Fatal(code)
	}
	if !CheckPassword(r.store.Get().AdminHash, "a new long one") || CheckPassword(r.store.Get().AdminHash, "correct horse") {
		t.Fatal("the stored hash must follow the change")
	}
	if strings.Contains(r.store.Get().AdminHash, "a new long one") {
		t.Fatal("the password must not be stored")
	}
}

func TestTooManyWrongPasswordsLockTheAddressOut(t *testing.T) {
	r := newRig(t)
	if testing.Short() {
		t.Skip("each wrong guess waits")
	}
	for i := 0; i < maxFailures; i++ {
		if code, _ := r.do("POST", "/api/login", map[string]string{"password": "wrong"}, nil); code != 401 {
			t.Fatal(code)
		}
	}
	if code, _ := r.do("POST", "/api/login", map[string]string{"password": "correct horse"}, nil); code != 429 {
		t.Fatalf("even the right password is refused while locked out: %d", code)
	}
}

func TestPasswordHelpers(t *testing.T) {
	h, err := HashPassword("secret")
	if err != nil || !CheckPassword(h, "secret") || CheckPassword(h, "Secret") || CheckPassword("", "") || CheckPassword("garbage", "x") {
		t.Fatal(h, err)
	}
	a, b := RandomPassword(), RandomPassword()
	if len(a) != 14 || a == b {
		t.Fatal(a, b)
	}
	store, _, _ := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	gen, err := EnsurePassword(store, "")
	if err != nil || gen == "" || !CheckPassword(store.Get().AdminHash, gen) {
		t.Fatal(gen, err)
	}
	if again, _ := EnsurePassword(store, ""); again != "" {
		t.Fatal("an existing password is kept")
	}
	if _, err := EnsurePassword(store, "given one"); err != nil || !CheckPassword(store.Get().AdminHash, "given one") {
		t.Fatal(err)
	}
}

func TestThePageAndTheRedirectAreServed(t *testing.T) {
	r := newRig(t)
	req, _ := http.NewRequest("GET", r.ts.URL+"/admin/", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(res.Body)
	if res.StatusCode != 200 || !strings.Contains(buf.String(), "Gola Bootlace") || !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("%d %s", res.StatusCode, res.Header)
	}
	if code, _ := r.do("GET", "/", nil, nil); code != 302 {
		t.Fatalf("the address of the server itself leads to the pages: %d", code)
	}
}
