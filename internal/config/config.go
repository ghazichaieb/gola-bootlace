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
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ghazichaieb/gola-bootlace/internal/ipxesafe"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
)

// Rule says where one PC goes when it starts from the network.
type Rule struct {
	MAC    string    `json:"mac"`    // lower case, colon separated
	Action string    `json:"action"` // "menu" (show the menu: the same as no rule), "local" (its own disk) or "iso:<id>"
	Note   string    `json:"note"`
	Added  time.Time `json:"added"`
}

// GroupRule says where every PC of a kind goes when it starts from the network: by the manufacturer or model its firmware reports, or by its platform. A rule for one PC (by
// MAC address) wins over a group rule; group rules apply in the order they are listed, the first that matches decides.
type GroupRule struct {
	Kind   string    `json:"kind"`   // "model" (the manufacturer and model text contains Match), "manufacturer" (the manufacturer text contains it) or "platform" ("efi" or "pcbios")
	Match  string    `json:"match"`  // compared without regard to case
	Action string    `json:"action"` // like Rule.Action; "menu" shows the menu and stops looking at later group rules
	Note   string    `json:"note"`
	Added  time.Time `json:"added"`
}

// GroupKinds are the kinds a GroupRule may have.
var GroupKinds = []string{"model", "manufacturer", "platform"}

// Matches says whether a PC with these facts (from its firmware) is in the group.
func (g GroupRule) Matches(manufacturer, product, platform string) bool {
	m := strings.ToLower(strings.TrimSpace(g.Match))
	if m == "" {
		return false
	}
	switch g.Kind {
	case "model":
		return strings.Contains(strings.ToLower(manufacturer+" "+product), m)
	case "manufacturer":
		return strings.Contains(strings.ToLower(manufacturer), m)
	case "platform":
		return strings.ToLower(platform) == m
	}
	return false
}

// Describe is the group in words, for the log and the menu script: `model "ThinkCentre"`.
func (g GroupRule) Describe() string { return g.Kind + ` "` + g.Match + `"` }

// Config is the whole file.
type Config struct {
	MenuTimeout     int                   `json:"menuTimeout"`   // seconds before the default entry starts; 0 waits for a choice
	PCDays          int                   `json:"pcDays"`        // PCs not seen for this many days are dropped from the list of PCs; 0 keeps them for ever
	Default         string                `json:"default"`       // "" (wait), "local", "iso:<id>"; older command lines also gave "index:<n>" and "pick:<text>"
	Hidden          map[string]bool       `json:"hidden"`        // ISO id -> not in the menu
	Titles          map[string]string     `json:"titles"`        // ISO id -> the name shown instead of the file name
	Debug           map[string]DebugMode  `json:"debug"`         // ISO id -> how to start it for finding faults (see DebugMode)
	Args            map[string]string     `json:"args"`          // ISO id -> extra kernel arguments added to the ones its kind of ISO gets (for finding faults, or a setting that one ISO needs)
	BootPasswords   map[string]string     `json:"bootPasswords"` // ISO id -> hash of the password the menu asks for before it starts that ISO (see package admin); a secret, not in backups without secrets
	Rules           []Rule                `json:"rules"`
	GroupRules      []GroupRule           `json:"groupRules"`      // rules for every PC of a kind (model, manufacturer, platform); a rule for one PC wins
	PCNames         map[string]string     `json:"pcNames"`         // MAC address -> the name a person gave the PC; kept when the PC is forgotten from the list
	PCOptions       map[string]PCOptions  `json:"pcOptions"`       // MAC address -> extra kernel arguments and debug switches for every ISO that PC starts from its files (see PCOptions)
	AdminHash       string                `json:"adminHash"`       // see package admin
	SMBPassword     string                `json:"smbPassword"`     // what Windows' set-up logs in to the share with; it is inside the boot.wim copies, so it stays the same
	Library         Library               `json:"library"`         // the folder with the ISOs; empty until one is chosen
	NoAutoStart     bool                  `json:"noAutoStart"`     // the services stay stopped when the program starts, until they are started in the web interface
	Access          Access                `json:"access"`          // which addresses may use the server, besides the local network
	ImportLimitMBps int                   `json:"importLimitMBps"` // most MB a second that an import may use while PCs are installing; 0 is no limit
	Cache           CacheSettings         `json:"cache"`
	Tuning          Tuning                `json:"tuning"`
	SetupChecks     SetupChecks           `json:"setupChecks"`    // checks of Windows 11 Setup to skip
	PasswordChosen  bool                  `json:"passwordChosen"` // the person set the web interface's password (it was not only made up at the first start)
	FirstStartDone  bool                  `json:"firstStartDone"` // the first-start guide was finished or skipped
	LibrarySync     string                `json:"librarySync"`    // fingerprint of the copy of the settings that was last written to, or taken from, the library (see package backup)
	MenuAccent      string                `json:"menuAccent"`     // the colour of the highlighted line in the menu: "" (Gola's red) or one of AccentNames
	HidePatterns    []string              `json:"hidePatterns"`   // ISOs whose path in the library matches one of these are not in the menu (see MatchesHide)
	Answers         map[string]AnswerFile `json:"answers"`        // answer file id -> the file (a secret: it can hold passwords; see answers.go)
	ISOAnswers      map[string]string     `json:"isoAnswers"`     // ISO id -> the answer file every PC that starts that ISO gets (a PC's own choice wins)
}

