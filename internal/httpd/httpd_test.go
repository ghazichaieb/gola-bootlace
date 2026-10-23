package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func server(t *testing.T) (*httptest.Server, string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "My Linux.iso"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{ISODir: dir, Base: "http://10.0.0.1:8080"}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func get(t *testing.T, url string, header map[string]string) (int, string) {
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestTheMenuListsTheIsosAndBootsThemOverHTTP(t *testing.T) {
	ts, _ := server(t)
	code, body := get(t, ts.URL+"/boot.ipxe", nil)
	if code != 200 || !strings.Contains(body, "chain http://10.0.0.1:8080/menu?mac=${mac}") {
		t.Fatalf("the boot script must ask for the menu with the PC's identity: %d %q", code, body)
	}
	code, body = get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:ff&ip=10.0.0.9", nil)
	if code != 200 || !strings.HasPrefix(body, "#!ipxe") || !strings.Contains(body, "My Linux") || !strings.Contains(body, "sanboot --no-describe --drive 0x80 http://10.0.0.1:8080/iso/My%20Linux.iso") || !strings.Contains(body, "as a disk") {
		t.Fatalf("%d %q", code, body)
	}
}

func TestIsosAreServedWithRangesAndUnknownNamesAreRefused(t *testing.T) {
	ts, _ := server(t)
	if code, body := get(t, ts.URL+"/iso/My%20Linux.iso", nil); code != 200 || body != "0123456789" {
		t.Fatalf("%d %q", code, body)
	}
	if code, body := get(t, ts.URL+"/iso/My%20Linux.iso", map[string]string{"Range": "bytes=2-4"}); code != 206 || body != "234" {
		t.Fatalf("range: %d %q", code, body)
	}
	for _, name := range []string{"/iso/missing.iso", "/iso/../x", "/iso/"} {
		if code, _ := get(t, ts.URL+name, nil); code != 404 && code != 301 {
			t.Fatalf("%s: %d", name, code)
		}
	}
}

func TestFreeBSDsLoaderConfigurationGetsTheMemoryDiskLines(t *testing.T) {
	got := string(loaderConf([]byte("autoboot_delay=\"2\"")))
	for _, want := range []string{"autoboot_delay=\"2\"\n", "mdroot_load=\"YES\"", "mdroot_type=\"md_image\"", "mdroot_name=\"/boot.iso\"", "vfs.root.mountfrom=\"cd9660:/dev/md0\"", "efi.rt.disabled=\"1\""} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if !isLoaderName("boot/loader.efi") || !isLoaderName("boot/pxeboot") || isLoaderName("boot/kernel/kernel") {
		t.Error("only the two loaders say which ISO a PC chose")
	}
}

// While the server is stopped the PCs get "stopped", the web interface and the health check still answer, and with no library nothing is listed.
func TestAStoppedServerAnswersOnlyTheWebInterface(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.iso"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	active := true
	s := &Server{ISODir: dir, Base: "http://10.0.0.1:8080", Active: func() bool { return active },
		Mount: map[string]http.Handler{"/admin/": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "page") })}}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	if code, _ := get(t, ts.URL+"/iso/a.iso", nil); code != 200 {
		t.Fatalf("running: %d", code)
	}
	active = false
	for _, p := range []string{"/iso/a.iso", "/boot.ipxe", "/menu"} {
		if code, _ := get(t, ts.URL+p, nil); code != 503 {
			t.Errorf("%s while stopped: %d", p, code)
		}
	}
	if code, body := get(t, ts.URL+"/admin/", nil); code != 200 || body != "page" {
		t.Errorf("the web interface while stopped: %d %q", code, body)
	}
	if code, _ := get(t, ts.URL+"/health", nil); code != 200 {
		t.Errorf("health while stopped: %d", code)
	}
}

func TestNoLibraryListsNothing(t *testing.T) {
	s := &Server{Library: library.New("", ""), Base: "http://10.0.0.1:8080"}
	if list, err := s.scan(); err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}

