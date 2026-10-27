package admin

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/backup"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
)

// ownFolder is a folder for the own-key way with the shim and the second stage in it.
func ownFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"shimx64.efi", "grubx64.efi"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// logText is everything in the web interface's log.
func logText(r *rig) string {
	lines, _ := r.log.After(0, 1000)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

func TestTheSecureBootRouteIsChangedThroughTheApi(t *testing.T) {
	dir := t.TempDir()
	r := newRealRig(t, httpd.SecureBoot{OwnDir: dir}) // an installation from before the setting existed: nothing saved
	code, out := r.do("GET", "/api/secureboot", nil, nil)
	if code != 200 || out["on"] != false || out["mode"] != "off" || out["scope"] != "marked" || out["unset"] != true || out["modeSource"] != "default" || out["modeLocked"] != false {
		t.Fatalf("%d %v", code, out)
	}
	builtin, _ := out["builtin"].(map[string]any)
	files, _ := builtin["files"].([]any)
	if builtin["ok"] != true || builtin["release"] != "v2.0.0" || len(files) != 2 || files[0].(map[string]any)["sha256"] == "" || files[0].(map[string]any)["size"].(float64) < 100000 {
		t.Fatalf("the built-in files are described: %v", builtin)
	}
	if own, _ := out["own"].(map[string]any); own["dir"] != dir || own["ready"] != false {
		t.Fatalf("the own-key folder is described: %v", own)
	}

	// nothing but a login, and the header other sites cannot add
	anon := &rig{t: t, ts: r.ts}
	if code, _ := anon.do("PUT", "/api/secureboot", map[string]any{"mode": "builtin"}, nil); code != 401 {
		t.Fatalf("without a login: %d", code)
	}
	if code, _ := r.do("PUT", "/api/secureboot", map[string]any{"mode": "builtin"}, map[string]string{"X-Bootlace": ""}); code != 403 {
		t.Fatalf("without the header: %d", code)
	}
	if mode, _, _ := r.store.SecureBoot(); mode != "" {
		t.Fatalf("a refused request saved something: %q", mode)
	}

	// validation: unknown values are a bad request, a mode that cannot be served is a conflict
	for name, body := range map[string]map[string]any{"nothing": {}, "an unknown mode": {"mode": "on"}, "an unknown scope": {"scope": "some"}, "a number": {"mode": 1}} {
		if code, out := r.do("PUT", "/api/secureboot", body, nil); code != 400 {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if code, out := r.do("PUT", "/api/secureboot", map[string]any{"mode": "own"}, nil); code != 409 || !strings.Contains(out["error"].(string), "shimx64.efi") {
		t.Fatalf("own with an empty folder: %d %v", code, out)
	}
	if mode, _, _ := r.store.SecureBoot(); mode != "" {
		t.Fatalf("a refused change saved something: %q", mode)
	}

	// the switch, and what the DHCP answer says at once
	mac := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x05}
	req, _ := discover(t, dhcp.Discover, mac, 7, "0.0.0.0")
	if reply, ok := r.boot.Reply(req); !ok || reply.File != "ipxe.efi" {
		t.Fatalf("off: %q", replyFile(reply))
	}
	code, out = r.do("PUT", "/api/secureboot", map[string]any{"mode": "builtin", "scope": "all"}, nil)
	if code != 200 || out["on"] != true || out["ready"] != true || out["mode"] != "builtin" || out["scope"] != "all" || out["modeSource"] != "dashboard" || out["unset"] != false || out["shim"] != "ipxe-shimx64.efi" {
		t.Fatalf("%d %v", code, out)
	}
	if reply, ok := r.boot.Reply(req); !ok || reply.File != "sb/ipxe-shimx64.efi" {
		t.Fatalf("built in, all PCs: %q", replyFile(reply))
	}
	if mode, scope, chosen := r.store.SecureBoot(); mode != "builtin" || scope != "all" || !chosen {
		t.Fatalf("saved: %q %q %v", mode, scope, chosen)
	}
	if code, out = r.do("PUT", "/api/secureboot", map[string]any{"scope": "marked"}, nil); code != 200 || out["mode"] != "builtin" || out["scope"] != "marked" {
		t.Fatalf("%d %v", code, out)
	}
	if reply, _ := r.boot.Reply(req); reply.File != "ipxe.efi" {
		t.Fatalf("marked, and this PC is not: %q", replyFile(reply))
	}
	if code, out = r.do("PUT", "/api/secureboot", map[string]any{"mode": "off"}, nil); code != 200 || out["on"] != false || !strings.Contains(out["message"].(string), "off") {
		t.Fatalf("%d %v", code, out)
	}
	if !strings.Contains(logText(r.rig), "Secure Boot route: mode off") {
		t.Errorf("a change is in the log")
	}
}

func TestWhatTheCommandLineDecidesCannotBeChangedFromThePage(t *testing.T) {
	r := newRealRig(t, httpd.SecureBoot{Dir: ownFolder(t), Scope: "marked"})
	code, out := r.do("GET", "/api/secureboot", nil, nil)
	if code != 200 || out["mode"] != "own" || out["modeSource"] != "option" || out["modeLocked"] != true || out["scopeLocked"] != true || out["unset"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := r.do("PUT", "/api/secureboot", map[string]any{"mode": "builtin"}, nil); code != 409 || !strings.Contains(out["error"].(string), "command line") {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("PUT", "/api/secureboot", map[string]any{"scope": "all"}, nil); code != 409 {
		t.Fatalf("scope: %d", code)
	}
	if mode, scope, _ := r.store.SecureBoot(); mode != "" || scope != "" {
		t.Fatalf("an option is not copied into the saved settings: %q %q", mode, scope)
	}
}

// The DHCP answer per mode and scope, through the real route: 64-bit UEFI PCs (architecture codes 7 and 9) go through the shim when the mode and scope say so; BIOS, ARM64,
// a PC that already runs iPXE and a PC with a Windows hand-off in progress never do.
func TestTheDHCPAnswerFollowsTheModeAndTheScope(t *testing.T) {
	mac := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x05}
	other := [6]byte{0xd8, 0x43, 0xae, 0xd6, 0x2f, 0x06}
	for _, tc := range []struct {
		name        string
		opts        httpd.SecureBoot
		mode, scope string
		markedPC    bool
		wantUEFI    string // for the PC with the first MAC
		wantOther   string // for the PC with the second MAC (never marked)
	}{
		{"built in, all", httpd.SecureBoot{}, "builtin", "all", false, "sb/ipxe-shimx64.efi", "sb/ipxe-shimx64.efi"},
		{"built in, marked, the PC is marked", httpd.SecureBoot{}, "builtin", "marked", true, "sb/ipxe-shimx64.efi", "ipxe.efi"},
		{"built in, marked, no PC is", httpd.SecureBoot{}, "builtin", "marked", false, "ipxe.efi", "ipxe.efi"},
		{"off, all", httpd.SecureBoot{}, "off", "all", true, "ipxe.efi", "ipxe.efi"},
		{"own key, all", httpd.SecureBoot{Dir: ownFolder(t)}, "builtin", "all", false, "sb/shimx64.efi", "sb/shimx64.efi"}, // (the folder option decides the mode)
		{"own key chosen in the page, marked", httpd.SecureBoot{OwnDir: ownFolder(t)}, "own", "marked", true, "sb/shimx64.efi", "ipxe.efi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealRig(t, tc.opts)
			r.boot.Files[dhcp.ArchARM64] = "arm.efi"
			if err := r.store.Update(func(c *config.Config) error {
				c.SecureBootMode, c.SecureBootScope, c.SecureBootChosen = tc.mode, tc.scope, true
				if tc.markedPC {
					c.PCOptions = map[string]config.PCOptions{"d8:43:ae:d6:2f:05": {SecureBoot: true}}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			file := func(m [6]byte, arch uint16, mutate func(*dhcp.Packet)) string {
				req, _ := discover(t, dhcp.Discover, m, arch, "0.0.0.0")
				if mutate != nil {
					mutate(req)
				}
				reply, ok := r.boot.Reply(req)
				if !ok {
					t.Fatalf("no answer for arch %d", arch)
				}
				return reply.File
			}
			for _, arch := range []uint16{7, 9} {
				if got := file(mac, arch, nil); got != tc.wantUEFI {
					t.Errorf("arch %d: %q, want %q", arch, got, tc.wantUEFI)
				}
				if got := file(other, arch, nil); got != tc.wantOther {
					t.Errorf("another PC, arch %d: %q, want %q", arch, got, tc.wantOther)
				}
			}
			if got := file(mac, 0, nil); got != "undionly.kpxe" {
				t.Errorf("a BIOS PC has no Secure Boot: %q", got)
			}
			for _, arch := range []uint16{11, 19} {
				if got := file(mac, arch, nil); got != "arm.efi" {
					t.Errorf("an x86-64 shim is never sent to an ARM64 PC (arch %d): %q", arch, got)
				}
			}
			if got := file(mac, 7, func(p *dhcp.Packet) { p.Opts[dhcp.OptUserClass] = []byte("iPXE") }); got != "http://10.0.0.1:8080/boot.ipxe" {
				t.Errorf("a PC that runs iPXE already gets its script, not the shim again: %q", got)
			}
			// a Windows start in progress (the firmware's own boot manager) wins over the shim
			r.boot.Handoff = func(string, bool) (string, bool) { return "win/abc/bootmgfw.efi", true }
			if got := file(mac, 7, nil); got != "win/abc/bootmgfw.efi" {
				t.Errorf("a native start is not sent through the shim: %q", got)
			}
		})
	}
}

func TestThePCsPageKnowsHowTheRouteReachesPCs(t *testing.T) {
	r := newRealRig(t, httpd.SecureBoot{})
	for _, tc := range []struct {
		mode, scope, want string
	}{{"off", "marked", ""}, {"builtin", "marked", "pcs"}, {"builtin", "all", "all"}} {
		if err := r.store.Update(func(c *config.Config) error { c.SecureBootMode, c.SecureBootScope = tc.mode, tc.scope; return nil }); err != nil {
			t.Fatal(err)
		}
		if code, out := r.do("GET", "/api/pcs", nil, nil); code != 200 || out["secureBoot"] != tc.want {
			t.Errorf("%s/%s: %d %v, want %q", tc.mode, tc.scope, code, out["secureBoot"], tc.want)
		}
	}
}

// The mode, the scope and the answer to "did a person choose" are settings like the others: they are in a backup, come back with a restore, and a backup from before they
// existed leaves an installation off with the question open.
func TestTheSecureBootSettingsAreInBackups(t *testing.T) {
	r := newRig(t)
	r.login()
	if err := r.store.Update(func(c *config.Config) error {
		c.SecureBootMode, c.SecureBootScope, c.SecureBootChosen = "own", "marked", true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code, plain := r.raw("GET", "/api/backup/download", nil)
	if code != 200 {
		t.Fatal(code)
	}
	c, err := backup.ReadZip(plain)
	if err != nil || c.Config.SecureBootMode != "own" || c.Config.SecureBootScope != "marked" || !c.Config.SecureBootChosen {
		t.Fatalf("%+v %v", c.Config, err)
	}
	if err := r.store.Update(func(c *config.Config) error {
		c.SecureBootMode, c.SecureBootScope, c.SecureBootChosen = "builtin", "all", false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if code, body := r.raw("POST", "/api/backup/restore", plain); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if mode, scope, chosen := r.store.SecureBoot(); mode != "own" || scope != "marked" || !chosen {
		t.Fatalf("restored: %q %q %v", mode, scope, chosen)
	}

	// a backup made before the settings existed
	var old bytes.Buffer
	if err := backup.WriteZip(&old, config.Config{MenuTimeout: 20, PCDays: 30, Default: "local"}, nil, false, "0.5.0", time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, body := r.raw("POST", "/api/backup/restore", old.Bytes()); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if mode, scope, chosen := r.store.SecureBoot(); mode != "" || scope != "" || chosen {
		t.Fatalf("an old backup has no Secure Boot setting: %q %q %v", mode, scope, chosen)
	}
}
