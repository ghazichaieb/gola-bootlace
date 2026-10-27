package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", "bootlace.json")
	s, created, err := Open(p, Config{MenuTimeout: 5})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	return s, p
}

func TestTheFirstOpenWritesTheSeedAndTheNextOneReadsItBack(t *testing.T) {
	s, p := open(t)
	if got := s.Get(); got.MenuTimeout != 5 {
		t.Fatalf("%+v", got)
	}
	if err := s.Update(func(c *Config) error { c.MenuTimeout = 30; c.Default = "local"; return nil }); err != nil {
		t.Fatal(err)
	}
	again, created, err := Open(p, Config{})
	if err != nil || created {
		t.Fatal(err, created)
	}
	if got := again.Get(); got.MenuTimeout != 30 || got.Default != "local" {
		t.Fatalf("%+v", got)
	}
}

func TestAnInvalidChangeIsRefusedAndNothingChanges(t *testing.T) {
	s, p := open(t)
	before, _ := os.ReadFile(p)
	for name, fn := range map[string]func(*Config){
		"timeout": func(c *Config) { c.MenuTimeout = -1 },
		"default": func(c *Config) { c.Default = "rm -rf" },
		"mac":     func(c *Config) { c.Rules = []Rule{{MAC: "nope", Action: "local"}} },
		"action":  func(c *Config) { c.Rules = []Rule{{MAC: "aa:bb:cc:dd:ee:ff", Action: "iso:zz"}} },
		"twice": func(c *Config) {
			c.Rules = []Rule{{MAC: "aa:bb:cc:dd:ee:ff", Action: "local"}, {MAC: "AA-BB-CC-DD-EE-FF", Action: "menu"}}
		},
		"longnote": func(c *Config) {
			c.Rules = []Rule{{MAC: "aa:bb:cc:dd:ee:ff", Action: "local", Note: strings.Repeat("x", 200)}}
		},
	} {
		if err := s.Update(func(c *Config) error { fn(c); return nil }); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) || s.Get().MenuTimeout != 5 {
		t.Fatal("a refused change must leave settings and file as they were")
	}
}

