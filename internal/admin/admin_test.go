package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
	"github.com/ghazichaieb/gola-bootlace/internal/pxecheck"
	"github.com/ghazichaieb/gola-bootlace/internal/selftest"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
	"github.com/ghazichaieb/gola-bootlace/internal/speedtest"
)

type fakeBackend struct {
	isos    []httpd.ISOInfo
	clients []httpd.Client
	rescans int
	inUse   map[string]bool
	cache   string // the container's cache folder CacheUsage reports
}

func (f *fakeBackend) ISOInfos() ([]httpd.ISOInfo, error) { return f.isos, nil }
func (f *fakeBackend) Clients() []httpd.Client            { return f.clients }
func (f *fakeBackend) Rescan()                            { f.rescans++ }
func (f *fakeBackend) ReplaceClients(list []httpd.Client) { f.clients = list }
func (f *fakeBackend) ForgetPC(mac string) bool {
	for i, c := range f.clients {
		if c.MAC == mac {
			f.clients = append(f.clients[:i], f.clients[i+1:]...)
			return true
		}
	}
	return false
}
func (f *fakeBackend) ForgetPCsNotSeenFor(d time.Duration) int {
	kept, n := f.clients[:0], 0
	for _, c := range f.clients {
		if time.Since(c.LastSeen) > d {
			n++
		} else {
			kept = append(kept, c)
		}
	}
	f.clients = kept
	return n
}
func (f *fakeBackend) StartedAt() time.Time   { return time.Now().Add(-time.Minute) }
func (f *fakeBackend) InUse(path string) bool { return f.inUse[path] }
func (f *fakeBackend) CacheUsage() httpd.CacheUsage {
	dir := f.cache
	if dir == "" {
		dir = "/cache"
	}
	return httpd.CacheUsage{Where: "auto", LocalLimit: 2 << 30, LocalDir: dir, LocalBytes: 5}
}

type rig struct {
	t       *testing.T
	ts      *httptest.Server
	store   *config.Store
	backend *fakeBackend
	log     *logbuf.Buffer
	cookie  *http.Cookie
	admin   *Admin
	lib     *library.Library
	sup     *service.Supervisor
	probe   *probeService
}

func newRig(t *testing.T) *rig {
	t.Helper()
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
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
	lib := library.New("", "")
	indexer := &library.Indexer{Lib: lib}
	imp := importer.New(lib, indexer.Get, nil)
	sup := &service.Supervisor{Gate: lib.Check}
	probe := &probeService{}
	sup.Add(service.Def{ID: "probe", Title: "Probe", Start: probe.start})
	a := New(Options{Store: store, Log: lg, Backend: b, Library: lib, Services: sup, Access: access.New(nil), ServerIP: "192.168.1.5", Importer: imp, Indexer: indexer, Facts: [][2]string{{"Server address", "10.0.0.1"}}})
	mux := http.NewServeMux()
	for p, h := range a.Mount() {
		mux.Handle(p, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &rig{t: t, ts: ts, store: store, backend: b, log: lg, lib: lib, sup: sup, probe: probe, admin: a}
}

// probeService counts its starts and stops, and has the PCs it is told it has.
type probeService struct {
	starts, stops int
	sessions      []string
}

func (p *probeService) start() (service.Instance, error) {
	p.starts++
	return service.Instance{Stop: func() { p.stops++ }, Sessions: func() []string { return p.sessions }}, nil
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
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"menuTimeout": 3, "default": ""}, map[string]string{"X-Bootlace": ""}); code != 403 {
		t.Fatalf("without the header: %d", code)
	}
	if code, _ := r.do("POST", "/api/login", map[string]string{"password": "correct horse"}, map[string]string{"X-Bootlace": ""}); code != 403 {
		t.Fatalf("login without the header: %d", code)
	}
}

