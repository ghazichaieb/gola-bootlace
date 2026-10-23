// Package config holds the settings that are changed in the web interface and kept in a file: the menu's timeout, what starts by itself,
// the ISOs that are hidden or renamed, and what each PC (by MAC address) is sent to. Infrastructure settings (addresses, ports, DHCP mode) stay on
// the command line.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
)

// Rule says where one PC goes when it starts from the network.
type Rule struct {
	MAC    string    `json:"mac"`    // lower case, colon separated
	Action string    `json:"action"` // "menu" (show the menu: the same as no rule), "local" (its own disk) or "iso:<id>"
	Note   string    `json:"note"`
	Added  time.Time `json:"added"`
}

// Config is the whole file.
type Config struct {
	MenuTimeout     int               `json:"menuTimeout"` // seconds before the default entry starts; 0 waits for a choice
	PCDays          int               `json:"pcDays"`      // PCs not seen for this many days are dropped from the list of PCs; 0 keeps them for ever
	Default         string            `json:"default"`     // "" (wait), "local", "iso:<id>"; older command lines also gave "index:<n>" and "pick:<text>"
	Hidden          map[string]bool   `json:"hidden"`      // ISO id -> not in the menu
	Titles          map[string]string `json:"titles"`      // ISO id -> the name shown instead of the file name
	Args            map[string]string `json:"args"`        // ISO id -> extra kernel arguments added to the ones its kind of ISO gets (for finding faults, or a setting that one ISO needs)
	Rules           []Rule            `json:"rules"`
	PCNames         map[string]string `json:"pcNames"`         // MAC address -> the name a person gave the PC; kept when the PC is forgotten from the list
	AdminHash       string            `json:"adminHash"`       // see package admin
	SMBPassword     string            `json:"smbPassword"`     // what Windows' set-up logs in to the share with; it is inside the boot.wim copies, so it stays the same
	Library         Library           `json:"library"`         // the folder with the ISOs; empty until one is chosen
	NoAutoStart     bool              `json:"noAutoStart"`     // the services stay stopped when the program starts, until they are started in the web interface
	Access          Access            `json:"access"`          // which addresses may use the server, besides the local network
	ImportLimitMBps int               `json:"importLimitMBps"` // most MB a second that an import may use while PCs are installing; 0 is no limit
	Cache           CacheSettings     `json:"cache"`
}

// Library is the folder the ISOs are in, as it was added (see package library): its path and the id that was written into its marker.
type Library struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

// DefaultPCDays is how long a PC stays in the list after it was last seen, unless changed.
const DefaultPCDays = 90

var (
	idPattern     = regexp.MustCompile(`^[0-9a-f]{10}$`)
	defaultAction = regexp.MustCompile(`^(|local|iso:[0-9a-f]{10}|index:[0-9]{1,3}|pick:[^\r\n]{1,80})$`)
)

// NormalizeMAC returns the MAC address in lower case with colons, or false when it is not one.
func NormalizeMAC(s string) (string, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "-", ":"))
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return "", false
	}
	return strings.ToLower(hw.String()), true
}

// ValidAction says whether a rule may have this action.
func ValidAction(a string) bool {
	if a == "menu" || a == "local" {
		return true
	}
	if id, ok := strings.CutPrefix(a, "native:"); ok { // started by the PC's own firmware, so that UEFI Secure Boot can stay on
		return idPattern.MatchString(id)
	}
	id, ok := strings.CutPrefix(a, "iso:")
	return ok && idPattern.MatchString(id)
}

