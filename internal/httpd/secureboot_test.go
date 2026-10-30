package httpd

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/secureboot"
)

// sbFolder is a folder as the own-key way wants it: a shim and the second stage (what they hold does not matter here).
func sbFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"shimx64.efi": "SHIM", "grubx64.efi": "IPXE", "mmx64.efi": "MOK", "notes.txt": "x"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// saved is the Secure Boot setting kept in a settings file: how a person (chosen) or the seed of a new installation (not chosen) left it.
type saved struct {
	mode, scope string
	chosen      bool
}

// sbServer is a server with the options it was started with, a settings file that holds `saved`, and the PCs marked for the route.
func sbServer(t *testing.T, opts SecureBoot, st saved, marked ...string) *Server {
	t.Helper()
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error {
		c.SecureBootMode, c.SecureBootScope, c.SecureBootChosen = st.mode, st.scope, st.chosen
		if len(marked) > 0 {
			c.PCOptions = map[string]config.PCOptions{}
			for _, m := range marked {
				c.PCOptions[m] = config.PCOptions{SecureBoot: true}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return &Server{Config: store, SecureBoot: opts}
}

var (
	freshDefault  = saved{"builtin", "all", false}   // what a new installation starts with
	builtinMarked = saved{"builtin", "marked", true} // chosen in the web interface
	ownAll        = saved{"own", "all", true}
	offChosen     = saved{"off", "marked", true}
)

func TestTheSettingIsTheOptionsThenTheSavedChoiceThenTheDefault(t *testing.T) {
	type want struct {
		mode, modeSrc, scope, scopeSrc string
		unset                          bool
	}
	for name, tc := range map[string]struct {
		opts SecureBoot
		st   saved
		want want
	}{
		"nothing given, nothing saved (an older settings file)": {SecureBoot{}, saved{}, want{"off", "default", "marked", "default", true}},
		"a new installation's seed":                             {SecureBoot{}, freshDefault, want{"builtin", "default", "all", "default", false}},
		"chosen in the web interface":                           {SecureBoot{}, builtinMarked, want{"builtin", "dashboard", "marked", "dashboard", false}},
		"a mode saved without a scope is marked":                {SecureBoot{}, saved{"builtin", "", true}, want{"builtin", "dashboard", "marked", "default", false}},
		"a folder means the own key way, whatever was saved":    {SecureBoot{Dir: "/sb"}, freshDefault, want{"own", "option", "all", "default", false}},
		"-secure-boot wins over the saved mode":                 {SecureBoot{Mode: "off"}, freshDefault, want{"off", "option", "all", "default", false}},
		"-secure-boot wins over a folder":                       {SecureBoot{Mode: "builtin", Dir: "/sb"}, saved{}, want{"builtin", "option", "marked", "default", false}},
		"-secure-boot-all":                                      {SecureBoot{All: true}, builtinMarked, want{"builtin", "dashboard", "all", "option", false}},
		"-secure-boot-scope wins over the saved scope":          {SecureBoot{Scope: "marked"}, freshDefault, want{"builtin", "default", "marked", "option", false}},
		"-secure-boot-scope wins over -secure-boot-all":         {SecureBoot{Scope: "marked", All: true}, saved{}, want{"off", "default", "marked", "option", true}},
		"every option": {SecureBoot{Mode: "own", Scope: "all"}, offChosen, want{"own", "option", "all", "option", false}},
	} {
		got := tc.opts.resolve(tc.st.mode, tc.st.scope, tc.st.chosen)
		if got.Mode != tc.want.mode || got.ModeSource != tc.want.modeSrc || got.Scope != tc.want.scope || got.ScopeSource != tc.want.scopeSrc || got.Unset != tc.want.unset {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
	r := SecureBoot{OwnDir: "/data/secure-boot"}.resolve("own", "all", true)
	if r.Dir != "/data/secure-boot" || r.DirFromOption || r.Shim != "shimx64.efi" {
		t.Errorf("the default folder of the own key way: %+v", r)
	}
	if r = (SecureBoot{OwnDir: "/data/secure-boot", Dir: "/mine", Shim: "ipxe-shimx64.efi"}).resolve("", "", false); r.Dir != "/mine" || !r.DirFromOption || r.Shim != "ipxe-shimx64.efi" {
		t.Errorf("a folder given: %+v", r)
	}
}

func TestOptionsThatAreNotChoicesAreRefusedAndAFolderMustHoldTheShim(t *testing.T) {
	dir := sbFolder(t)
	for _, ok := range []SecureBoot{{}, {Mode: "builtin"}, {Mode: "off", Scope: "all"}, {Dir: dir}, {Mode: "own"}, {Mode: "builtin", Dir: filepath.Join(dir, "nope")}, {Mode: "off", Dir: dir, Shim: "../x"}} {
		if err := ok.Check(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []SecureBoot{{Mode: "yes"}, {Scope: "some"}, {Dir: filepath.Join(dir, "nope")}, {Dir: dir, Shim: "other.efi"}, {Dir: dir, Shim: "sub"}, {Dir: dir, Shim: "../x"}, {Mode: "own", Dir: dir, Shim: "other.efi"}} {
		if err := bad.Check(); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestAnOlderSettingsFileStaysOffAndAsks(t *testing.T) {
	// a settings file as the version before the route was built in wrote it: no Secure Boot keys at all
	path := filepath.Join(t.TempDir(), "bootlace.json")
	if err := os.WriteFile(path, []byte(`{"menuTimeout":20,"pcDays":30,"default":"local","hidden":{},"titles":{},"passwordChosen":true,"adminHash":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, created, err := config.Open(path, config.Config{SecureBootMode: "builtin", SecureBootScope: "all"}) // (a seed is only used for a file that does not exist)
	if err != nil || created {
		t.Fatal(created, err)
	}
	s := &Server{Config: store}
	st := s.SecureBootStatus()
	if st.On || st.Mode != "off" || st.Scope != "marked" || !st.Unset || st.ModeSource != SourceDefault || !strings.Contains(st.Message, "Built in") {
		t.Fatalf("an upgrade changes no behaviour and offers the switch: %+v", st)
	}
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("no PC is sent through the shim")
	}
	if f, ok := s.TFTPLookup(net.IPv4(10, 0, 0, 5), "sb/ipxe-shimx64.efi"); ok {
		f.Close()
		t.Fatal("nothing is served")
	}
	if lines := strings.Join(s.SecureBootStartLines(), "\n"); !strings.Contains(lines, "Secure Boot route: OFF") || !strings.Contains(lines, "turn it on on the Server page") {
		t.Fatalf("the log: %q", lines)
	}
	// the person decides: the choice is kept, and the question is gone
	if st, err = s.SetSecureBoot("off", ""); err != nil || st.Unset || st.ModeSource != SourceDashboard {
		t.Fatalf("choosing off is a choice too: %+v %v", st, err)
	}
	if st, err = s.SetSecureBoot("builtin", "all"); err != nil || !st.On || st.Mode != "builtin" || st.Scope != "all" {
		t.Fatalf("%+v %v", st, err)
	}
	reopened, _, err := config.Open(path, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if mode, scope, chosen := reopened.SecureBoot(); mode != "builtin" || scope != "all" || !chosen {
		t.Fatalf("the choice must survive a restart: %q %q %v", mode, scope, chosen)
	}
}

func TestOnlyMarkedPCsGoThroughTheShimUnlessTheScopeIsAll(t *testing.T) {
	for name, tc := range map[string]struct {
		s      *Server
		marked string // the file the marked PC gets
		other  string // the file an unmarked PC gets; "" is the usual boot file
	}{
		"built in, marked":       {sbServer(t, SecureBoot{}, builtinMarked, "aa:bb:cc:dd:ee:01"), "sb/ipxe-shimx64.efi", ""},
		"built in, all":          {sbServer(t, SecureBoot{}, freshDefault, "aa:bb:cc:dd:ee:01"), "sb/ipxe-shimx64.efi", "sb/ipxe-shimx64.efi"},
		"own key, marked":        {sbServer(t, SecureBoot{Dir: sbFolder(t)}, saved{}, "aa:bb:cc:dd:ee:01"), "sb/shimx64.efi", ""},
		"own key, all":           {sbServer(t, SecureBoot{Dir: sbFolder(t), All: true}, saved{}, "aa:bb:cc:dd:ee:01"), "sb/shimx64.efi", "sb/shimx64.efi"},
		"own key, named shim":    {sbServer(t, SecureBoot{Dir: sbFolder(t), Shim: "ipxe-shimx64.efi", All: true}, saved{}), "sb/ipxe-shimx64.efi", "sb/ipxe-shimx64.efi"},
		"off":                    {sbServer(t, SecureBoot{}, offChosen, "aa:bb:cc:dd:ee:01"), "", ""},
		"off but all":            {sbServer(t, SecureBoot{Scope: "all"}, offChosen, "aa:bb:cc:dd:ee:01"), "", ""},
		"an older settings file": {sbServer(t, SecureBoot{}, saved{}, "aa:bb:cc:dd:ee:01"), "", ""},
	} {
		if f, ok := tc.s.SecureBootFile("AA-BB-CC-DD-EE-01"); (f != "") != ok || f != tc.marked {
			t.Errorf("%s: a marked PC: %q %v, want %q", name, f, ok, tc.marked)
		}
		if f, ok := tc.s.SecureBootFile("aa:bb:cc:dd:ee:02"); (f != "") != ok || f != tc.other {
			t.Errorf("%s: another PC: %q %v, want %q", name, f, ok, tc.other)
		}
		if f, ok := tc.s.SecureBootFile("not a mac"); ok {
			t.Errorf("%s: not a MAC address: %q", name, f)
		}
	}
}

func TestTheSwitchCountsForTheNextAnswerWithoutARestart(t *testing.T) {
	s := sbServer(t, SecureBoot{OwnDir: sbFolder(t)}, offChosen)
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); ok {
		t.Fatal("off")
	}
	if _, err := s.SetSecureBoot("builtin", "all"); err != nil {
		t.Fatal(err)
	}
	if f, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); !ok || f != "sb/ipxe-shimx64.efi" {
		t.Fatalf("built in, all: %q %v", f, ok)
	}
	if _, err := s.SetSecureBoot("own", ""); err != nil {
		t.Fatal(err)
	}
	if f, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); !ok || f != "sb/shimx64.efi" {
		t.Fatalf("own key, all: %q %v", f, ok)
	}
	if _, err := s.SetSecureBoot("builtin", "marked"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); ok {
		t.Fatal("marked, and this PC is not")
	}
	if _, err := s.SetSecureBoot("off", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); ok {
		t.Fatal("off again")
	}
}

func readAll(t *testing.T, s *Server, client net.IP, name string) (string, bool) {
	t.Helper()
	f, ok := s.TFTPLookup(client, name)
	if !ok {
		return "", false
	}
	b, _ := io.ReadAll(f)
	f.Close()
	return string(b), true
}

var traversal = []string{"sb/", "sb/missing.efi", "sb/sub", "sb/sub/x", "sb/../shimx64.efi", `sb/..\shimx64.efi`, "sb/.", "sb/..", "sb/c:x", "sb/shimx64.efi:stream", "sb//ipxe.efi", "sb/./ipxe.efi",
	"sb/ipxe.efi/", "sb/ipxe.efi\x00", "sb/ipxe.efi ", "sb/../sb/ipxe.efi", "sb/MANIFEST.json", "sb/assets/ipxe.efi", "sb/x86_64-sb/ipxe.efi", "sb/shimaa64.efi", "sb/arm64-sb/ipxe.efi"}

func TestTheBuiltInFilesAreServedByTheirExactNamesAndNothingElse(t *testing.T) {
	set := secureboot.Builtin()
	client := net.IPv4(10, 0, 0, 5)
	for _, st := range []saved{freshDefault, builtinMarked} { // (whatever the scope: the files are served, which PCs are sent is another question)
		s := sbServer(t, SecureBoot{}, st)
		for _, name := range []string{secureboot.Shim, secureboot.SecondStage} {
			want, _ := set.Open(name)
			got, ok := readAll(t, s, client, "sb/"+name)
			if !ok || got != string(want) || len(got) < 100000 {
				t.Errorf("%s: served=%v, %d bytes, want %d", name, ok, len(got), len(want))
			}
		}
		for _, name := range append([]string{"sb/IPXE.EFI", "sb/Ipxe-Shimx64.efi", "sb/shimx64.efi", "sb/grubx64.efi", "sb/mmx64.efi"}, traversal...) {
			if f, ok := s.TFTPLookup(client, name); ok {
				f.Close()
				t.Errorf("%q must not be served (built in)", name)
			}
		}
	}
	// the other folder-less names under "sb/" are not served from a folder either: the own folder is not looked at in this mode
	dir := sbFolder(t)
	s := sbServer(t, SecureBoot{OwnDir: dir}, freshDefault)
	if f, ok := s.TFTPLookup(client, "sb/grubx64.efi"); ok {
		f.Close()
		t.Error("built in mode does not serve the own folder")
	}
	f, _ := s.TFTPLookup(client, "sb/ipxe.efi")
	st, err := f.Stat()
	f.Close()
	if err != nil || st.IsDir() || st.Size() != int64(len(mustOpen(t, set, secureboot.SecondStage))) {
		t.Errorf("the size TFTP announces: %v %v", st, err)
	}
}

func mustOpen(t *testing.T, set *secureboot.Set, name string) []byte {
	t.Helper()
	b, ok := set.Open(name)
	if !ok {
		t.Fatalf("%s not in the set", name)
	}
	return b
}

func TestTheOwnFolderIsServedByNameAndNothingElse(t *testing.T) {
	dir := sbFolder(t)
	client := net.IPv4(10, 0, 0, 5)
	for _, s := range []*Server{sbServer(t, SecureBoot{Dir: dir}, saved{}), sbServer(t, SecureBoot{OwnDir: dir}, ownAll)} {
		for name, want := range map[string]string{"sb/shimx64.efi": "SHIM", "sb/grubx64.efi": "IPXE", "sb/MMX64.EFI": "MOK", "sb/notes.txt": "x"} {
			if got, ok := readAll(t, s, client, name); !ok || got != want {
				t.Errorf("%s: %q %v, want %q", name, got, ok, want)
			}
		}
		for _, name := range append([]string{"sb/ipxe-shimx64.efi", "sb/ipxe.efi"}, traversal...) { // (the built-in files are not served in this mode)
			if f, ok := s.TFTPLookup(client, name); ok {
				f.Close()
				t.Errorf("%q must not be served (own key)", name)
			}
		}
	}
}

func TestNothingIsServedUnderSbWhenTheRouteIsOffOrTheFilesDoNotMatch(t *testing.T) {
	client := net.IPv4(10, 0, 0, 5)
	dir := sbFolder(t)
	for name, s := range map[string]*Server{
		"off":                  sbServer(t, SecureBoot{OwnDir: dir}, offChosen),
		"an older file":        sbServer(t, SecureBoot{OwnDir: dir}, saved{}),
		"off by an option":     sbServer(t, SecureBoot{Mode: "off", Dir: dir}, freshDefault),
		"own mode, no folder":  sbServer(t, SecureBoot{}, ownAll),
		"no settings at all":   {SecureBoot: SecureBoot{}},
		"damaged built in set": nil,
	} {
		if s == nil { // the built-in files do not match their manifest: a byte of the shim changed
			m := secureboot.Builtin().Manifest
			shim, ipxe := mustOpen(t, secureboot.Builtin(), secureboot.Shim), mustOpen(t, secureboot.Builtin(), secureboot.SecondStage)
			bad := append([]byte(nil), shim...)
			bad[len(bad)/2] ^= 1
			s = sbServer(t, SecureBoot{}, freshDefault, "aa:bb:cc:dd:ee:01")
			s.BuiltinSecureBoot = secureboot.NewSet(m, map[string][]byte{secureboot.Shim: bad, secureboot.SecondStage: ipxe})
			if s.BuiltinSecureBoot.Err() == nil {
				t.Fatal("the damaged set must be refused")
			}
		}
		for _, n := range []string{"sb/ipxe-shimx64.efi", "sb/ipxe.efi", "sb/shimx64.efi", "sb/grubx64.efi", "sb/notes.txt"} {
			if f, ok := s.TFTPLookup(client, n); ok {
				f.Close()
				t.Errorf("%s: %q must not be served", name, n)
			}
		}
		if f, ok := s.SecureBootFile("aa:bb:cc:dd:ee:01"); ok {
			t.Errorf("%s: no PC is sent to %q", name, f)
		}
	}
}

func TestAMismatchOfTheBuiltInFilesIsToldLoudlyAndTheStatusSaysSo(t *testing.T) {
	m := secureboot.Builtin().Manifest
	s := sbServer(t, SecureBoot{}, freshDefault)
	s.BuiltinSecureBoot = secureboot.NewSet(m, map[string][]byte{secureboot.Shim: []byte("MZ"), secureboot.SecondStage: []byte("MZ")})
	lines := strings.Join(s.SecureBootStartLines(), "\n")
	if !strings.Contains(lines, "Secure Boot route: ERROR") || !strings.Contains(lines, "NOT served") {
		t.Fatalf("the log: %q", lines)
	}
	st := s.SecureBootStatus()
	if st.On || st.Ready || st.Builtin.OK || st.Builtin.Error == "" || !strings.Contains(st.Problem, "do not match their manifest") || st.Message != st.Problem {
		t.Fatalf("%+v", st)
	}
	if _, err := s.SetSecureBoot("builtin", ""); !errors.Is(err, ErrSecureBootRefused) {
		t.Fatalf("switching to files that do not match: %v", err)
	}
	// another mode: the damaged files are only a warning
	own := sbServer(t, SecureBoot{Dir: sbFolder(t)}, freshDefault)
	own.BuiltinSecureBoot = s.BuiltinSecureBoot
	if lines = strings.Join(own.SecureBootStartLines(), "\n"); !strings.Contains(lines, "warning: the Secure Boot files built into this program do not match") || !strings.Contains(lines, "ON, own key") {
		t.Fatalf("own key with damaged built-in files: %q", lines)
	}
}

func TestKernelEntriesStartThroughTheIsosOwnShimOnlyWhenTheRouteIsOn(t *testing.T) {
	linux := []string{"kernel http://10.0.0.1:8080/fs/abc/casper/vmlinuz ip=dhcp", "initrd http://10.0.0.1:8080/fs/abc/casper/initrd", "boot"}
	want := []string{linux[0], linux[1], "shim http://10.0.0.1:8080/fs/abc/EFI/boot/bootx64.efi", "boot"}
	for name, on := range map[string]*Server{
		"built in": sbServer(t, SecureBoot{}, freshDefault),
		"marked":   sbServer(t, SecureBoot{}, builtinMarked), // (the ISO's shim line does not depend on which PCs use the iPXE shim: it does nothing with Secure Boot off)
		"own key":  sbServer(t, SecureBoot{Dir: sbFolder(t)}, saved{}),
	} {
		on.Base = "http://10.0.0.1:8080"
		if got := on.withShim("abc", "Ubuntu (casper)", linux); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v", name, got)
		}
		if linux[2] != "boot" || len(linux) != 3 {
			t.Fatal("the entry's own commands must not change")
		}
		for what, cmds := range map[string][]string{
			"a disk start":   {"echo loading", "sanboot --no-describe --drive 0x80 http://x/iso"},
			"no final boot":  {"kernel http://x/vmlinuz"},
			"nothing at all": nil,
		} {
			if got := on.withShim("abc", "Debian live", cmds); !reflect.DeepEqual(got, cmds) {
				t.Errorf("%s, %s: %v", name, what, got)
			}
		}
		win := []string{"kernel http://x/wimboot", "boot"}
		if got := on.withShim("abc", "Windows setup", win); !reflect.DeepEqual(got, win) {
			t.Errorf("%s: a shim cannot check wimboot: %v", name, got)
		}
		if !strings.Contains(strings.Join(on.withShim("abc", "Fedora (live)", linux), "\n"), "shim ") {
			t.Errorf("%s: Fedora too", name)
		}
	}
	for name, off := range map[string]*Server{
		"off":                    sbServer(t, SecureBoot{}, offChosen),
		"an older settings file": sbServer(t, SecureBoot{}, saved{}),
		"no settings":            {},
	} {
		if got := off.withShim("abc", "Debian live", linux); !reflect.DeepEqual(got, linux) {
			t.Errorf("%s: no shim command when the route is off: %v", name, got)
		}
	}
}

func TestTheSecondStageTheShimAsksForFollowsTheShimsName(t *testing.T) {
	for shim, want := range map[string]string{"": "grubx64.efi", "shimx64.efi": "grubx64.efi", "ipxe-shimx64.efi": "ipxe.efi", "IPXE-shimx64.efi": "ipxe.efi"} {
		if got := secondStage(shim); got != want {
			t.Errorf("shim %q: %q, want %q", shim, got, want)
		}
	}
	if secureboot.SecondStage != secondStage(secureboot.Shim) {
		t.Error("the built-in shim must ask for the built-in iPXE")
	}
}

func fileStates(files []SecureBootFileState) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[f.Name] = f.Present
	}
	return out
}

func TestTheStatusOfTheBuiltInRouteNamesTheFilesTheReleaseAndTheWayItWasChosen(t *testing.T) {
	st := sbServer(t, SecureBoot{}, freshDefault).SecureBootStatus()
	if !st.On || !st.Ready || st.Mode != "builtin" || st.Scope != "all" || st.ModeSource != SourceDefault || st.ScopeSource != SourceDefault || st.ModeLocked || st.ScopeLocked || st.Unset || st.Problem != "" ||
		st.Shim != "ipxe-shimx64.efi" || st.Dir != "" {
		t.Fatalf("%+v", st)
	}
	if !strings.Contains(st.Message, "every 64-bit UEFI PC") || !strings.Contains(st.Message, "v2.0.0") {
		t.Errorf("message: %q", st.Message)
	}
	if got := fileStates(st.Files); !reflect.DeepEqual(got, map[string]bool{"ipxe-shimx64.efi": true, "ipxe.efi": true}) {
		t.Errorf("files: %v", got)
	}
	for _, f := range st.Files {
		want := mustOpen(t, secureboot.Builtin(), f.Name)
		if f.Size != int64(len(want)) || len(f.SHA256) != 64 || !f.Required {
			t.Errorf("%+v", f)
		}
	}
	b := st.Builtin
	if !b.OK || b.Release != "v2.0.0" || b.TarballSHA256 != "01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1" || !strings.HasSuffix(b.Source, "/v2.0.0/ipxeboot.tar.gz") || len(b.Files) != 2 {
		t.Errorf("builtin: %+v", b)
	}
	if shim := b.Files[0]; shim.Release != "ipxe-16.1" || strings.Join(shim.SignedWith, " ") != "2011 2023" || !strings.HasSuffix(shim.Source, "/ipxe-16.1/ipxe-shimx64.efi") {
		t.Errorf("the shim's release and the CAs that signed it must reach the Server page: %+v", shim)
	}
	// it is also described while another mode is on, and the own folder's state is there for the person who thinks of switching
	dir := t.TempDir()
	off := sbServer(t, SecureBoot{OwnDir: dir}, offChosen).SecureBootStatus()
	if off.On || off.Ready || off.Mode != "off" || off.ModeSource != SourceDashboard || !off.Builtin.OK || off.Own.Ready || off.Own.Dir != dir || !strings.Contains(off.Own.Problem, "are missing") ||
		!strings.Contains(off.Message, "Built in") {
		t.Errorf("off: %+v", off)
	}
	marked := sbServer(t, SecureBoot{}, builtinMarked, "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02").SecureBootStatus()
	if marked.Marked != 2 || !strings.Contains(marked.Message, "2 PC(s) are marked") {
		t.Errorf("%+v", marked)
	}
	if none := sbServer(t, SecureBoot{}, builtinMarked).SecureBootStatus(); none.Marked != 0 || !strings.Contains(none.Message, "No PC is marked yet") {
		t.Errorf("%+v", none)
	}
	if all := sbServer(t, SecureBoot{}, freshDefault).SecureBootStatus(); strings.Contains(all.Message, "marked yet") {
		t.Errorf("with every PC sent, marking does not matter: %+v", all)
	}
}

// TestAnEmptyOwnFolderNamesBothFilesInTheRightGrammar checks the sentence for a folder that holds neither the shim nor the file it asks for.
func TestAnEmptyOwnFolderNamesBothFilesInTheRightGrammar(t *testing.T) {
	files, problem := ownFiles(t.TempDir(), DefaultShim)
	if !strings.HasPrefix(problem, "shimx64.efi and grubx64.efi are missing from ") || !strings.HasSuffix(problem, "Copy them into the folder.") {
		t.Fatalf("problem: %q", problem)
	}
	if len(files) < 2 || files[0].Present || files[1].Present {
		t.Fatalf("files: %+v", files)
	}
}

func TestTheStatusOfTheOwnKeyRouteSaysWhatIsThereAndWhatIsMissing(t *testing.T) {
	dir := sbFolder(t) // shimx64.efi, grubx64.efi, mmx64.efi
	st := sbServer(t, SecureBoot{Dir: dir}, freshDefault).SecureBootStatus()
	if !st.On || !st.Ready || st.Mode != "own" || st.ModeSource != SourceOption || !st.ModeLocked || st.ScopeLocked || st.Problem != "" || st.Shim != "shimx64.efi" || st.Dir != dir || !st.Own.DirFromOption {
		t.Fatalf("a distribution's shim with its files: %+v", st)
	}
	if got := fileStates(st.Files); !reflect.DeepEqual(got, map[string]bool{"shimx64.efi": true, "grubx64.efi": true, "mmx64.efi": true}) {
		t.Fatalf("files: %v", got)
	}
	if all := sbServer(t, SecureBoot{Dir: dir, All: true}, freshDefault).SecureBootStatus(); all.Scope != "all" || !all.ScopeLocked || !strings.Contains(all.Message, "every 64-bit UEFI PC") {
		t.Fatalf("all: %+v", all)
	}

	// the iPXE project's shim in the own folder, without the ipxe.efi it asks for: the start is not refused, the status says what to copy
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "ipxe-shimx64.efi"), []byte("SHIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := SecureBoot{Dir: only, Shim: "ipxe-shimx64.efi", All: true}
	if err := opts.Check(); err != nil {
		t.Fatalf("a missing second stage is not a reason to refuse the start: %v", err)
	}
	s := sbServer(t, opts, saved{})
	st = s.SecureBootStatus()
	if st.Ready || !strings.Contains(st.Problem, "ipxe.efi is missing") || st.Message != st.Problem {
		t.Fatalf("missing ipxe.efi: %+v", st)
	}
	if got := fileStates(st.Files); !reflect.DeepEqual(got, map[string]bool{"ipxe-shimx64.efi": true, "ipxe.efi": false}) {
		t.Fatalf("files: %v (a distribution's MokManager is not expected for this shim)", got)
	}
	lines := s.SecureBootStartLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "Secure Boot route: ON, own key") || !strings.HasPrefix(lines[1], "warning: ") || !strings.Contains(lines[1], "has no ipxe.efi") || !strings.Contains(lines[1], "docs/secure-boot.md") {
		t.Fatalf("log: %q", lines)
	}
	// copied in while the server runs: counts at once, with the case of the name not mattering
	if err := os.WriteFile(filepath.Join(only, "IPXE.EFI"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st = s.SecureBootStatus(); !st.Ready || len(s.SecureBootStartLines()) != 1 {
		t.Fatalf("after copying it: %+v", st)
	}
	// MokManager is optional; the shim removed after the start; a folder that is gone
	if err := os.Remove(filepath.Join(dir, "mmx64.efi")); err != nil {
		t.Fatal(err)
	}
	own := sbServer(t, SecureBoot{Dir: dir}, saved{})
	if st := own.SecureBootStatus(); !st.Ready {
		t.Fatalf("MokManager is only needed to enrol a key: %+v", st)
	}
	if err := os.Remove(filepath.Join(dir, "shimx64.efi")); err != nil {
		t.Fatal(err)
	}
	if st := own.SecureBootStatus(); st.Ready || !strings.Contains(st.Problem, "shimx64.efi is missing") {
		t.Fatalf("no shim: %+v", st)
	}
	if st := sbServer(t, SecureBoot{Dir: filepath.Join(dir, "gone")}, saved{}).SecureBootStatus(); st.Ready || !strings.Contains(st.Problem, "cannot be read") {
		t.Fatalf("no folder: %+v", st)
	}
}

func TestChangingTheModeAndTheScope(t *testing.T) {
	dir := t.TempDir() // the own folder: empty at first
	s := sbServer(t, SecureBoot{OwnDir: dir}, saved{})
	for name, tc := range map[string]struct {
		mode, scope string
		err         error
	}{
		"nothing":            {"", "", ErrSecureBootInvalid},
		"an unknown mode":    {"yes", "", ErrSecureBootInvalid},
		"an unknown scope":   {"", "some", ErrSecureBootInvalid},
		"a capital":          {"Builtin", "", ErrSecureBootInvalid},
		"own with no shim":   {"own", "", ErrSecureBootRefused},
		"own with no folder": {"own", "all", ErrSecureBootRefused},
	} {
		if _, err := s.SetSecureBoot(tc.mode, tc.scope); !errors.Is(err, tc.err) {
			t.Errorf("%s: %v, want %v", name, err, tc.err)
		}
	}
	if mode, scope, chosen := s.Config.SecureBoot(); mode != "" || scope != "" || chosen {
		t.Fatalf("a refused change saves nothing: %q %q %v", mode, scope, chosen)
	}
	st, err := s.SetSecureBoot("builtin", "")
	if err != nil || st.Mode != "builtin" || st.Scope != "marked" || !st.On || !st.Ready || st.ModeSource != SourceDashboard || st.Unset {
		t.Fatalf("%+v %v", st, err)
	}
	if st, err = s.SetSecureBoot("", "all"); err != nil || st.Mode != "builtin" || st.Scope != "all" {
		t.Fatalf("only the scope: %+v %v", st, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shimx64.efi"), []byte("SHIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, err = s.SetSecureBoot("own", ""); err != nil || st.Mode != "own" || st.Dir != dir || st.Shim != "shimx64.efi" || !strings.Contains(st.Problem, "grubx64.efi") {
		t.Fatalf("own, with the shim in the folder and the second stage missing is allowed, with the problem shown: %+v %v", st, err)
	}
	if st, err = s.SetSecureBoot("off", "marked"); err != nil || st.On || st.Mode != "off" || st.Scope != "marked" {
		t.Fatalf("%+v %v", st, err)
	}

	// what an option decides cannot be changed from the web interface, and is not saved
	locked := sbServer(t, SecureBoot{Mode: "builtin", Scope: "all"}, saved{"off", "marked", true})
	if _, err := locked.SetSecureBoot("off", ""); !errors.Is(err, ErrSecureBootRefused) || !strings.Contains(err.Error(), "command line") {
		t.Errorf("mode set by an option: %v", err)
	}
	if _, err := locked.SetSecureBoot("", "marked"); !errors.Is(err, ErrSecureBootRefused) {
		t.Errorf("scope set by an option: %v", err)
	}
	if st, err = locked.SetSecureBoot("builtin", "all"); err != nil || !st.ModeLocked || !st.ScopeLocked || st.Mode != "builtin" {
		t.Errorf("asking for what is already so is no change: %+v %v", st, err)
	}
	if mode, scope, _ := locked.Config.SecureBoot(); mode != "off" || scope != "marked" {
		t.Errorf("the saved choice is not overwritten by an option's value: %q %q", mode, scope)
	}
	onlyScope := sbServer(t, SecureBoot{Dir: sbFolder(t)}, saved{})
	if st, err = onlyScope.SetSecureBoot("", "all"); err != nil || st.Scope != "all" || st.Mode != "own" || !st.ModeLocked || st.ScopeLocked {
		t.Errorf("a folder fixes the mode, the scope stays free: %+v %v", st, err)
	}
	if _, err := (&Server{}).SetSecureBoot("builtin", ""); !errors.Is(err, ErrSecureBootRefused) {
		t.Errorf("no settings to keep it in: %v", err)
	}
}
