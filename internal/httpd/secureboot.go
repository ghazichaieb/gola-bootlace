// The route to a PC with UEFI Secure Boot on: the DHCP answer names a shim that Microsoft signed, the shim asks for iPXE next to itself, and iPXE (signed by someone the shim
// trusts) shows the menu. Two ways, chosen in the web interface or at the start (see docs/secure-boot.md):
//
//   - "builtin": the iPXE project's own signed shim and iPXE, which are part of this program (package secureboot): nothing to download, no key in any PC.
//   - "own": a folder with a distribution's shim and an iPXE signed with the owner's own key, which each PC has to enrol once (MokManager). Nothing here signs or builds
//     anything: tools/ipxe/build-sb.sh makes the signed iPXE and the folder is filled by hand.
//
// The third mode, "off", serves nothing. Which PCs are sent through the shim is the scope: "all" 64-bit UEFI PCs, or only the PCs marked on the PCs page.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/secureboot"
)

// SecureBoot is what the program was started with: the options and the environment. They win over what the web interface saved, and the web interface shows them as such.
// The zero value says nothing, so the saved setting (or, with none, the default) decides.
type SecureBoot struct {
	// Mode is -secure-boot: "builtin", "own" or "off". Empty: a folder (Dir) means "own", otherwise the saved setting decides.
	Mode string
	// Scope is -secure-boot-scope: "all" or "marked". Empty: All, otherwise the saved setting decides.
	Scope string
	// Dir is -secure-boot-dir, the folder of the "own" mode: a shim and what it fetches (the second stage, MokManager). Every file directly in it is served by TFTP under
	// "sb/", by its own name; the folder is never browsed (no sub-folder, no path).
	Dir string
	// Shim is the file in Dir that the DHCP answer names in the "own" mode (default shimx64.efi). A shim chooses its second stage by its own name: a distribution's shim asks for
	// grubx64.efi (put iPXE, signed with your key, under that name), the iPXE project's shim asks for the name without "shim" (ipxe-shimx64.efi loads ipxe.efi).
	Shim string
	// All is -secure-boot-all: every 64-bit UEFI PC that asks for a network boot goes through the shim, not only the PCs marked in the web interface (config.PCOptions.SecureBoot).
	// The shim also starts with Secure Boot off, but a PC that could have used iPXE directly then gets an extra boot stage for nothing.
	All bool
	// OwnDir is the folder of the "own" mode when none was given with Dir (the web interface can switch to "own" without any option).
	OwnDir string
}

// Where a setting came from, for the web interface and the log.
const (
	SourceOption    = "option"    // the command line or the environment
	SourceDashboard = "dashboard" // a person chose it in the web interface
	SourceDefault   = "default"   // nobody chose it: what a new installation starts with, or the setting of an older one
)

// The errors of SetSecureBoot: a value that is not one of the choices (a bad request), and a change that cannot be made now (a conflict).
var (
	ErrSecureBootInvalid = errors.New("invalid Secure Boot setting")
	ErrSecureBootRefused = errors.New("Secure Boot setting refused")
)

// secureBootPrefix is the folder name the shim route's files are served under, so that they never shadow another file.
const secureBootPrefix = secureboot.Prefix

// DefaultShim is the file name of the shim of the "own" mode when none is given.
const DefaultShim = "shimx64.efi"

func (sb SecureBoot) shim() string {
	if sb.Shim == "" {
		return DefaultShim
	}
	return sb.Shim
}