func TestRulesAreCleanedAndTitlesDropWhenEmpty(t *testing.T) {
	s, _ := open(t)
	err := s.Update(func(c *Config) error {
		c.Rules = []Rule{{MAC: "AA-BB-CC-DD-EE-0F", Action: "iso:0123456789", Note: " lab PC "}}
		c.Titles = map[string]string{"0123456789": "  Windows 11 EN  ", "abcdefabcd": "  "}
		c.Hidden = map[string]bool{"0123456789": true, "abcdefabcd": false}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if got.Rules[0].MAC != "aa:bb:cc:dd:ee:0f" || got.Rules[0].Note != "lab PC" || got.Titles["0123456789"] != "Windows 11 EN" || len(got.Titles) != 1 || len(got.Hidden) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s, _ := open(t)
	c := s.Get()
	c.Hidden["0123456789"] = true
	c.Rules = append(c.Rules, Rule{MAC: "aa:bb:cc:dd:ee:ff", Action: "local"})
	if got := s.Get(); len(got.Hidden) != 0 || len(got.Rules) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPCNamesAreKeyedByNormalisedMACAndCleaned(t *testing.T) {
	s, _ := open(t)
	err := s.Update(func(c *Config) error {
		c.PCNames = map[string]string{"AA-BB-CC-DD-EE-0F": "  Front desk  ", "aa:bb:cc:dd:ee:10": "   ", "not a mac": "x"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().PCNames; len(got) != 1 || got["aa:bb:cc:dd:ee:0f"] != "Front desk" {
		t.Fatalf("%v", got)
	}
	for _, bad := range []string{"two\nlines", strings.Repeat("x", MaxPCName+1)} {
		if err := s.Update(func(c *Config) error { c.PCNames["aa:bb:cc:dd:ee:0f"] = bad; return nil }); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if s.Get().PCNames["aa:bb:cc:dd:ee:0f"] != "Front desk" {
		t.Fatal("a refused change must leave the name as it was")
	}
}

func TestHidePatterns(t *testing.T) {
	patterns := []string{"old/", "*BETA*", "win11.iso", "Backups/*.iso", "dev-?.img", "  "}
	for rel, want := range map[string]bool{
		"old/a.iso": true, "Old/sub/a.iso": true, "older/a.iso": false, "x/old/a.iso": false,
		"ubuntu-beta-2.iso": true, "x/Ubuntu_BETA.iso": true,
		"win11.iso": true, "Windows/WIN11.ISO": true, "win11.iso.bak": false, "mywin11.iso": false,
		"backups/2024/a.iso": true, "backups/a.img": false,
		"dev-1.img": true, "dev-10.img": false,
		"keep.iso": false,
	} {
		if got := MatchesHide(patterns, rel); got != want {
			t.Errorf("%q: got %v, want %v", rel, got, want)
		}
	}
	if MatchesHide(nil, "a.iso") || MatchesHide([]string{" "}, "a.iso") {
		t.Error("no pattern matches nothing")
	}
}

func TestTuningAndPatternsAreChecked(t *testing.T) {
	s, _ := open(t)
	if err := s.Update(func(c *Config) error { c.Tuning.TFTPBlockSize = 100; return nil }); err == nil {
		t.Error("a block size under 512 must be refused")
	}
	if err := s.Update(func(c *Config) error { c.Tuning.TFTPWindow = 17; return nil }); err == nil {
		t.Error("a window over 16 must be refused")
	}
	if err := s.Update(func(c *Config) error { c.HidePatterns = []string{"a\nb"}; return nil }); err == nil {
		t.Error("a pattern with a line break must be refused")
	}
	if err := s.Update(func(c *Config) error {
		c.Tuning = Tuning{TFTPBlockSize: 1024, TFTPWindow: 8}
		c.HidePatterns = []string{" old/ ", "", "*beta*"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.Tuning.TFTPBlockSize != 1024 || len(got.HidePatterns) != 2 || got.HidePatterns[0] != "old/" {
		t.Fatalf("%+v", got)
	}
}

func TestOnlyKnownAccentColoursAreAccepted(t *testing.T) {
	s, _ := open(t)
	if err := s.Update(func(c *Config) error { c.MenuAccent = "purple-ish"; return nil }); err == nil {
		t.Error("an unknown colour must be refused")
	}
	for _, name := range append([]string{""}, AccentNames...) {
		if err := s.Update(func(c *Config) error { c.MenuAccent = name; return nil }); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestOnChangeHearsEveryKeptChangeInOrderAndNotTheQuietOnes(t *testing.T) {
	s, _ := open(t)
	got := make(chan int, 10)
	s.OnChange(func(c Config) {
		_ = s.Get() // a function may use the store
		got <- c.MenuTimeout
	})
	for _, n := range []int{11, 12, 13} {
		if err := s.Update(func(c *Config) error { c.MenuTimeout = n; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpdateQuiet(func(c *Config) error { c.MenuTimeout = 99; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) error { c.MenuTimeout = -5; return nil }); err == nil {
		t.Fatal("an invalid change must be refused")
	}
	for _, want := range []int{11, 12, 13} {
		select {
		case v := <-got:
			if v != want {
				t.Fatalf("got %d, want %d", v, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no call for %d", want)
		}
	}
	select {
	case v := <-got:
		t.Fatalf("an unexpected call: %d", v)
	case <-time.After(200 * time.Millisecond):
	}
	if s.Get().MenuTimeout != 99 {
		t.Fatal("the quiet change was kept")
	}
}

func TestAnInstallationFromBeforeTheGuideHasDoneItsFirstStart(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"menuTimeout": 5, "adminHash": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, created, err := Open(old, Config{})
	if err != nil || created || !s.Get().FirstStartDone {
		t.Fatalf("%v %v %+v", err, created, s.Get())
	}
	fresh, created, err := Open(filepath.Join(dir, "new.json"), Config{})
	if err != nil || !created || fresh.Get().FirstStartDone {
		t.Fatalf("a new installation has not: %v %v %+v", err, created, fresh.Get())
	}
}

func TestPCOptionsAreKeyedByNormalisedMACAndCheckedLikeTheArgumentsOfAnISO(t *testing.T) {
	s, p := open(t)
	err := s.Update(func(c *Config) error {
		c.PCOptions = map[string]PCOptions{
			"AA-BB-CC-DD-EE-01": {Args: "  nomodeset  "},
			"aa:bb:cc:dd:ee:02": {Debug: DebugMode{Serial: true}},
			"aa:bb:cc:dd:ee:03": {},                              // nothing set: not kept
			"aa:bb:cc:dd:ee:04": {Debug: DebugMode{Shell: true}}, // the shell switch alone is a setting
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get().PCOptions
	if len(got) != 3 || got["aa:bb:cc:dd:ee:01"].Args != "nomodeset" || !got["aa:bb:cc:dd:ee:02"].Debug.Serial || !got["aa:bb:cc:dd:ee:04"].Debug.Shell {
		t.Fatalf("%+v", got)
	}
	if again, _, _ := Open(p, Config{}); again.Get().PCOptions["aa:bb:cc:dd:ee:01"].Args != "nomodeset" {
		t.Fatal("the options are kept in the file")
	}
	for name, o := range map[string]PCOptions{"two lines": {Args: "a\nb"}, "too long": {Args: strings.Repeat("x", MaxArgs+1)}} {
		if err := s.Update(func(c *Config) error { c.PCOptions["aa:bb:cc:dd:ee:09"] = o; return nil }); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := s.Update(func(c *Config) error { c.PCOptions["not-a-mac"] = PCOptions{Args: "x"}; return nil }); err == nil {
		t.Error("a key that is not a MAC address was accepted")
	}
	if got := s.Get().PCOptions; len(got) != 3 {
		t.Fatalf("a refused change must change nothing: %+v", got)
	}
	got["aa:bb:cc:dd:ee:01"] = PCOptions{Args: "changed"}
	if s.Get().PCOptions["aa:bb:cc:dd:ee:01"].Args != "nomodeset" {
		t.Fatal("Get must return a copy")
	}
}

func TestAnISOWithOnlyTheDebugShellSwitchKeepsItAndOneWithNothingDoesNot(t *testing.T) {
	s, p := open(t)
	err := s.Update(func(c *Config) error {
		c.Debug = map[string]DebugMode{"0123456789": {Shell: true}, "abcdef0123": {}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Debug; len(got) != 1 || !got["0123456789"].Shell {
		t.Fatalf("%+v", got)
	}
	if again, _, _ := Open(p, Config{}); !again.Get().Debug["0123456789"].Shell {
		t.Fatal("the switch is kept in the file")
	}
	if (PCOptions{Debug: DebugMode{Shell: true}}).Empty() {
		t.Fatal("options with only the shell switch are not empty")
	}
}

func TestAPCsSecureBootMarkIsKeptAndAloneKeepsTheEntry(t *testing.T) {
	c := Config{PCOptions: map[string]PCOptions{"AA-BB-CC-DD-EE-01": {SecureBoot: true}, "aa:bb:cc:dd:ee:02": {}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if o, ok := c.PCOptions["aa:bb:cc:dd:ee:01"]; !ok || !o.SecureBoot {
		t.Fatalf("a PC with only the mark must be kept: %+v", c.PCOptions)
	}
	if _, ok := c.PCOptions["aa:bb:cc:dd:ee:02"]; ok {
		t.Fatal("an entry with nothing set is dropped")
	}
}