// AccentNames are the colours the menu's highlighted line may have (see package menu).
var AccentNames = []string{"red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// SetupChecks says which checks of Windows 11 Setup are skipped when a PC is installed through Bootlace. Each one lets Windows 11 be installed on a PC that Microsoft does not
// list as supported; Windows then runs there without a promise of updates or support from Microsoft. Nothing is skipped by default.
type SetupChecks struct {
	TPM        bool `json:"tpm,omitempty"`
	SecureBoot bool `json:"secureBoot,omitempty"`
	RAM        bool `json:"ram,omitempty"`
	CPU        bool `json:"cpu,omitempty"`
	Storage    bool `json:"storage,omitempty"`
}

// LabConfig are the names of the values of HKLM\SYSTEM\Setup\LabConfig that skip the checks that are on.
func (c SetupChecks) LabConfig() []string {
	var out []string
	for _, e := range []struct {
		on   bool
		name string
	}{{c.TPM, "BypassTPMCheck"}, {c.SecureBoot, "BypassSecureBootCheck"}, {c.RAM, "BypassRAMCheck"}, {c.CPU, "BypassCPUCheck"}, {c.Storage, "BypassStorageCheck"}} {
		if e.on {
			out = append(out, e.name)
		}
	}
	return out
}

// Tuning are the knobs of the network boot itself, for firmware and switches that do not cope with the defaults.
type Tuning struct {
	TFTPBlockSize int `json:"tftpBlockSize"` // the largest TFTP block to agree to; 0 follows what each client asks for (up to 65464)
	TFTPWindow    int `json:"tftpWindow"`    // blocks sent ahead to FreeBSD's loader; 0 keeps the program's own number (-tftp-window), 1 sends one at a time
}

// MaxHidePatterns and MaxHidePattern bound the list of patterns and the length of one.
const (
	MaxHidePatterns = 200
	MaxHidePattern  = 200
)

// MatchesHide says whether the path of an ISO in the library (with "/" between folders) matches one of the patterns. A pattern is compared without regard to case.
// "*" stands for any text (also across folders) and "?" for one character, so "old/*" is everything in the folder old and "*beta*" any path with beta in it. A
// pattern without them that ends in "/" is a folder (everything under it); any other must be the whole path or the file's own name.
func MatchesHide(patterns []string, rel string) bool {
	rel = strings.ToLower(rel)
	base := rel[strings.LastIndex(rel, "/")+1:]
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "":
		case strings.ContainsAny(p, "*?"):
			if glob(p, rel) {
				return true
			}
		case strings.HasSuffix(p, "/"):
			if strings.HasPrefix(rel, p) {
				return true
			}
		case p == rel || p == base:
			return true
		}
	}
	return false
}

