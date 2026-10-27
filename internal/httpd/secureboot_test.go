package httpd

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// sbFolder is a folder as the Secure Boot route wants it: a shim and the second stage (what they hold does not matter here).
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

func sbServer(t *testing.T, sb SecureBoot, marked ...string) *Server {
	t.Helper()
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(marked) > 0 {
		if err := store.Update(func(c *config.Config) error {
			c.PCOptions = map[string]config.PCOptions{}
			for _, m := range marked {
				c.PCOptions[m] = config.PCOptions{SecureBoot: true}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{Config: store, SecureBoot: sb}
}

func TestTheSecureBootRouteIsOffUnlessAFolderIsGiven(t *testing.T) {
	s := sbServer(t, SecureBoot{})
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("no folder: no shim")
	}
	if _, ok := s.TFTPLookup(net.IPv4(10, 0, 0, 5), "sb/shimx64.efi"); ok {
		t.Fatal("no folder: nothing is served under sb/")
	}
	cmds := []string{"kernel http://s/vmlinuz", "boot"}
	if got := s.withShim("abc", "Debian live", cmds); !reflect.DeepEqual(got, cmds) {
		t.Fatalf("no shim command when the route is off: %v", got)
	}
}

func TestOnlyMarkedPCsGoThroughTheShimUnlessAllAre(t *testing.T) {
	dir := sbFolder(t)
	s := sbServer(t, SecureBoot{Dir: dir}, "aa:bb:cc:dd:ee:01")
	if f, ok := s.SecureBootFile("AA-BB-CC-DD-EE-01"); !ok || f != "sb/shimx64.efi" {
		t.Fatalf("a marked PC: %q %v", f, ok)
	}
	if _, ok := s.SecureBootFile("aa:bb:cc:dd:ee:02"); ok {
		t.Fatal("a PC that is not marked keeps the usual boot file")
	}
	if _, ok := s.SecureBootFile("not a mac"); ok {
		t.Fatal("not a MAC address")
	}
	all := sbServer(t, SecureBoot{Dir: dir, All: true})
	if f, ok := all.SecureBootFile("aa:bb:cc:dd:ee:02"); !ok || f != "sb/shimx64.efi" {
		t.Fatalf("every PC with -secure-boot-all: %q %v", f, ok)
	}
	named := sbServer(t, SecureBoot{Dir: dir, All: true, Shim: "ipxe-shimx64.efi"})
	if f, _ := named.SecureBootFile("aa:bb:cc:dd:ee:02"); f != "sb/ipxe-shimx64.efi" {
		t.Fatalf("the shim's name is the one given: %q", f)
	}
}

func TestTheShimsFilesAreServedByNameAndNothingElse(t *testing.T) {
	dir := sbFolder(t)
	s := sbServer(t, SecureBoot{Dir: dir})
	client := net.IPv4(10, 0, 0, 5)
	for name, want := range map[string]string{"sb/shimx64.efi": "SHIM", "sb/grubx64.efi": "IPXE", "sb/MMX64.EFI": "MOK", "sb/notes.txt": "x"} {
		f, ok := s.TFTPLookup(client, name)
		if !ok {
			t.Errorf("%s: not served", name)
			continue
		}
		b, _ := io.ReadAll(f)
		f.Close()
		if string(b) != want {
			t.Errorf("%s: %q, want %q", name, b, want)
		}
	}
	for _, name := range []string{"sb/", "sb/missing.efi", "sb/sub", "sb/sub/x", "sb/../shimx64.efi", `sb/..\shimx64.efi`, "sb/.", "sb/..", "sb/c:x", "sb/shimx64.efi:stream"} {
		if f, ok := s.TFTPLookup(client, name); ok {
			f.Close()
			t.Errorf("%q must not be served", name)
		}
	}
}

func TestTheFolderMustHoldTheShim(t *testing.T) {
	dir := sbFolder(t)
	if err := (SecureBoot{}).Check(); err != nil {
		t.Fatalf("off is fine: %v", err)
	}
	if err := (SecureBoot{Dir: dir}).Check(); err != nil {
		t.Fatalf("a folder with the shim: %v", err)
	}
	for _, sb := range []SecureBoot{{Dir: filepath.Join(dir, "nope")}, {Dir: dir, Shim: "other.efi"}, {Dir: dir, Shim: "sub"}, {Dir: dir, Shim: "../x"}} {
		if err := sb.Check(); err == nil {
			t.Errorf("%+v must be refused", sb)
		}
	}
}

func TestKernelEntriesStartThroughTheIsosOwnShimOnlyWhenTheRouteIsOn(t *testing.T) {
	s := sbServer(t, SecureBoot{Dir: sbFolder(t)})
	s.Base = "http://10.0.0.1:8080"
	linux := []string{"kernel http://10.0.0.1:8080/fs/abc/casper/vmlinuz ip=dhcp", "initrd http://10.0.0.1:8080/fs/abc/casper/initrd", "boot"}
	want := []string{linux[0], linux[1], "shim http://10.0.0.1:8080/fs/abc/EFI/boot/bootx64.efi", "boot"}
	if got := s.withShim("abc", "Ubuntu (casper)", linux); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if linux[2] != "boot" || len(linux) != 3 {
		t.Fatal("the entry's own commands must not change")
	}
	for name, cmds := range map[string][]string{
		"a disk start":   {"echo loading", "sanboot --no-describe --drive 0x80 http://x/iso"},
		"no final boot":  {"kernel http://x/vmlinuz"},
		"nothing at all": nil,
	} {
		if got := s.withShim("abc", "Debian live", cmds); !reflect.DeepEqual(got, cmds) {
			t.Errorf("%s: %v", name, got)
		}
	}
	win := []string{"kernel http://x/wimboot", "boot"}
	if got := s.withShim("abc", "Windows setup", win); !reflect.DeepEqual(got, win) {
		t.Errorf("a shim cannot help wimboot: %v", got)
	}
	if !strings.Contains(strings.Join(s.withShim("abc", "Fedora (live)", linux), "\n"), "shim ") {
		t.Error("Fedora too")
	}
}

func TestTheSecondStageTheShimAsksForFollowsTheShimsName(t *testing.T) {
	for shim, want := range map[string]string{"": "grubx64.efi", "shimx64.efi": "grubx64.efi", "ipxe-shimx64.efi": "ipxe.efi", "IPXE-shimx64.efi": "ipxe.efi"} {
		if got := (SecureBoot{Dir: "/sb", Shim: shim}).SecondStage(); got != want {
			t.Errorf("shim %q: %q, want %q", shim, got, want)
		}
	}
}

func fileStates(st SecureBootStatus) map[string]bool {
	out := map[string]bool{}
	for _, f := range st.Files {
		out[f.Name] = f.Present
	}
	return out
}

func TestTheStatusOfTheRouteSaysWhatIsThereAndWhatIsMissing(t *testing.T) {
	off := (SecureBoot{}).Status()
	if off.On || off.Mode != "" || off.Ready || !strings.Contains(off.Message, "BOOTLACE_SECURE_BOOT_DIR") || len(off.Files) != 0 {
		t.Fatalf("off: %+v", off)
	}
	if p := (SecureBoot{}).Problems(); len(p) != 0 {
		t.Fatalf("off has no problems: %v", p)
	}

	dir := sbFolder(t) // shimx64.efi, grubx64.efi, mmx64.efi
	st := SecureBoot{Dir: dir}.Status()
	if !st.On || !st.Ready || st.Mode != "pcs" || st.Problem != "" || st.Shim != "shimx64.efi" || st.Dir != dir {
		t.Fatalf("a distribution's shim with its files: %+v", st)
	}
	if got := fileStates(st); !reflect.DeepEqual(got, map[string]bool{"shimx64.efi": true, "grubx64.efi": true, "mmx64.efi": true}) {
		t.Fatalf("files: %v", got)
	}
	if all := (SecureBoot{Dir: dir, All: true}).Status(); all.Mode != "all" || !strings.Contains(all.Message, "every 64-bit UEFI PC") {
		t.Fatalf("all: %+v", all)
	}

	// the iPXE project's shim without the ipxe.efi it asks for: the shim is there, the start is not refused, the status and the warning say what to copy
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "ipxe-shimx64.efi"), []byte("SHIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	sb := SecureBoot{Dir: only, Shim: "ipxe-shimx64.efi", All: true}
	if err := sb.Check(); err != nil {
		t.Fatalf("a missing second stage is not a reason to refuse the start: %v", err)
	}
	st = sb.Status()
	if st.Ready || !strings.Contains(st.Problem, "ipxe.efi is missing") || st.Message != st.Problem {
		t.Fatalf("missing ipxe.efi: %+v", st)
	}
	if got := fileStates(st); !reflect.DeepEqual(got, map[string]bool{"ipxe-shimx64.efi": true, "ipxe.efi": false}) {
		t.Fatalf("files: %v (a distribution's MokManager is not expected for this shim)", got)
	}
	if p := sb.Problems(); len(p) != 1 || !strings.Contains(p[0], "ipxe.efi") || !strings.Contains(p[0], "docs/secure-boot.md") {
		t.Fatalf("problems: %v", p)
	}
	// copied in while the server runs: counts at once, with the case of the name not mattering
	if err := os.WriteFile(filepath.Join(only, "IPXE.EFI"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st = sb.Status(); !st.Ready || len(sb.Problems()) != 0 {
		t.Fatalf("after copying it: %+v", st)
	}
	// MokManager is optional
	if st := (SecureBoot{Dir: dir}).Status(); !st.Ready {
		t.Fatal("ready")
	}
	if err := os.Remove(filepath.Join(dir, "mmx64.efi")); err != nil {
		t.Fatal(err)
	}
	if st := (SecureBoot{Dir: dir}).Status(); !st.Ready {
		t.Fatalf("MokManager is only needed to enrol a key: %+v", st)
	}
	// the shim removed after the start, and a folder that is gone
	if err := os.Remove(filepath.Join(dir, "shimx64.efi")); err != nil {
		t.Fatal(err)
	}
	if st := (SecureBoot{Dir: dir}).Status(); st.Ready || !strings.Contains(st.Problem, "shimx64.efi is missing") {
		t.Fatalf("no shim: %+v", st)
	}
	if st := (SecureBoot{Dir: filepath.Join(dir, "gone")}).Status(); st.Ready || !strings.Contains(st.Problem, "cannot be read") {
		t.Fatalf("no folder: %+v", st)
	}
}

func TestTheServersStatusCountsThePCsMarkedForTheRoute(t *testing.T) {
	dir := sbFolder(t)
	s := sbServer(t, SecureBoot{Dir: dir}, "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02")
	if st := s.SecureBootStatus(); st.Marked != 2 || !strings.Contains(st.Message, "2 PC(s) are marked") {
		t.Fatalf("%+v", st)
	}
	none := sbServer(t, SecureBoot{Dir: dir})
	if st := none.SecureBootStatus(); st.Marked != 0 || !strings.Contains(st.Message, "No PC is marked yet") {
		t.Fatalf("%+v", st)
	}
	if st := sbServer(t, SecureBoot{Dir: dir, All: true}).SecureBootStatus(); strings.Contains(st.Message, "marked yet") {
		t.Fatalf("with every PC sent, marking does not matter: %+v", st)
	}
}