// The web server answers the local network only, the web interface included, unless the policy says otherwise.
func TestAddressesOutsideTheLocalNetworkAreRefused(t *testing.T) {
	policy := access.New(nil)
	s := &Server{ISODir: t.TempDir(), Base: "http://10.0.0.1:8080", Access: policy,
		Mount: map[string]http.Handler{"/admin/": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "page") })}}
	h := s.Handler()
	ask := func(from, path string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = from + ":5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, path := range []string{"/boot.ipxe", "/admin/", "/health", "/iso/x.iso"} {
		if code := ask("203.0.113.9", path); code != 403 {
			t.Errorf("a public address got %d for %s", code, path)
		}
		if code := ask("192.168.1.20", path); code == 403 {
			t.Errorf("a local address was refused for %s", path)
		}
	}
	_ = policy.Set([]string{"203.0.113.0/24"}, false)
	if code := ask("203.0.113.9", "/health"); code != 200 {
		t.Errorf("an allowed network was refused: %d", code)
	}
	if st := policy.Stats(); st.Refused != 4 {
		t.Errorf("refusals counted: %d", st.Refused)
	}
}

// New cache entries go to the container's own folder while it has room, then to the library's; an entry is found in either.
func TestTheCacheSpillsFromTheContainerToTheLibrary(t *testing.T) {
	lib := t.TempDir()
	id, err := library.Prepare(lib)
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	where, limit := "auto", int64(1000)
	s := &Server{Library: library.New(lib, id), CacheDir: local, CacheSettings: func() (string, int64) { return where, limit }}
	libCache := filepath.Join(lib, library.MetaDir, "cache")
	if got := s.cachePlace(500); got != local {
		t.Fatalf("with room: %s", got)
	}
	if err := os.WriteFile(filepath.Join(local, "boot-a.wim"), make([]byte, 800), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.cachePlace(500); got != libCache {
		t.Fatalf("without room it must spill to the library: %s", got)
	}
	if got := s.cachePlace(100); got != local {
		t.Fatalf("a small entry still fits: %s", got)
	}
	where = "library"
	if got := s.cachePlace(1); got != libCache {
		t.Fatalf("library only: %s", got)
	}
	where = "local"
	if got := s.cachePlace(1 << 40); got != local {
		t.Fatalf("local only: %s", got)
	}
	if err := os.MkdirAll(libCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libCache, "boot-b.wim"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"boot-a.wim": local, "boot-b.wim": libCache} {
		if p, ok := s.cacheFind(name); !ok || filepath.Dir(p) != want {
			t.Errorf("%s: %s %v", name, p, ok)
		}
	}
	if u := s.CacheUsage(); u.LocalBytes != 800 || u.LibraryBytes != 50 || u.LibraryDir != libCache {
		t.Errorf("usage: %+v", u)
	}
	// a library that is not there is never written to, whatever the setting says
	os.RemoveAll(filepath.Join(lib, library.MetaDir))
	s.libCacheAt = s.libCacheAt.Add(-time.Hour) // (the answer is kept for a couple of seconds)
	where = "library"
	if got := s.cachePlace(1); got != local {
		t.Fatalf("the library is gone: %s", got)
	}
}

func TestTheNameAPCOffersIsKeptAndANamedPCIsNamedInTheLog(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/menu?mac=AA-BB-CC-DD-EE-0F&ip=10.0.0.9&host=desk-12&asset=A%0A1", nil)
	req.RemoteAddr = "10.0.0.9:5555"
	c := s.noteClient(req)
	if c.MAC != "aa:bb:cc:dd:ee:0f" || c.Hostname != "desk-12" || strings.ContainsAny(c.Asset, "\r\n") {
		t.Fatalf("%+v", c)
	}
	// a later request that does not say it must not wipe what an earlier one told
	req = httptest.NewRequest("GET", "/menu?mac=aa:bb:cc:dd:ee:0f", nil)
	req.RemoteAddr = "10.0.0.9:5555"
	if c = s.noteClient(req); c.Hostname != "desk-12" {
		t.Fatalf("%+v", c)
	}
	if got := s.who(c); got != "aa:bb:cc:dd:ee:0f (10.0.0.9)" {
		t.Fatal(got)
	}
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s.Config = store
	_ = store.Update(func(cfg *config.Config) error { cfg.PCNames = map[string]string{c.MAC: "Reception"}; return nil })
	if got := s.who(c); got != "Reception (aa:bb:cc:dd:ee:0f, 10.0.0.9)" {
		t.Fatal(got)
	}
}