// glob reports whether s matches the pattern, where * is any text and ? is any one character.
func glob(p, s string) bool {
	pr, sr := []rune(p), []rune(s)
	star, mark := -1, 0
	pi, si := 0, 0
	for si < len(sr) {
		switch {
		case pi < len(pr) && (pr[pi] == '?' || pr[pi] == sr[si]) && pr[pi] != '*':
			pi++
			si++
		case pi < len(pr) && pr[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi, mark = star+1, mark+1
			si = mark
		default:
			return false
		}
	}
	for pi < len(pr) && pr[pi] == '*' {
		pi++
	}
	return pi == len(pr)
}

// DebugMode is how an ISO is started when it does not start well: Verbose takes out the quiet start-up (no `quiet`, `splash`, `rhgb`) and turns the kernel's messages
// up; Serial also sends the console to the first serial port (115200 baud), for a PC or a virtual machine whose serial port is being recorded; Shell makes the start-up stop
// at a shell before the system goes on (the word depends on the kind of system, see family.ShellWords). The three work alone or together: verbose alone never opens a shell.
type DebugMode struct {
	Verbose bool `json:"verbose,omitempty"`
	Serial  bool `json:"serial,omitempty"`
	Shell   bool `json:"shell,omitempty"`
}

// On says that at least one switch is set.
func (d DebugMode) On() bool { return d.Verbose || d.Serial || d.Shell }

// PCOptions are how one PC is started when it needs it (a graphics card that wants `nomodeset`, a PC whose serial port is being recorded): the arguments come after the ones
// of the ISO itself and the switches work like those of an ISO (see DebugMode), on the kernel line of every ISO that PC starts from its own files.
type PCOptions struct {
	Args   string    `json:"args,omitempty"`
	Debug  DebugMode `json:"debug,omitempty"`
	Answer string    `json:"answer,omitempty"` // the answer file this PC gets for the ISOs that take one: an id, NoAnswer to keep it away, or empty to follow the ISO
}

// Empty says that nothing is set: such an entry is not kept.
func (o PCOptions) Empty() bool {
	return o.Args == "" && !o.Debug.On() && o.Answer == ""
}

// MaxArgs is the longest extra kernel arguments line.
const MaxArgs = 300

// CleanArgs checks a line of extra kernel arguments (one line, MaxArgs characters at most) and returns it without the spaces around it.
func CleanArgs(a string) (string, error) {
	a = strings.TrimSpace(a)
	if strings.ContainsAny(a, "\r\n") || len(a) > MaxArgs {
		return "", fmt.Errorf("extra kernel arguments must be one line of at most %d characters", MaxArgs)
	}
	if why := ipxesafe.UnsafeArgs(a); why != "" {
		return "", errors.New(why)
	}
	return a, nil
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
	if c.MenuAccent != "" && !slices.Contains(AccentNames, c.MenuAccent) {
		return errors.New("the accent colour must be one of: " + strings.Join(AccentNames, ", "))
	}
	if c.Tuning.TFTPBlockSize != 0 && (c.Tuning.TFTPBlockSize < 512 || c.Tuning.TFTPBlockSize > 65464) {
		return errors.New("the TFTP block size must be 0 (follow the PC) or between 512 and 65464")
	}
	if c.Tuning.TFTPWindow < 0 || c.Tuning.TFTPWindow > 16 {
		return errors.New("the TFTP window must be 0 (the program's own number) or between 1 and 16")
	}
	if len(c.HidePatterns) > MaxHidePatterns {
		return fmt.Errorf("at most %d patterns", MaxHidePatterns)
	}
	patterns := []string{}
	for _, p := range c.HidePatterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, "\r\n\x00") || utf8.RuneCountInString(p) > MaxHidePattern {
			return fmt.Errorf("a pattern must be one line of at most %d characters", MaxHidePattern)
		}
		patterns = append(patterns, p)
	}
	c.HidePatterns = patterns
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
		clean, err := CleanArgs(a)
		if err != nil {
			return err
		}
		c.Args[id] = clean
	}
	for id, h := range c.BootPasswords {
		if !idPattern.MatchString(id) || h == "" {
			delete(c.BootPasswords, id)
		}
	}
	for id, d := range c.Debug {
		if !idPattern.MatchString(id) || !d.On() {
			delete(c.Debug, id)
		}
	}
	pcOptions := map[string]PCOptions{}
	for mac, o := range c.PCOptions {
		key, ok := NormalizeMAC(mac)
		if !ok {
			return fmt.Errorf("%q is not a MAC address", mac)
		}
		args, err := CleanArgs(o.Args)
		if err != nil {
			return err
		}
		o.Args = args
		if !o.Empty() {
			pcOptions[key] = o
		}
	}
	c.PCOptions = pcOptions
	if err := c.validateAnswers(); err != nil {
		return err
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
	groups := []GroupRule{}
	seenGroup := map[string]bool{}
	for _, g := range c.GroupRules {
		g.Match = strings.TrimSpace(g.Match)
		g.Note = strings.TrimSpace(g.Note)
		if !slices.Contains(GroupKinds, g.Kind) {
			return fmt.Errorf("a group rule is for a model, a manufacturer or a platform, not %q", g.Kind)
		}
		if g.Match == "" || utf8.RuneCountInString(g.Match) > 60 || strings.IndexFunc(g.Match, unicode.IsControl) >= 0 {
			return errors.New("what a group rule matches must be one line of 1 to 60 characters")
		}
		if g.Kind == "platform" {
			g.Match = strings.ToLower(g.Match)
			if g.Match != "efi" && g.Match != "pcbios" {
				return errors.New("a platform is efi (UEFI) or pcbios (BIOS)")
			}
		}
		if !ValidAction(g.Action) {
			return fmt.Errorf("the action %q is not valid", g.Action)
		}
		if strings.ContainsAny(g.Note, "\r\n") || len(g.Note) > 120 {
			return errors.New("a note must be one line of at most 120 characters")
		}
		key := g.Kind + "|" + strings.ToLower(g.Match)
		if seenGroup[key] {
			return fmt.Errorf("two group rules for %s", g.Describe())
		}
		seenGroup[key] = true
		groups = append(groups, g)
	}
	c.GroupRules = groups
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
	d.BootPasswords = map[string]string{}
	for k, v := range c.BootPasswords {
		d.BootPasswords[k] = v
	}
	d.Debug = map[string]DebugMode{}
	for k, v := range c.Debug {
		d.Debug[k] = v
	}
	d.Args = map[string]string{}
	for k, v := range c.Args {
		d.Args[k] = v
	}
	d.PCNames = map[string]string{}
	for k, v := range c.PCNames {
		d.PCNames[k] = v
	}
	d.PCOptions = map[string]PCOptions{}
	for k, v := range c.PCOptions {
		d.PCOptions[k] = v
	}
	d.Answers = map[string]AnswerFile{}
	for k, v := range c.Answers {
		d.Answers[k] = v
	}
	d.ISOAnswers = map[string]string{}
	for k, v := range c.ISOAnswers {
		d.ISOAnswers[k] = v
	}
	d.Rules = append([]Rule(nil), c.Rules...)
	d.GroupRules = append([]GroupRule(nil), c.GroupRules...)
	d.Access.Networks = append([]string(nil), c.Access.Networks...)
	d.HidePatterns = append([]string(nil), c.HidePatterns...)
	return d
}

