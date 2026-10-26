package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func sample() config.Config {
	return config.Config{
		MenuTimeout: 15, PCDays: 90, Default: "local",
		Hidden:    map[string]bool{"0123456789": true},
		Titles:    map[string]string{"0123456789": "Windows 11"},
		PCNames:   map[string]string{"aa:bb:cc:dd:ee:01": "Front desk"},
		PCOptions: map[string]config.PCOptions{"aa:bb:cc:dd:ee:01": {Args: "nomodeset", Debug: config.DebugMode{Serial: true, Shell: true}}},
		Rules:     []config.Rule{{MAC: "aa:bb:cc:dd:ee:01", Action: "local", Note: "desk"}},
		AdminHash: "pbkdf2$secret", SMBPassword: "share-secret",
		Library: config.Library{Path: "/srv/isos", ID: "lib-1"}, LibrarySync: "abc",
	}
}

func zipOf(t *testing.T, c config.Config, pcs string, secrets bool) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := WriteZip(&b, c, json.RawMessage(pcs), secrets, "0.1.5", time.Now()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestABackupHoldsNoSecretsOrLibraryUnlessAskedAndComesBackWhole(t *testing.T) {
	got, err := ReadZip(zipOf(t, sample(), `[{"mac":"aa:bb:cc:dd:ee:01"}]`, false))
	if err != nil {
		t.Fatal(err)
	}
	c := got.Config
	if c.AdminHash != "" || c.SMBPassword != "" || c.Library.Path != "" || c.LibrarySync != "" || got.Manifest.Secrets {
		t.Fatalf("secrets or the machine's library leaked into the backup: %+v", c)
	}
	if len(c.Rules) != 1 || c.PCNames["aa:bb:cc:dd:ee:01"] != "Front desk" || c.PCOptions["aa:bb:cc:dd:ee:01"].Args != "nomodeset" || !c.PCOptions["aa:bb:cc:dd:ee:01"].Debug.Serial || !c.PCOptions["aa:bb:cc:dd:ee:01"].Debug.Shell || c.Titles["0123456789"] != "Windows 11" || !c.Hidden["0123456789"] || string(got.PCs) == "" {
		t.Fatalf("settings lost: %+v", got)
	}
	withSecrets, err := ReadZip(zipOf(t, sample(), "", true))
	if err != nil || withSecrets.Config.AdminHash != "pbkdf2$secret" || withSecrets.Config.SMBPassword != "share-secret" || !withSecrets.Manifest.Secrets || withSecrets.Config.Library.Path != "" {
		t.Fatalf("%+v %v", withSecrets, err)
	}
}

func TestMergeKeepsThisMachinesLibraryAndSecretsUnlessTheBackupHasThem(t *testing.T) {
	cur := sample()
	cur.Library = config.Library{Path: "/mnt/other", ID: "lib-2"}
	cur.AdminHash, cur.SMBPassword = "mine", "mine-smb"
	in, _ := ReadZip(zipOf(t, sample(), "", false))
	m := Merge(cur, in)
	if m.Library != cur.Library || m.AdminHash != "mine" || m.SMBPassword != "mine-smb" || len(m.Rules) != 1 {
		t.Fatalf("%+v", m)
	}
	in, _ = ReadZip(zipOf(t, sample(), "", true))
	m = Merge(cur, in)
	if m.AdminHash != "pbkdf2$secret" || m.SMBPassword != "share-secret" || m.Library != cur.Library {
		t.Fatalf("%+v", m)
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for n, c := range files {
		f, _ := zw.Create(n)
		f.Write([]byte(c))
	}
	zw.Close()
	return b.Bytes()
}

func TestBackupsThatCannotBeTrustedAreRefused(t *testing.T) {
	good := `{"format":1,"program":"Gola Bootlace","version":"0.1.5"}`
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"not a zip":       {[]byte("hello"), "not a zip"},
		"newer format":    {makeZip(t, map[string]string{"manifest.json": `{"format":99,"program":"Gola Bootlace","version":"9.0.0"}`, "settings.json": "{}"}), "newer version"},
		"other program":   {makeZip(t, map[string]string{"manifest.json": `{"format":1,"program":"Something","version":"1"}`, "settings.json": "{}"}), "not a backup made by Gola Bootlace"},
		"extra file":      {makeZip(t, map[string]string{"manifest.json": good, "settings.json": "{}", "evil.sh": "x"}), "does not"},
		"no settings":     {makeZip(t, map[string]string{"manifest.json": good}), "no readable settings"},
		"invalid values":  {makeZip(t, map[string]string{"manifest.json": good, "settings.json": `{"menuTimeout": -4}`}), "not valid"},
		"unknown accents": {makeZip(t, map[string]string{"manifest.json": good, "settings.json": `{"menuAccent":"mauve"}`}), "not valid"},
	} {
		if _, err := ReadZip(tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
	if _, err := ReadZip(makeZip(t, map[string]string{"manifest.json": good, "settings.json": "{}"})); err != nil {
		t.Errorf("minimal backup: %v", err)
	}
}

func newLib(t *testing.T) *library.Library {
	t.Helper()
	dir := t.TempDir()
	id, err := library.Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	return library.New(dir, id)
}

func newStore(t *testing.T, c config.Config) *config.Store {
	t.Helper()
	s, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheLibraryKeepsACopyAndANewContainerIsOfferedIt(t *testing.T) {
	lib := newLib(t)
	a := newStore(t, config.Config{MenuTimeout: 5})
	sa := &Sync{Store: a, Lib: lib, Version: "0.1.5"}
	a.OnChange(sa.Changed)
	sa.Check() // the first container writes its settings
	file := filepath.Join(lib.Path(), library.MetaDir, FileName)
	if _, err := os.Stat(file); err != nil {
		t.Fatal("no copy was written:", err)
	}
	if err := a.Update(func(c *config.Config) error {
		c.AdminHash = "never-in-the-library"
		c.Rules = []config.Rule{{MAC: "aa:bb:cc:dd:ee:01", Action: "local"}}
		c.PCNames = map[string]string{"aa:bb:cc:dd:ee:01": "Desk"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(file)
		if strings.Contains(string(data), "Desk") {
			if strings.Contains(string(data), "never-in-the-library") {
				t.Fatal("a secret reached the library")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the change did not reach the library:", string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := sa.State(0); st.Pending || !st.Available {
		t.Fatalf("the container that wrote it has nothing to decide: %+v", st)
	}

	// a second container, new, given the same library
	b := newStore(t, config.Config{MenuTimeout: 30})
	sb := &Sync{Store: b, Lib: lib, Version: "0.1.5"}
	b.OnChange(sb.Changed)
	st := sb.State(0)
	if !st.Pending || st.Library.Rules != 1 || st.Library.PCNames != 1 || st.Current.Rules != 0 {
		t.Fatalf("%+v", st)
	}
	sb.Check()
	if err := b.Update(func(c *config.Config) error { c.MenuTimeout = 31; return nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "Desk") {
		t.Fatal("a copy that was not decided about must not be overwritten")
	}
	if err := sb.Restore(); err != nil {
		t.Fatal(err)
	}
	got := b.Get()
	if len(got.Rules) != 1 || got.PCNames["aa:bb:cc:dd:ee:01"] != "Desk" || got.AdminHash != "" {
		t.Fatalf("%+v", got)
	}
	if st := sb.State(0); st.Pending {
		t.Fatalf("after the restore there is nothing to decide: %+v", st)
	}

	// keep: a third container chooses its own settings over the library's copy
	c := newStore(t, config.Config{MenuTimeout: 40})
	sc := &Sync{Store: c, Lib: lib, Version: "0.1.5"}
	if !sc.State(0).Pending {
		t.Fatal("pending expected")
	}
	if err := sc.Keep(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); strings.Contains(string(data), "Desk") || !strings.Contains(string(data), `"menuTimeout": 40`) {
		t.Fatalf("the library did not take the settings: %s", data)
	}
	if sc.State(0).Pending {
		t.Fatal("kept")
	}
}

func TestNothingIsDoneWithoutALibrary(t *testing.T) {
	s := &Sync{Store: newStore(t, config.Config{}), Lib: library.New("", "")}
	s.Check()
	if st := s.State(0); st.Available || st.Pending {
		t.Fatalf("%+v", st)
	}
	if err := s.Restore(); err == nil {
		t.Fatal("restore without a library")
	}
}

func TestBootEntryChoicesRoundTripThroughBackup(t *testing.T) {
	const way = "0123456789abcdef0123456789abcdef"
	c := sample()
	c.ISOEntries = map[string]config.ISOEntrySettings{"0123456789": {Default: way, Entries: map[string]config.EntryOptions{way: {Title: "Recovery", Args: "nomodeset"}}}}
	var buf bytes.Buffer
	if err := WriteZip(&buf, c, nil, false, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got := restored.Config.ISOEntries["0123456789"]
	if got.Default != way || got.Entries[way].Title != "Recovery" || got.Entries[way].Args != "nomodeset" {
		t.Fatalf("lost entry choices: %+v", got)
	}
}