func TestSettingsAndIsosAreChangedThroughTheApi(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, out := r.do("PUT", "/api/settings", map[string]any{"menuTimeout": 12, "default": "iso:0123456789"}, nil); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if c := r.store.Get(); c.MenuTimeout != 12 || c.Default != "iso:0123456789" {
		t.Fatalf("%+v", c)
	}
	if code, out := r.do("PUT", "/api/settings", map[string]any{"menuTimeout": -5, "default": ""}, nil); code != 400 || out["error"] == nil {
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
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"bogus": 1}, nil); code != 400 {
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

func TestPcListIsPagedSearchedAndCleaned(t *testing.T) {
	r := newRig(t)
	r.login()
	r.backend.clients = nil
	// Keep fixtures away from the exact ten-day cutoff: Windows clock
	// resolution may give setup and the request the same timestamp.
	seen := time.Now().Add(-12 * time.Hour)
	for i := 0; i < 30; i++ {
		r.backend.clients = append(r.backend.clients, httpd.Client{MAC: fmt.Sprintf("aa:bb:cc:dd:ee:%02x", i), IP: fmt.Sprintf("10.0.0.%d", i), Product: "Box", LastSeen: seen.Add(-time.Duration(i) * 24 * time.Hour)})
	}
	_, out := r.do("GET", "/api/pcs?limit=10&offset=20", nil, nil)
	if out["total"].(float64) != 30 || len(out["pcs"].([]any)) != 10 || out["offset"].(float64) != 20 {
		t.Fatalf("%v", out)
	}
	if _, out = r.do("GET", "/api/pcs?limit=10&offset=999", nil, nil); len(out["pcs"].([]any)) != 0 {
		t.Fatalf("past the end: %v", out)
	}
	if _, out = r.do("GET", "/api/pcs?q=10.0.0.7", nil, nil); out["total"].(float64) != 1 {
		t.Fatalf("search: %v", out)
	}
	if code, out := r.do("POST", "/api/pcs/clean", map[string]any{"days": 10}, nil); code != 200 || out["removed"].(float64) != 20 {
		t.Fatalf("clean: %d %v", code, out)
	}
	if code, _ := r.do("POST", "/api/pcs/clean", map[string]any{"days": -1}, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _ := r.do("DELETE", "/api/pcs/aa:bb:cc:dd:ee:00", nil, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("DELETE", "/api/pcs/aa:bb:cc:dd:ee:00", nil, nil); code != 404 {
		t.Fatal(code)
	}
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"menuTimeout": 0, "default": "", "pcDays": 14}, nil); code != 200 || r.store.Get().PCDays != 14 {
		t.Fatal(code, r.store.Get().PCDays)
	}
	if code, _ := r.do("PUT", "/api/settings", map[string]any{"menuTimeout": 0, "default": ""}, nil); code != 200 || r.store.Get().PCDays != 14 {
		t.Fatal("a save that leaves pcDays out must keep it")
	}
}

func TestTheLogCanBeDownloadedOnlyAfterALogin(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do("GET", "/api/log/download", nil, nil); code != 401 {
		t.Fatalf("without a login: %d", code)
	}
	r.login()
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/log/download", nil)
	req.AddCookie(r.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body bytes.Buffer
	_, _ = body.ReadFrom(res.Body)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") || !strings.Contains(body.String(), "  first line\n") {
		t.Fatalf("%d %q %q", res.StatusCode, res.Header.Get("Content-Disposition"), body.String())
	}
}

func TestAPCCanBeNamedAndTheNameIsSearchedAndKeptWhenForgotten(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/pcs/not-a-mac/name", map[string]string{"name": "x"}, nil); code != 400 {
		t.Fatalf("a bad MAC: %d", code)
	}
	if code, _ := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/name", map[string]string{"name": "  Reception  "}, nil); code != 200 {
		t.Fatal(code)
	}
	_, out := r.do("GET", "/api/pcs?q=recep", nil, nil)
	pcs := out["pcs"].([]any)
	if len(pcs) != 1 || pcs[0].(map[string]any)["name"] != "Reception" {
		t.Fatalf("%v", out)
	}
	if code, _ := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/name", map[string]string{"name": "two\nlines"}, nil); code != 400 {
		t.Fatalf("a name with a line break: %d", code)
	}
	// forgotten from the list, the PC is still listed by its name
	r.do("DELETE", "/api/pcs/aa:bb:cc:dd:ee:01", nil, nil)
	_, out = r.do("GET", "/api/pcs", nil, nil)
	pcs = out["pcs"].([]any)
	if len(pcs) != 1 || pcs[0].(map[string]any)["name"] != "Reception" || pcs[0].(map[string]any)["mac"] != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("%v", out)
	}
	// an empty name takes it away
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/name", map[string]string{"name": ""}, nil)
	if _, out = r.do("GET", "/api/pcs", nil, nil); out["total"].(float64) != 0 {
		t.Fatalf("%v", out)
	}
}

