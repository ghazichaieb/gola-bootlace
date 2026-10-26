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
