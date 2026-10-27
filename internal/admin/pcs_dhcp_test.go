package admin

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
)

// realRig is the web interface on top of the real server (not the fake backend of newRig) and the real DHCP answer, wired as cmd/bootlace wires them.
type realRig struct {
	*rig
	web  *httpd.Server
	boot dhcp.Boot
	dir  string
}

func newRealRig(t *testing.T, sb httpd.SecureBoot) *realRig {
	t.Helper()
	dir := t.TempDir()
	store, _, err := config.Open(filepath.Join(dir, "c.json"), config.Config{PCDays: config.DefaultPCDays})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePassword(store, "correct horse"); err != nil {
		t.Fatal(err)
	}
	logs := logbuf.New(100)
	web := &httpd.Server{Config: store, PCFile: filepath.Join(dir, "pcs.json"), Base: "http://10.0.0.1:8080", Logf: logs.Logf, SecureBoot: sb}
	lib := library.New("", "")
	indexer := &library.Indexer{Lib: lib}
	a := New(Options{Store: store, Log: logs, Backend: web, Library: lib, Services: &service.Supervisor{Gate: lib.Check}, Access: access.New(nil), ServerIP: "10.0.0.1",
		Importer: importer.New(lib, indexer.Get, nil), Indexer: indexer, SecureBootStatus: web.SecureBootStatus, SecureBootSet: web.SetSecureBoot})
	mux := http.NewServeMux()
	for p, h := range a.Mount() {
		mux.Handle(p, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	boot := dhcp.Boot{ServerIP: net.IPv4(10, 0, 0, 1), HTTPBase: "http://10.0.0.1:8080", Script: "http://10.0.0.1:8080/boot.ipxe", Shim: web.SecureBootFile, Seen: web.NoteDHCP,
		Files: map[dhcp.Arch]string{dhcp.ArchBIOS: "undionly.kpxe", dhcp.ArchX64EFI: "ipxe.efi"}}
	r := &rig{t: t, ts: ts, store: store, log: logs, admin: a}
	rr := &realRig{rig: r, web: web, boot: boot, dir: dir}
	r.login()
	return rr
}

// discover is the DHCP discover (or request) of a PC's firmware doing a network boot, as a real packet on the wire and back.
func discover(t *testing.T, msg byte, mac [6]byte, arch uint16, src string) (*dhcp.Packet, *dhcp.Packet) {
	t.Helper()
	p := &dhcp.Packet{Op: 1, HType: 1, HLen: 6, XID: 0x1234, Flags: 0x8000, CIAddr: net.ParseIP(src),
		Opts: map[byte][]byte{dhcp.OptMessageType: {msg}, dhcp.OptVendorClass: []byte("PXEClient:Arch:00007:UNDI:003016"), dhcp.OptClientArch: {byte(arch >> 8), byte(arch)},
			12: []byte("MSI-PC"), 61: append([]byte{1}, mac[:]...)}}
	copy(p.CHAddr[:], mac[:])
	req, err := dhcp.Parse(p.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	return req, nil
}

func pcRows(t *testing.T, r *rig, path string) []any {
	t.Helper()
	code, out := r.do("GET", path, nil, nil)
	if code != 200 {
		t.Fatalf("%s: %d %v", path, code, out)
	}
	list, _ := out["pcs"].([]any)
	return list
}

// A PC that only asked for a boot file (its firmware refuses the iPXE it was sent) is listed on the PCs page, whatever its architecture code, the address it asked from, and
// whether the proxy answer names iPXE or the shim (issue #1 of the Secure Boot route: the test PC was not listed).
func TestAPCThatOnlyAskedForABootFileIsListedOnThePCsPage(t *testing.T) {
	sbDir := t.TempDir()
	for _, n := range []string{"ipxe-shimx64.efi", "ipxe.efi"} {
		if err := os.WriteFile(filepath.Join(sbDir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mac := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x05}
	for _, tc := range []struct {
		name string
		sb   httpd.SecureBoot
		msg  byte
		arch uint16
		src  string
		file string
	}{
		{"discover from 0.0.0.0, route off", httpd.SecureBoot{}, dhcp.Discover, 7, "0.0.0.0", "ipxe.efi"},
		{"request, arch 9", httpd.SecureBoot{}, dhcp.Request, 9, "10.0.0.50", "ipxe.efi"},
		{"arch 16 (HTTP-boot code)", httpd.SecureBoot{}, dhcp.Discover, 16, "0.0.0.0", "ipxe.efi"},
		{"route on, all PCs", httpd.SecureBoot{Dir: sbDir, Shim: "ipxe-shimx64.efi", All: true}, dhcp.Discover, 7, "0.0.0.0", "sb/ipxe-shimx64.efi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealRig(t, tc.sb)
			req, _ := discover(t, tc.msg, mac, tc.arch, tc.src)
			reply, ok := r.boot.Reply(req)
			if !ok || reply.File != tc.file {
				t.Fatalf("the answer: %v %q, want %q", ok, replyFile(reply), tc.file)
			}
			rows := pcRows(t, r.rig, "/api/pcs")
			if len(rows) != 1 {
				t.Fatalf("the PCs page lists %d PCs, want the one that asked for a boot file: %v", len(rows), rows)
			}
			row := rows[0].(map[string]any)
			if row["mac"] != "d8:43:ae:d6:2f:05" {
				t.Fatalf("listed as %v", row["mac"])
			}
			// the same with the page's own paging and search
			if n := len(pcRows(t, r.rig, "/api/pcs?limit=25&offset=0&q=")); n != 1 {
				t.Fatalf("with the page's paging: %d", n)
			}
			if n := len(pcRows(t, r.rig, "/api/pcs?q=d8:43")); n != 1 {
				t.Fatalf("searched for by MAC: %d", n)
			}
		})
	}
}

func replyFile(p *dhcp.Packet) string {
	if p == nil {
		return ""
	}
	return p.File
}

// The PC that only asked for a boot file says so, can be marked for the Secure Boot route from its row, and is sent through the shim at its next boot file request.
func TestAPCThatOnlyAskedForABootFileIsLabelledAndCanBeMarkedForTheSecureBootRoute(t *testing.T) {
	sbDir := t.TempDir()
	for _, n := range []string{"ipxe-shimx64.efi", "ipxe.efi"} {
		if err := os.WriteFile(filepath.Join(sbDir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := newRealRig(t, httpd.SecureBoot{Dir: sbDir, Shim: "ipxe-shimx64.efi"})
	mac := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x05}
	req, _ := discover(t, dhcp.Discover, mac, 7, "0.0.0.0")
	if reply, ok := r.boot.Reply(req); !ok || reply.File != "ipxe.efi" {
		t.Fatalf("an unmarked PC gets plain iPXE: %v %q", ok, replyFile(reply))
	}
	row := pcRows(t, r.rig, "/api/pcs")[0].(map[string]any)
	hint, _ := row["hint"].(string)
	if !strings.Contains(hint, "Asked for a boot file; Secure Boot may be refusing iPXE") || !strings.Contains(hint, `press "Secure Boot route"`) {
		t.Fatalf("hint %q", hint)
	}
	if code, out := r.do("PUT", "/api/pcs/d8:43:ae:d6:2f:05/secureboot", map[string]any{"on": true}, nil); code != 200 {
		t.Fatalf("the button: %d %v", code, out)
	}
	if reply, ok := r.boot.Reply(req); !ok || reply.File != "sb/ipxe-shimx64.efi" {
		t.Fatalf("a marked PC is sent through the shim: %v %q", ok, replyFile(reply))
	}
	row = pcRows(t, r.rig, "/api/pcs")[0].(map[string]any)
	if hint, _ = row["hint"].(string); !strings.Contains(hint, "sent through the signed shim already") {
		t.Fatalf("hint after marking: %q", hint)
	}
	if opts, _ := row["options"].(map[string]any); opts["secureBoot"] != true {
		t.Fatalf("options %v", row["options"])
	}
}

func TestThePCsPageHintWhenTheRouteIsOffAndTheStatusEndpoint(t *testing.T) {
	r := newRealRig(t, httpd.SecureBoot{})
	mac := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x05}
	req, _ := discover(t, dhcp.Request, mac, 7, "10.0.0.50")
	r.boot.Reply(req)
	row := pcRows(t, r.rig, "/api/pcs")[0].(map[string]any)
	if hint, _ := row["hint"].(string); !strings.Contains(hint, "turn the Secure Boot route on") {
		t.Fatalf("hint %q", hint)
	}
	code, out := r.do("GET", "/api/secureboot", nil, nil)
	if code != 200 || out["on"] != false || out["mode"] != "off" || out["unset"] != true || !strings.Contains(out["message"].(string), "Built in") {
		t.Fatalf("%d %v", code, out)
	}
	if _, st := r.do("GET", "/api/status", nil, nil); st["secureBoot"] == nil {
		t.Fatalf("the status carries it too: %v", st)
	}
}

func TestTheSecureBootEndpointListsTheFilesAndWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ipxe-shimx64.efi"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRealRig(t, httpd.SecureBoot{Dir: dir, Shim: "ipxe-shimx64.efi", All: true})
	code, out := r.do("GET", "/api/secureboot", nil, nil)
	if code != 200 || out["on"] != true || out["mode"] != "own" || out["scope"] != "all" || out["ready"] != false || out["dir"] != dir || !strings.Contains(out["problem"].(string), "ipxe.efi is missing") {
		t.Fatalf("%d %v", code, out)
	}
	files, _ := out["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["present"] != true || files[1].(map[string]any)["name"] != "ipxe.efi" || files[1].(map[string]any)["present"] != false {
		t.Fatalf("files %v", files)
	}
	// nothing but a login opens it
	anon := &rig{t: t, ts: r.ts}
	if code, _ := anon.do("GET", "/api/secureboot", nil, nil); code != 401 {
		t.Fatalf("without a login: %d", code)
	}
}