func TestTuningAndPatternsAreSavedAndPatternsHideIsos(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/tuning", map[string]any{"tftpBlockSize": 100}, nil); code != 400 {
		t.Fatalf("a block size under 512: %d", code)
	}
	code, _ := r.do("PUT", "/api/tuning", map[string]any{"tftpBlockSize": 1024, "tftpWindow": 2, "hidePatterns": []string{" old/ ", "", "*beta*"}, "setupChecks": map[string]bool{"tpm": true, "ram": true}}, nil)
	if code != 200 || r.backend.rescans != 1 {
		t.Fatalf("%d, rescans %d", code, r.backend.rescans)
	}
	_, out := r.do("GET", "/api/tuning", nil, nil)
	if sc, _ := out["setupChecks"].(map[string]any); sc["tpm"] != true || sc["ram"] != true || sc["cpu"] != nil {
		t.Fatalf("the Windows 11 checks to skip are saved and read back: %v", out["setupChecks"])
	}
	pats := out["hidePatterns"].([]any)
	if out["tftpBlockSize"].(float64) != 1024 || out["tftpWindow"].(float64) != 2 || len(pats) != 2 || pats[0] != "old/" {
		t.Fatalf("%v", out)
	}
	if got := r.store.Get(); got.Tuning.TFTPBlockSize != 1024 || len(got.HidePatterns) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSelfTestRunsTheStepsAndReportsFailures(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/selftest", map[string]any{}, nil); code != 404 {
		t.Fatalf("without a target the self-test is not offered: %d", code)
	}
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("#!ipxe\n")) }))
	defer web.Close()
	host, port, _ := net.SplitHostPort(web.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	closed, _ := net.ListenPacket("udp4", "127.0.0.1:0") // a UDP port with nothing behind it (not 69: another computer program may use that one)
	_, cport, _ := net.SplitHostPort(closed.LocalAddr().String())
	closed.Close()
	tp, _ := strconv.Atoi(cport)
	r.admin.o.SelfTest = func() selftest.Target { return selftest.Target{IP: host, HTTP: p, TFTP: tp} }
	code, out := r.do("POST", "/api/selftest", map[string]any{}, nil)
	results, _ := out["results"].([]any)
	if code != 200 || len(results) != 6 {
		t.Fatalf("%d %v", code, out)
	}
	byID := map[string]string{}
	for _, x := range results {
		m := x.(map[string]any)
		byID[m["id"].(string)] = m["status"].(string)
	}
	if byID["menu"] != "ok" || byID["iso"] != "skipped" || byID["smb"] != "skipped" || byID["tftp"] != "fail" {
		t.Fatalf("%v", byID)
	}
	if out["failed"].(float64) != 1 {
		t.Fatalf("only the TFTP step (nothing on port 69) fails: %v", out)
	}
}

func TestLookingForOtherServersKeepsTheLastResult(t *testing.T) {
	r := newRig(t)
	r.login()
	if _, out := r.do("GET", "/api/pxecheck", nil, nil); out["available"] != false {
		t.Fatalf("without a checker: %v", out)
	}
	silent, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer silent.Close()
	free, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	listen := free.LocalAddr().String()
	free.Close()
	r.admin.o.PXE = &pxecheck.Checker{Options: func() pxecheck.Options {
		return pxecheck.Options{Local: net.ParseIP("127.0.0.1"), ListenAddr: listen, SendTo: silent.LocalAddr().String(), Wait: 300 * time.Millisecond}
	}}
	if _, out := r.do("GET", "/api/pxecheck", nil, nil); out["available"] != true || out["result"] != nil {
		t.Fatalf("before the first look: %v", out)
	}
	code, out := r.do("POST", "/api/pxecheck", map[string]any{}, nil)
	res, _ := out["result"].(map[string]any)
	if code != 200 || res == nil || len(res["answers"].([]any)) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	if _, out = r.do("GET", "/api/pxecheck", nil, nil); out["result"] == nil {
		t.Fatalf("the last result is kept: %v", out)
	}
}

func TestTheSpeedTestWaitsForPCsAndMeasuresTheLibraryAndTheCacheFolder(t *testing.T) {
	old := speedtest.MaxSize
	speedtest.MaxSize = 16 << 20
	defer func() { speedtest.MaxSize = old }()
	r := newRig(t)
	r.login()
	r.backend.cache = t.TempDir()
	r.do("POST", "/api/services/start", map[string]any{}, nil) // (they want to run and wait for a library)
	if code, _ := r.do("PUT", "/api/library", map[string]any{"path": t.TempDir(), "create": true}, nil); code != 200 {
		t.Fatal(code)
	}
	r.sup.Reconcile()
	if r.probe.starts == 0 {
		t.Fatal("the services did not start")
	}
	r.probe.sessions = []string{"10.0.0.5"}
	code, out := r.do("POST", "/api/speedtest", map[string]any{}, nil)
	if code != 409 || out["busy"] != true {
		t.Fatalf("PCs are using the server: %d %v", code, out)
	}
	code, out = r.do("POST", "/api/speedtest", map[string]any{"force": true}, nil)
	results, _ := out["results"].([]any)
	if code != 200 || len(results) != 2 {
		t.Fatalf("%d %v", code, out)
	}
	lib := results[0].(map[string]any)
	if lib["name"] != "The library" || lib["error"] != nil || lib["writeMBps"].(float64) <= 0 || lib["readMBps"].(float64) <= 0 {
		t.Errorf("the library: %v", lib)
	}
}