// plainName says whether a name is a file name and nothing more: no folder, no drive, no way up.
func plainName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\:`) && !strings.ContainsRune(n, 0)
}

// optionMode is the mode the options ask for: -secure-boot, or "own" for a folder given with -secure-boot-dir; empty when they say nothing.
func (sb SecureBoot) optionMode() string {
	switch {
	case sb.Mode != "":
		return sb.Mode
	case sb.Dir != "":
		return "own"
	}
	return ""
}

// Check says whether the options can be used: the values must be known ones, and a folder given with -secure-boot-dir must hold the shim.
func (sb SecureBoot) Check() error {
	if sb.Mode != "" && !slices.Contains(config.SecureBootModes, sb.Mode) {
		return fmt.Errorf("-secure-boot must be %s, not %q", strings.Join(config.SecureBootModes, ", "), sb.Mode)
	}
	if sb.Scope != "" && !slices.Contains(config.SecureBootScopes, sb.Scope) {
		return fmt.Errorf("-secure-boot-scope must be %s, not %q", strings.Join(config.SecureBootScopes, " or "), sb.Scope)
	}
	if sb.optionMode() != "own" || sb.Dir == "" {
		return nil
	}
	if !plainName(sb.shim()) {
		return fmt.Errorf("%q is not a file name", sb.shim())
	}
	if st, err := os.Stat(filepath.Join(sb.Dir, sb.shim())); err != nil || st.IsDir() {
		return fmt.Errorf("the Secure Boot folder %q has no %s", sb.Dir, sb.shim())
	}
	return nil
}

// route is the setting as it is now: the options first, then what the web interface saved, then the default.
type route struct {
	Mode, ModeSource   string // "builtin", "own" or "off"; Source* says where it came from
	Scope, ScopeSource string // "all" or "marked"
	// Unset is true when nobody has chosen a mode and no option says one: an installation from before the route was built in, which stays off until a person decides.
	Unset         bool
	Dir, Shim     string // the folder and boot file of the "own" mode
	DirFromOption bool
}

// resolve combines the options with the saved setting. A new installation has "builtin" and "all" saved (as the default, chosen=false); a settings file from before the
// setting existed has nothing saved, which means "off" and "marked": nothing changes on an upgrade.
func (sb SecureBoot) resolve(savedMode, savedScope string, chosen bool) route {
	var r route
	saved := func() string {
		if chosen {
			return SourceDashboard
		}
		return SourceDefault
	}
	switch {
	case sb.optionMode() != "":
		r.Mode, r.ModeSource = sb.optionMode(), SourceOption
	case savedMode != "":
		r.Mode, r.ModeSource = savedMode, saved()
	default:
		r.Mode, r.ModeSource, r.Unset = "off", SourceDefault, true
	}
	switch {
	case sb.Scope != "":
		r.Scope, r.ScopeSource = sb.Scope, SourceOption
	case sb.All:
		r.Scope, r.ScopeSource = "all", SourceOption
	case savedScope != "":
		r.Scope, r.ScopeSource = savedScope, saved()
	default:
		r.Scope, r.ScopeSource = "marked", SourceDefault
	}
	r.Dir, r.DirFromOption = sb.OwnDir, false
	if sb.Dir != "" {
		r.Dir, r.DirFromOption = sb.Dir, true
	}
	r.Shim = sb.shim()
	return r
}

// builtin is the set of built-in files: the program's own unless a test put another one in.
func (s *Server) builtin() *secureboot.Set {
	if s.BuiltinSecureBoot != nil {
		return s.BuiltinSecureBoot
	}
	return secureboot.Builtin()
}

// route is the Secure Boot setting now (the saved part is read at every call, so a change in the web interface counts for the next DHCP answer).
func (s *Server) route() route {
	var mode, scope string
	var chosen bool
	if s.Config != nil {
		mode, scope, chosen = s.Config.SecureBoot()
	}
	return s.SecureBoot.resolve(mode, scope, chosen)
}

// on says whether the route is offered: the built-in files are there and match their manifest, or the own folder is named.
func (s *Server) on(r route) bool {
	switch r.Mode {
	case "builtin":
		return s.builtin().Err() == nil
	case "own":
		return r.Dir != ""
	}
	return false
}

// shimName is the boot file the DHCP answer names (under "sb/").
func (r route) shimName() string {
	if r.Mode == "builtin" {
		return secureboot.Shim
	}
	return r.Shim
}

// secondStage is the file the shim asks for after itself, by TFTP from the same folder. The iPXE project's shim (its file is called ipxe-shimx64.efi) asks for ipxe.efi, a
// distribution's shim for grubx64.efi: the name is guessed from the shim's file name, which is the only thing known about it without reading it.
func secondStage(shim string) string {
	if strings.Contains(strings.ToLower(shim), "ipxe") {
		return secureboot.SecondStage
	}
	return "grubx64.efi"
}

// mokManager is the file a distribution's shim starts to enrol a key (the own-key way in docs/secure-boot.md); the iPXE project's shim needs none.
const mokManager = "mmx64.efi"

// SecureBootFileState is one file the route expects.
type SecureBootFileState struct {
	Name     string `json:"name"`
	Role     string `json:"role"`     // what the file is for, in a few words
	Required bool   `json:"required"` // the route cannot work without it
	Present  bool   `json:"present"`
	Size     int64  `json:"size,omitempty"`   // built-in files: the size in bytes
	SHA256   string `json:"sha256,omitempty"` // built-in files: the checksum
	// Release, Source and SignedWith are set for a built-in file that comes from another release than the archive (the re-signed shim): its tag, its address, and the Microsoft CAs that signed it.
	Release    string   `json:"release,omitempty"`
	Source     string   `json:"source,omitempty"`
	SignedWith []string `json:"signedWith,omitempty"`
}

// SecureBootBuiltin says what is built into the program.
type SecureBootBuiltin struct {
	OK            bool                  `json:"ok"`              // the bytes in the program are the ones the manifest lists
	Error         string                `json:"error,omitempty"` // what is wrong when not OK
	Release       string                `json:"release"`         // the iPXE release the files are from, for example v2.0.0
	Released      string                `json:"released"`        // its date
	Added         string                `json:"added"`           // the day they were put into the program
	Source        string                `json:"source"`          // the address of the release archive
	TarballSHA256 string                `json:"tarballSha256"`   // of that archive
	Files         []SecureBootFileState `json:"files"`
}

// SecureBootOwn says what the folder of the "own" mode holds.
type SecureBootOwn struct {
	Dir           string                `json:"dir"`
	DirFromOption bool                  `json:"dirFromOption"`
	Shim          string                `json:"shim"`
	Files         []SecureBootFileState `json:"files"`
	Ready         bool                  `json:"ready"`   // the shim and the second stage are there
	Problem       string                `json:"problem"` // what is missing, in a sentence; empty when ready
}

// SecureBootStatus is what the web interface shows about the route: the mode and scope with where they came from, what is served, what is missing, and one plain sentence.
type SecureBootStatus struct {
	On          bool                  `json:"on"`   // the route is offered now
	Mode        string                `json:"mode"` // "builtin", "own" or "off"
	Scope       string                `json:"scope"`
	ModeSource  string                `json:"modeSource"` // "option", "dashboard" or "default"
	ScopeSource string                `json:"scopeSource"`
	ModeLocked  bool                  `json:"modeLocked"`  // an option decides the mode: the web interface cannot change it
	ScopeLocked bool                  `json:"scopeLocked"` // an option decides the scope
	Unset       bool                  `json:"unset"`       // an installation from before the route was built in, nobody has chosen yet: the page offers to turn it on
	Dir         string                `json:"dir,omitempty"`
	Shim        string                `json:"shim,omitempty"` // the boot file PCs are told to start (under sb/)
	Marked      int                   `json:"marked"`         // PCs marked for the route on the PCs page (they matter when Scope is "marked")
	Files       []SecureBootFileState `json:"files,omitempty"`
	Ready       bool                  `json:"ready"`   // on, and every required file is there
	Problem     string                `json:"problem"` // what is missing or wrong, in a sentence; empty when Ready or off
	Message     string                `json:"message"` // the state in a sentence
	Builtin     SecureBootBuiltin     `json:"builtin"`
	Own         SecureBootOwn         `json:"own"`
}

// ownFiles looks in the folder now (the files are read at every request, so a file copied in later counts without a restart) and says what is there and what is missing.
func ownFiles(dir, shim string) (files []SecureBootFileState, problem string) {
	if dir == "" {
		return nil, "No folder is set for the own-key way: give one with -secure-boot-dir."
	}
	have := map[string]bool{}
	entries, err := os.ReadDir(dir)
	for _, e := range entries {
		if e.Type().IsRegular() {
			have[strings.ToLower(e.Name())] = true
		}
	}
	files = []SecureBootFileState{{Name: shim, Role: "the boot file the PC is told to start (the shim)", Required: true}, {Name: secondStage(shim), Role: "what the shim starts next (iPXE)", Required: true}}
	if secondStage(shim) == "grubx64.efi" {
		files = append(files, SecureBootFileState{Name: mokManager, Role: "MokManager, to enrol a key (only for a key of your own)"})
	}
	var missing []string
	for i := range files {
		files[i].Present = have[strings.ToLower(files[i].Name)]
		if files[i].Required && !files[i].Present {
			missing = append(missing, files[i].Name)
		}
	}
	switch {
	case err != nil:
		problem = fmt.Sprintf("The Secure Boot folder %s cannot be read (%v), so no PC can be sent through the shim.", dir, err)
	case !plainName(shim):
		problem = fmt.Sprintf("%q is not a file name, so no PC can be sent through the shim.", shim)
	case len(missing) == 1 && missing[0] == shim:
		problem = fmt.Sprintf("%s is missing from %s: a PC sent through the shim cannot get it.", shim, dir)
	case len(missing) > 1:
		problem = fmt.Sprintf("%s are missing from %s: a PC sent through the shim cannot get the shim, and the shim asks for the second file next. Copy them into the folder.", strings.Join(missing, " and "), dir)
	case len(missing) == 1:
		problem = fmt.Sprintf("%s is missing from %s: the shim asks for it next, so a PC that gets the shim then stops with \"Security Violation\" or an error. Copy it into the folder.", missing[0], dir)
	}
	return files, problem
}

func (s *Server) builtinStatus() SecureBootBuiltin {
	set := s.builtin()
	b := SecureBootBuiltin{OK: set.Err() == nil}
	if set != nil {
		m := set.Manifest
		b.Release, b.Released, b.Added, b.Source, b.TarballSHA256 = m.Release, m.Released, m.Added, m.Source, m.TarballSHA256
	}
	if err := set.Err(); err != nil {
		b.Error = err.Error()
	}
	for _, f := range set.Infos() {
		b.Files = append(b.Files, SecureBootFileState{Name: f.Name, Role: f.Role, Required: true, Present: b.OK, Size: f.Size, SHA256: f.SHA256, Release: f.Release, Source: f.Source, SignedWith: f.SignedWith})
	}
	return b
}

// SecureBootStatus looks at the route now: the mode and scope, what is served, what is missing and the number of PCs marked for it.
func (s *Server) SecureBootStatus() SecureBootStatus {
	r := s.route()
	st := SecureBootStatus{Mode: r.Mode, Scope: r.Scope, ModeSource: r.ModeSource, ScopeSource: r.ScopeSource, Unset: r.Unset,
		ModeLocked: r.ModeSource == SourceOption, ScopeLocked: r.ScopeSource == SourceOption, Builtin: s.builtinStatus()}
	st.Own = SecureBootOwn{Dir: r.Dir, DirFromOption: r.DirFromOption, Shim: r.Shim}
	st.Own.Files, st.Own.Problem = ownFiles(r.Dir, r.Shim)
	st.Own.Ready = st.Own.Problem == ""
	if s.Config != nil {
		for _, o := range s.Config.Get().PCOptions {
			if o.SecureBoot {
				st.Marked++
			}
		}
	}
	st.On = s.on(r)
	who := "the PCs marked \"Secure Boot route\" on the PCs page"
	if r.Scope == "all" {
		who = "every 64-bit UEFI PC"
	}
	switch r.Mode {
	case "builtin":
		st.Shim = secureboot.Shim
		st.Files = st.Builtin.Files
		if !st.Builtin.OK {
			st.Problem = "The Secure Boot files built into this program do not match their manifest (" + st.Builtin.Error + "), so they are not served. Install the program again from a release."
		}
	case "own":
		st.Dir, st.Shim = r.Dir, r.Shim
		st.Files, st.Problem = st.Own.Files, st.Own.Problem
	default:
		st.Message = "The Secure Boot route is off. A PC with UEFI Secure Boot on refuses iPXE and stops with \"Security Violation\". Choose \"Built in\" to turn it on (see docs/secure-boot.md)."
		return st
	}
	st.Ready = st.Problem == ""
	switch {
	case !st.Ready:
		st.Message = st.Problem
	case r.Mode == "builtin":
		st.Message = fmt.Sprintf("The Secure Boot route is on for %s: they are told to start %s, which then fetches %s. Both files are part of this program (the iPXE project's release %s).", who, st.Shim, secureboot.SecondStage, st.Builtin.Release)
	default:
		st.Message = fmt.Sprintf("The Secure Boot route is on for %s: they are told to start %s from %s, which then fetches %s.", who, st.Shim, st.Dir, secondStage(st.Shim))
	}
	if st.Ready && r.Scope == "marked" {
		if st.Marked == 0 {
			st.Message += " No PC is marked yet: press \"Secure Boot route\" on a PC's row of the PCs page (a PC appears there once it has asked for a boot file), or choose every 64-bit UEFI PC."
		} else {
			st.Message += fmt.Sprintf(" %d PC(s) are marked.", st.Marked)
		}
	}
	return st
}

// SecureBootStartLines are the lines the log gets at the start: that the route is on (and for whom, from where) or off, with why and what to do about it, and a warning for each
// problem. A PC with Secure Boot on that is not sent through the shim only shows "Security Violation", so the log must say which it is.
func (s *Server) SecureBootStartLines() []string {
	r := s.route()
	why := map[string]string{
		SourceOption:    "set by the command line or the environment",
		SourceDashboard: "chosen in the web interface",
		SourceDefault:   "the default of a new installation; change it on the Server page",
	}[r.ModeSource]
	who := "the PCs marked for it on the PCs page"
	if r.Scope == "all" {
		who = "every 64-bit UEFI PC"
	}
	var lines []string
	if err := s.builtin().Err(); err != nil && r.Mode != "builtin" { // (the self-test of the built-in files runs whatever the mode, so that a damaged program is noticed)
		lines = append(lines, fmt.Sprintf("warning: the Secure Boot files built into this program do not match their manifest and will NOT be served if the built-in way is chosen (%v); install the program again from a release", err))
	}
	switch r.Mode {
	case "builtin":
		set := s.builtin()
		if err := set.Err(); err != nil {
			return []string{fmt.Sprintf("Secure Boot route: ERROR, the built-in Secure Boot files do not match their manifest and are NOT served (%v); install the program again from a release", err)}
		}
		lines = append(lines, fmt.Sprintf("Secure Boot route: ON, built in (the iPXE project's signed shim and iPXE, release %s; %s) for %s, which are told to start %s; the shim then asks for %s", set.Manifest.Release, why, who, secureboot.Shim, secureboot.SecondStage))
	case "own":
		if r.Dir == "" {
			return []string{fmt.Sprintf("Secure Boot route: ON in the own-key way (%s) but no folder is set: give one with -secure-boot-dir; no PC is sent through a shim", why)}
		}
		lines = append(lines, fmt.Sprintf("Secure Boot route: ON, own key (%s) for %s, which are told to start %s from %s; the shim then asks for %s", why, who, r.Shim, r.Dir, secondStage(r.Shim)))
		files, _ := ownFiles(r.Dir, r.Shim)
		for _, f := range files {
			if f.Required && !f.Present && f.Name != r.Shim {
				lines = append(lines, fmt.Sprintf("warning: the Secure Boot folder %s has no %s, which the shim %s asks for next: a PC sent through the shim will stop with \"Security Violation\" (see docs/secure-boot.md)", r.Dir, f.Name, r.Shim))
			}
		}
	default:
		if r.Unset {
			lines = append(lines, "Secure Boot route: OFF, so a PC with UEFI Secure Boot on will refuse iPXE (\"Security Violation\"). Secure Boot support is built in now and this installation has not chosen yet: turn it on on the Server page of the web interface, or start with -secure-boot builtin (see docs/secure-boot.md)")
		} else {
			lines = append(lines, fmt.Sprintf("Secure Boot route: OFF (%s), so a PC with UEFI Secure Boot on will refuse iPXE (\"Security Violation\"); to turn it on use the Server page of the web interface or -secure-boot builtin, see docs/secure-boot.md", why))
		}
	}
	return lines
}

// SetSecureBoot changes the saved mode and/or scope (an empty one is left as it is). It takes effect for the next DHCP answer. A setting that the command line or the
// environment decides cannot be changed here, "builtin" needs the built-in files to be intact, and "own" needs the shim in the folder: those are conflicts
// (ErrSecureBootRefused); an unknown value is ErrSecureBootInvalid.
func (s *Server) SetSecureBoot(mode, scope string) (SecureBootStatus, error) {
	if mode == "" && scope == "" {
		return SecureBootStatus{}, fmt.Errorf("%w: say the mode, the scope or both", ErrSecureBootInvalid)
	}
	if mode != "" && !slices.Contains(config.SecureBootModes, mode) {
		return SecureBootStatus{}, fmt.Errorf("%w: the mode must be one of: %s", ErrSecureBootInvalid, strings.Join(config.SecureBootModes, ", "))
	}
	if scope != "" && !slices.Contains(config.SecureBootScopes, scope) {
		return SecureBootStatus{}, fmt.Errorf("%w: the scope must be one of: %s", ErrSecureBootInvalid, strings.Join(config.SecureBootScopes, ", "))
	}
	if s.Config == nil {
		return SecureBootStatus{}, fmt.Errorf("%w: there are no settings to keep it in", ErrSecureBootRefused)
	}
	cur := s.route()
	if mode != "" && mode != cur.Mode && cur.ModeSource == SourceOption {
		return SecureBootStatus{}, fmt.Errorf("%w: the mode is set by the command line or the environment (-secure-boot, -secure-boot-dir, BOOTLACE_SECURE_BOOT); change it there and restart", ErrSecureBootRefused)
	}
	if scope != "" && scope != cur.Scope && cur.ScopeSource == SourceOption {
		return SecureBootStatus{}, fmt.Errorf("%w: the scope is set by the command line or the environment (-secure-boot-scope, -secure-boot-all, BOOTLACE_SECURE_BOOT_SCOPE); change it there and restart", ErrSecureBootRefused)
	}
	switch mode {
	case "builtin":
		if err := s.builtin().Err(); err != nil {
			return SecureBootStatus{}, fmt.Errorf("%w: the built-in files do not match their manifest (%v)", ErrSecureBootRefused, err)
		}
	case "own":
		switch {
		case cur.Dir == "":
			return SecureBootStatus{}, fmt.Errorf("%w: no folder is set for the own-key way (-secure-boot-dir)", ErrSecureBootRefused)
		case !plainName(cur.Shim) || !fileExists(filepath.Join(cur.Dir, cur.Shim)):
			return SecureBootStatus{}, fmt.Errorf("%w: %s is not in %s. Put the shim and the signed iPXE in that folder first (docs/secure-boot.md), then switch", ErrSecureBootRefused, cur.Shim, cur.Dir)
		}
	}
	err := s.Config.Update(func(c *config.Config) error {
		if mode != "" && cur.ModeSource != SourceOption { // (what an option decides is not copied into the saved settings)
			c.SecureBootMode = mode
			c.SecureBootChosen = true
		}
		if scope != "" && cur.ScopeSource != SourceOption {
			c.SecureBootScope = scope
			c.SecureBootChosen = true
			if c.SecureBootMode == "" { // (choosing only the scope of an installation that has no mode yet keeps it off, as it was)
				c.SecureBootMode = "off"
			}
		}
		return nil
	})
	if err != nil {
		return SecureBootStatus{}, fmt.Errorf("%w: %v", ErrSecureBootInvalid, err)
	}
	now := s.route()
	s.logf("Secure Boot route: mode %s, scope %s (changed in the web interface)", now.Mode, now.Scope)
	return s.SecureBootStatus(), nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// SecureBootFile is the boot file for a 64-bit UEFI PC that is to go through the shim: its name in the DHCP answer, or false when the route is off or this PC is not one
// that uses it.
func (s *Server) SecureBootFile(mac string) (string, bool) {
	r := s.route()
	if !s.on(r) {
		return "", false
	}
	m, ok := config.NormalizeMAC(mac)
	if !ok {
		return "", false
	}
	if r.Scope != "all" {
		if s.Config == nil || !s.Config.Get().PCOptions[m].SecureBoot {
			return "", false
		}
	}
	if r.Mode == "builtin" {
		s.setAction(m, "secure boot: shim, then signed iPXE (built in)")
	} else {
		s.setAction(m, "secure boot: shim, then signed iPXE")
	}
	return secureBootPrefix + r.shimName(), true
}

// secureBootTFTP answers a request under "sb/". Built in: exactly the two names of the built-in set, nothing else. Own key: a file directly in the folder, by its exact name
// (case does not matter). Off: nothing.
func (s *Server) secureBootTFTP(client net.IP, rest string) (fs.File, bool) {
	r := s.route()
	if !s.on(r) || !plainName(rest) {
		return nil, false
	}
	if r.Mode == "builtin" {
		b, ok := s.builtin().Open(rest)
		if !ok {
			return nil, false
		}
		s.logf("tftp: %s gets %s (Secure Boot route, built in)", client, rest)
		return &memFile{Reader: bytes.NewReader(b), name: rest, size: int64(len(b))}, true
	}
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.EqualFold(e.Name(), rest) {
			continue
		}
		f, err := os.Open(filepath.Join(r.Dir, e.Name()))
		if err != nil {
			return nil, false
		}
		s.logf("tftp: %s gets %s (Secure Boot route)", client, e.Name())
		return f, true // the *os.File is the answer and closes itself
	}
	return nil, false
}

// shimPath is where an ISO keeps the shim of its distribution (every distribution that boots with Secure Boot on has one there: Ubuntu, Debian, Fedora, Linux Mint, openSUSE).
const shimPath = "/EFI/boot/bootx64.efi"

// withShim adds, before the final "boot" of an entry that starts a Linux kernel, the iPXE command that has the kernel started by the ISO's own shim. With UEFI Secure Boot on
// iPXE hands the kernel to that shim, which checks the kernel's signature against the keys it carries (the distribution's), so a kernel that the distribution signed starts
// without the PC having to trust anything more; with Secure Boot off, or on a PC that boots by BIOS, the command does nothing. The commands are left alone when the route is
// off, when no command loads a kernel, and for Windows (wimboot is signed by Microsoft, but it is not a kernel a shim can check).
func (s *Server) withShim(id, family string, cmds []string) []string {
	if !s.on(s.route()) || strings.HasPrefix(family, "Windows") {
		return cmds
	}
	kernel := false
	for _, c := range cmds {
		if strings.HasPrefix(c, "kernel ") {
			kernel = true
		}
	}
	n := len(cmds) - 1
	if !kernel || n < 0 || cmds[n] != "boot" {
		return cmds
	}
	out := append([]string(nil), cmds[:n]...)
	out = append(out, "shim "+s.Base+"/fs/"+id+shimPath)
	return append(out, cmds[n:]...)
}