// Store keeps the settings in memory and in a file, which is written whole and replaced in one step so that a crash never leaves half a file.
type Store struct {
	path string

	mu  sync.RWMutex
	cfg Config

	hooks    []func(Config)
	queue    []Config // changes the OnChange functions have not heard yet
	draining bool
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
		var probe struct {
			PCDays         *int
			FirstStartDone *bool
		}
		if json.Unmarshal(data, &probe) == nil && probe.PCDays == nil {
			s.cfg.PCDays = DefaultPCDays // a file from before the setting existed
		}
		if json.Unmarshal(data, &probe) == nil && probe.FirstStartDone == nil && s.cfg.AdminHash != "" {
			s.cfg.FirstStartDone = true // an installation from before the guide existed has been through its first start
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
		if s.cfg.BootPasswords == nil {
			s.cfg.BootPasswords = map[string]string{}
		}
		if s.cfg.Debug == nil {
			s.cfg.Debug = map[string]DebugMode{}
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

// OnChange registers a function that is called, with a copy of the new settings, after every Update that was kept (not after UpdateQuiet). It runs on its own goroutine, one
// call at a time in the order of the changes.
func (s *Store) OnChange(f func(Config)) {
	s.mu.Lock()
	s.hooks = append(s.hooks, f)
	s.mu.Unlock()
}

// Update lets fn change a copy of the settings, checks it and, when all is well, keeps it and writes the file.
func (s *Store) Update(fn func(*Config) error) error { return s.update(fn, true) }

// UpdateQuiet is Update for a change that the OnChange functions must not hear about (they would only write it down again).
func (s *Store) UpdateQuiet(fn func(*Config) error) error { return s.update(fn, false) }

func (s *Store) update(fn func(*Config) error, notify bool) error {
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
	if notify && len(s.hooks) > 0 {
		s.queue = append(s.queue, next.clone())
		if !s.draining {
			s.draining = true
			go s.drain()
		}
	}
	return nil
}

// drain calls the OnChange functions for the changes in the queue, in order, on one goroutine at a time. A function may use the store.
func (s *Store) drain() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.draining = false
			s.mu.Unlock()
			return
		}
		c, hooks := s.queue[0], s.hooks
		s.queue = s.queue[1:]
		s.mu.Unlock()
		for _, h := range hooks {
			h(c)
		}
	}
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
