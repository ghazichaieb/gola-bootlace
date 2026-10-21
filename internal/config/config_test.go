package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", "bootlace.json")
	s, created, err := Open(p, Config{Title: "Gola Bootlace", MenuTimeout: 5})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	return s, p
}

func TestTheFirstOpenWritesTheSeedAndTheNextOneReadsItBack(t *testing.T) {
	s, p := open(t)
	if got := s.Get(); got.Title != "Gola Bootlace" || got.MenuTimeout != 5 {
		t.Fatalf("%+v", got)
	}
	if err := s.Update(func(c *Config) error { c.MenuTimeout = 30; c.Default = "local"; return nil }); err != nil {
		t.Fatal(err)
	}
	again, created, err := Open(p, Config{Title: "ignored"})
	if err != nil || created {
		t.Fatal(err, created)
	}
	if got := again.Get(); got.MenuTimeout != 30 || got.Default != "local" || got.Title != "Gola Bootlace" {
		t.Fatalf("%+v", got)
	}
}

func TestAnInvalidChangeIsRefusedAndNothingChanges(t *testing.T) {
	s, p := open(t)
	before, _ := os.ReadFile(p)
	for name, fn := range map[string]func(*Config){
		"timeout": func(c *Config) { c.MenuTimeout = -1 },
		"default": func(c *Config) { c.Default = "rm -rf" },
		"title":   func(c *Config) { c.Title = "a\nb" },
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