// Validate checks what a person may have typed and cleans it up.
func (c *Config) Validate() error {
	if c.MenuTimeout < 0 || c.MenuTimeout > 3600 {
		return errors.New("the timeout must be between 0 and 3600 seconds")
	}
	if c.PCDays < 0 || c.PCDays > 3650 {
		return errors.New("the number of days must be between 0 and 3650")
	}
	if !defaultAction.MatchString(c.Default) {
		return errors.New("the default entry is not valid")
	}
	switch c.Cache.Where {
	case "", "auto", "local", "library":
	default:
		return errors.New("the cache place must be auto, local or library")
	}
	if c.Cache.LocalLimitMB < 0 || c.Cache.LocalLimitMB > 100000000 {
		return errors.New("the size of the cache must be between 0 and 100000000 MB")
	}
	if c.ImportLimitMBps < 0 || c.ImportLimitMBps > 100000 {
		return errors.New("the limit for imports must be between 0 and 100000 MB a second")
	}
	if _, err := access.Parse(c.Access.Networks); err != nil {
		return err
	}
	if (c.Library.Path == "") != (c.Library.ID == "") || strings.ContainsAny(c.Library.Path, "\r\n\x00") || len(c.Library.Path) > 4096 {
		return errors.New("the library setting is not valid")
	}
	for id, t := range c.Titles {
		if !idPattern.MatchString(id) {
			delete(c.Titles, id)
			continue
		}
		t = strings.TrimSpace(t)
		if t == "" {
			delete(c.Titles, id)
			continue
		}
		if strings.ContainsAny(t, "\r\n") || len(t) > 120 {
			return errors.New("a name must be one line of at most 120 characters")
		}
		c.Titles[id] = t
	}
	for id, h := range c.Hidden {
		if !idPattern.MatchString(id) || !h {
			delete(c.Hidden, id)
		}
	}
	for id, a := range c.Args {
		a = strings.TrimSpace(a)
		if !idPattern.MatchString(id) || a == "" {
			delete(c.Args, id)
			continue
		}
		if strings.ContainsAny(a, "\r\n") || len(a) > 300 {
			return errors.New("extra kernel arguments must be one line of at most 300 characters")
		}
		c.Args[id] = a
	}
	seen := map[string]bool{}
	rules := []Rule{}
	for _, r := range c.Rules {
		mac, ok := NormalizeMAC(r.MAC)
		if !ok {
			return fmt.Errorf("%q is not a MAC address", r.MAC)
		}
		if !ValidAction(r.Action) {
			return fmt.Errorf("the action %q is not valid", r.Action)
		}
		if seen[mac] {
			return fmt.Errorf("%s has two rules", mac)
		}
		seen[mac] = true
		r.MAC = mac
		r.Note = strings.TrimSpace(r.Note)
		if strings.ContainsAny(r.Note, "\r\n") || len(r.Note) > 120 {
			return errors.New("a note must be one line of at most 120 characters")
		}
		rules = append(rules, r)
	}
	c.Rules = rules
	for mac, name := range c.PCNames {
		key, ok := NormalizeMAC(mac)
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			delete(c.PCNames, mac)
			continue
		}
		if strings.IndexFunc(name, unicode.IsControl) >= 0 || utf8.RuneCountInString(name) > MaxPCName {
			return fmt.Errorf("a PC's name must be one line of at most %d characters", MaxPCName)
		}
		if key != mac {
			delete(c.PCNames, mac)
		}
		c.PCNames[key] = name
	}
	return nil
}

// MaxPCName is the longest name a PC may be given, in characters.
const MaxPCName = 60

func (c Config) clone() Config {
	d := c
	d.Hidden = map[string]bool{}
	for k, v := range c.Hidden {
		d.Hidden[k] = v
	}
	d.Titles = map[string]string{}
	for k, v := range c.Titles {
		d.Titles[k] = v
	}
	d.Args = map[string]string{}
	for k, v := range c.Args {
		d.Args[k] = v
	}
	d.PCNames = map[string]string{}
	for k, v := range c.PCNames {
		d.PCNames[k] = v
	}
	d.Rules = append([]Rule(nil), c.Rules...)
	d.Access.Networks = append([]string(nil), c.Access.Networks...)
	return d
}

// Store keeps the settings in memory and in a file, which is written whole and replaced in one step so that a crash never leaves half a file.
type Store struct {
	path string

	mu  sync.RWMutex
	cfg Config
}

// Open reads the file; when there is none yet the seed (the command-line values) is used and written. created tells which of the two happened.
func Open(path string, seed Config) (s *Store, created bool, err error) {
	s = &Store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
		var probe struct{ PCDays *int }
		if json.Unmarshal(data, &probe) == nil && probe.PCDays == nil {
			s.cfg.PCDays = DefaultPCDays // a file from before the setting existed
		}
		if s.cfg.Hidden == nil {
			s.cfg.Hidden = map[string]bool{}
		}
		if s.cfg.Titles == nil {
			s.cfg.Titles = map[string]string{}
		}
		if s.cfg.PCNames == nil {
			s.cfg.PCNames = map[string]string{}
		}
	case errors.Is(err, os.ErrNotExist):
		s.cfg = seed.clone()
		if err := s.cfg.Validate(); err != nil {
			return nil, false, err
		}
		created = true
		if err := s.save(); err != nil {
			return nil, false, err
		}
	default:
		return nil, false, err
	}
	return s, created, nil
}

// Get is a copy of the settings: change it freely, nothing happens until Update.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.clone()
}

// Path is where the settings are kept.
func (s *Store) Path() string { return s.path }

// Update lets fn change a copy of the settings, checks it and, when all is well, keeps it and writes the file.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	prev := s.cfg
	s.cfg = next
	if err := s.save(); err != nil {
		s.cfg = prev
		return err
	}
	return nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".bootlace-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Access says which addresses the server answers: the local network always (see package access), and these as well.
type Access struct {
	Networks    []string `json:"networks"`    // more networks, for example 100.64.0.0/10 for a VPN
	AllowPublic bool     `json:"allowPublic"` // answer any address (the server is then reachable from the internet if its ports are)
}

// CacheSettings say where the server keeps what it makes once and reuses (unpacked images, copies of boot.wim).
type CacheSettings struct {
	Where        string `json:"where"`        // "" or "auto" (the container's own folder while it has room, then the library), "local", "library"
	LocalLimitMB int    `json:"localLimitMB"` // how much the container's own folder may hold in "auto" mode; 0 is the default (2048)
}
