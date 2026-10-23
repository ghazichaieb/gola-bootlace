package httpd

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// plainCheck stands in for the PBKDF2 check of the web interface: the "hash" is the password with a prefix.
func plainCheck(hash, password string) bool { return hash == "h:"+password }

func protectedServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"Wipe.iso", "Other.iso"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := isos.IDOf("Wipe.iso")
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error { c.BootPasswords = map[string]string{id: "h:1234"}; return nil }); err != nil {
		t.Fatal(err)
	}
	return &Server{ISODir: dir, Base: "http://10.0.0.1:8080", Config: store, CheckPassword: plainCheck}, id
}

func TestAProtectedIsoAsksForItsPasswordInTheMenuAndAnotherDoesNot(t *testing.T) {
	s, id := protectedServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	_, body := get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:ff&ip=10.0.0.9", nil)
	blockOf := func(label string) string {
		i := strings.Index(body, label)
		if i < 0 {
			t.Fatalf("no entry %s in:\n%s", label, body)
		}
		rest := body[i:]
		if j := strings.Index(rest, "\n\n"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	block := blockOf(":e1-wipe")
	wantInOrder := []string{"login || goto denied", "imgfetch --name auth http://10.0.0.1:8080/auth?iso=" + id + "&mac=${mac}&pw=${password:uristring} || goto denied", "clear password", "sanboot"}
	pos := 0
	for _, w := range wantInOrder {
		j := strings.Index(block[pos:], w)
		if j < 0 {
			t.Fatalf("%q missing or out of order in\n%s", w, block)
		}
		pos += j
	}
	if other := blockOf(":e0-other"); strings.Contains(other, "login") || strings.Contains(other, "auth?") {
		t.Fatalf("an ISO without a password must start without asking:\n%s", other)
	}
	if !strings.Contains(body, ":denied\n") {
		t.Fatal("the denied label is missing")
	}
	// a rule that starts the ISO on purpose is not asked
	infos, _ := s.entryInfos(s.settings())
	for _, e := range infos {
		if e.ISO.ID == id {
			if !e.Protected || len(e.Entry.Guard) == 0 {
				t.Fatalf("%+v", e)
			}
			if direct := menu.Direct("rule", e.Entry, "http://x/menu"); strings.Contains(direct, "login") || strings.Contains(direct, "auth?") {
				t.Fatalf("a rule is a deliberate start, no password:\n%s", direct)
			}
		}
	}
}

func TestThePasswordIsCheckedByTheServerAndGuessingIsSlowedDown(t *testing.T) {
	s, id := protectedServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	url := func(pw string) string { return ts.URL + "/auth?iso=" + id + "&mac=aa&pw=" + pw }
	if code, _ := get(t, url("1234"), nil); code != 200 {
		t.Fatalf("the right password: %d", code)
	}
	if code, _ := get(t, ts.URL+"/auth?iso=ffffffffff&pw=1234", nil); code != 403 {
		t.Fatalf("an ISO with no password asks nothing and lets nothing through: %d", code)
	}
	if code, _ := get(t, url(""), nil); code != 403 {
		t.Fatalf("an empty password: %d", code)
	}
	for i := 0; i < 3; i++ { // (two failures so far: the unknown ISO and the empty password; the third, fourth and fifth lock the address)
		if code, _ := get(t, url("nope"), nil); code != 403 {
			t.Fatalf("wrong guess %d: %d", i+3, code)
		}
	}
	if code, _ := get(t, url("1234"), nil); code != 429 {
		t.Fatalf("after five wrong guesses in a row even the right password waits: %d", code)
	}
}