func TestTheFirstStartGuideIsOfferedUntilFinished(t *testing.T) {
	r := newRig(t)
	r.login()
	_ = r.store.Update(func(c *config.Config) error { c.PasswordChosen = false; return nil }) // (as after a password made up by the server)
	if _, out := r.do("GET", "/api/setup", nil, nil); out["done"] != false || out["passwordChosen"] != false {
		t.Fatalf("a new installation: %v", out)
	}
	if code, _ := r.do("POST", "/api/password", map[string]string{"current": "correct horse", "new": "a better password"}, nil); code != 200 {
		t.Fatal(code)
	}
	if _, out := r.do("GET", "/api/setup", nil, nil); out["passwordChosen"] != true {
		t.Fatalf("choosing a password is noted: %v", out)
	}
	if code, _ := r.do("POST", "/api/setup/finish", map[string]any{}, nil); code != 200 {
		t.Fatal(code)
	}
	if _, out := r.do("GET", "/api/setup", nil, nil); out["done"] != true {
		t.Fatalf("%v", out)
	}
}

func TestAPCsKernelOptionsAreSetShownAndTakenAway(t *testing.T) {
	r := newRig(t)
	r.login()
	body := map[string]any{"args": "  nomodeset  ", "debug": map[string]bool{"verbose": true}}
	if code, _ := r.do("PUT", "/api/pcs/not-a-mac/options", body, nil); code != 400 {
		t.Fatalf("a bad MAC: %d", code)
	}
	if code, _ := r.do("PUT", "/api/pcs/AA-BB-CC-DD-EE-01/options", body, nil); code != 200 {
		t.Fatal(code)
	}
	if o := r.store.Get().PCOptions["aa:bb:cc:dd:ee:01"]; o.Args != "nomodeset" || !o.Debug.Verbose || o.Debug.Serial {
		t.Fatalf("%+v", o)
	}
	_, out := r.do("GET", "/api/pcs", nil, nil)
	opts, _ := out["pcs"].([]any)[0].(map[string]any)["options"].(map[string]any)
	if opts["args"] != "nomodeset" || opts["debug"].(map[string]any)["verbose"] != true {
		t.Fatalf("the list of PCs must carry the options: %v", out)
	}
	if code, _ := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": "two\nlines"}, nil); code != 400 {
		t.Fatalf("arguments with a line break: %d", code)
	}
	// a PC that has options but has not asked since the server started is still listed
	r.do("DELETE", "/api/pcs/aa:bb:cc:dd:ee:01", nil, nil)
	if _, out = r.do("GET", "/api/pcs", nil, nil); len(out["pcs"].([]any)) != 1 {
		t.Fatalf("%v", out)
	}
	// nothing set takes them away
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": ""}, nil)
	if got := r.store.Get().PCOptions; len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAPCCanBeMarkedForTheSecureBootRouteAndItsOtherOptionsSurvive(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/pcs/not-a-mac/secureboot", map[string]any{"on": true}, nil); code != 400 {
		t.Fatalf("a bad MAC: %d", code)
	}
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": "nomodeset"}, nil)
	if code, _ := r.do("PUT", "/api/pcs/AA-BB-CC-DD-EE-01/secureboot", map[string]any{"on": true}, nil); code != 200 {
		t.Fatal(code)
	}
	if o := r.store.Get().PCOptions["aa:bb:cc:dd:ee:01"]; !o.SecureBoot || o.Args != "nomodeset" {
		t.Fatalf("the mark is set and the arguments stay: %+v", o)
	}
	// saving the kernel options (which the page does without the mark in the request) must not clear it
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": "quiet"}, nil)
	if o := r.store.Get().PCOptions["aa:bb:cc:dd:ee:01"]; !o.SecureBoot || o.Args != "quiet" {
		t.Fatalf("%+v", o)
	}
	_, out := r.do("GET", "/api/pcs", nil, nil)
	if opts, _ := out["pcs"].([]any)[0].(map[string]any)["options"].(map[string]any); opts["secureBoot"] != true {
		t.Fatalf("the list of PCs carries the mark: %v", out)
	}
	if out["secureBoot"] != "" {
		t.Fatalf("the route is off in this rig: %v", out["secureBoot"])
	}
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/secureboot", map[string]any{"on": false}, nil)
	r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": ""}, nil)
	if got := r.store.Get().PCOptions; len(got) != 0 {
		t.Fatalf("nothing set drops the entry: %+v", got)
	}
}
