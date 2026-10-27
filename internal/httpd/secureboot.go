// The signed-iPXE route to a PC with UEFI Secure Boot on (a prototype, off unless the server is started with -secure-boot-dir): the DHCP answer names a Microsoft-signed
// distribution shim, the shim asks for grubx64.efi next to itself, and that file is iPXE signed with the owner's own key, which the shim accepts once the key is enrolled in
// the PC (MokManager, "Enroll key from disk"). Nothing here signs or builds anything: tools/ipxe/build-sb.sh makes the signed iPXE and the folder is filled by hand.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// SecureBoot is what the server needs to offer the signed-iPXE route. The zero value offers nothing.
type SecureBoot struct {
	// Dir is the folder with the shim and the files it fetches (the second stage, MokManager). Empty: the route is off. Every file directly in it is served by TFTP under
	// "sb/", by its own name; the folder is never browsed (no sub-folder, no path).
	Dir string
	// Shim is the file in Dir that the DHCP answer names (default shimx64.efi). A shim chooses its second stage by its own name: a distribution's shim asks for grubx64.efi
	// (put iPXE, signed with your key, under that name), the iPXE project's shim asks for the name without "shim" (ipxe-shimx64.efi loads ipxe.efi).
	Shim string
	// All sends every 64-bit UEFI PC that asks for a network boot through the shim, not only the PCs marked in the web interface (config.PCOptions.SecureBoot). The shim also
	// starts with Secure Boot off, but a PC that could have used iPXE directly then needs the shim's extra steps for nothing.
	All bool
}

// secureBootPrefix is the folder name the shim route's files are served under, so that they never shadow another file.
const secureBootPrefix = "sb/"

// DefaultShim is the file name of the shim when none is given.
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

// Check says whether the folder can be used: the shim must be there.
func (sb SecureBoot) Check() error {
	if sb.Dir == "" {
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

// Enabled says whether the route is offered at all.
func (sb SecureBoot) Enabled() bool { return sb.Dir != "" }

// Mode is how the route is offered, for the web interface: "" (off), "pcs" (to the PCs marked for it) or "all".
func (sb SecureBoot) Mode() string {
	switch {
	case !sb.Enabled():
		return ""
	case sb.All:
		return "all"
	}
	return "pcs"
}

// SecondStage is the file the shim asks for after itself, by TFTP from the same folder. The iPXE project's shim (its file is called ipxe-shimx64.efi here) asks for ipxe.efi, a
// distribution's shim for grubx64.efi: the name is guessed from the shim's file name, which is the only thing known about it without reading it.
func (sb SecureBoot) SecondStage() string {
	if strings.Contains(strings.ToLower(sb.shim()), "ipxe") {
		return "ipxe.efi"
	}
	return "grubx64.efi"
}

// mokManager is the file a distribution's shim starts to enrol a key (Way 1 in docs/secure-boot.md); the iPXE project's shim needs none.
const mokManager = "mmx64.efi"

// SecureBootFileState is one file the route expects in the folder.
type SecureBootFileState struct {
	Name     string `json:"name"`
	Role     string `json:"role"`     // what the file is for, in a few words
	Required bool   `json:"required"` // the route cannot work without it
	Present  bool   `json:"present"`
}

// SecureBootStatus is what the web interface shows about the route: whether it is on, the folder, what is in it, and one plain sentence.
type SecureBootStatus struct {
	On      bool                  `json:"on"`
	Mode    string                `json:"mode"` // "", "pcs" or "all"
	Dir     string                `json:"dir,omitempty"`
	Shim    string                `json:"shim,omitempty"`
	Marked  int                   `json:"marked"` // PCs marked for the route on the PCs page (they matter when Mode is "pcs")
	Files   []SecureBootFileState `json:"files,omitempty"`
	Ready   bool                  `json:"ready"`   // on, and every required file is there
	Problem string                `json:"problem"` // what is missing or wrong, in a sentence; empty when Ready or off
	Message string                `json:"message"` // the state in a sentence
}

// Status looks in the folder now (the files are read at every request, so a file copied in later counts without a restart).
func (sb SecureBoot) Status() SecureBootStatus {
	st := SecureBootStatus{On: sb.Enabled(), Mode: sb.Mode(), Dir: sb.Dir}
	if !st.On {
		st.Message = "The Secure Boot route is off. A PC with UEFI Secure Boot on refuses iPXE and stops with \"Security Violation\". To turn it on, set BOOTLACE_SECURE_BOOT_DIR (see docs/secure-boot.md)."
		return st
	}
	st.Shim = sb.shim()
	have := map[string]bool{}
	entries, err := os.ReadDir(sb.Dir)
	for _, e := range entries {
		if e.Type().IsRegular() {
			have[strings.ToLower(e.Name())] = true
		}
	}
	files := []SecureBootFileState{{Name: st.Shim, Role: "the boot file the PC is told to start (the shim)", Required: true}, {Name: sb.SecondStage(), Role: "what the shim starts next (iPXE)", Required: true}}
	if sb.SecondStage() == "grubx64.efi" {
		files = append(files, SecureBootFileState{Name: mokManager, Role: "MokManager, to enrol a key (only for a key of your own)"})
	}
	var missing []string
	for i := range files {
		files[i].Present = have[strings.ToLower(files[i].Name)]
		if files[i].Required && !files[i].Present {
			missing = append(missing, files[i].Name)
		}
	}
	st.Files = files
	switch {
	case err != nil:
		st.Problem = fmt.Sprintf("The Secure Boot folder %s cannot be read (%v), so no PC can be sent through the shim.", sb.Dir, err)
	case !plainName(st.Shim):
		st.Problem = fmt.Sprintf("%q is not a file name, so no PC can be sent through the shim.", st.Shim)
	case len(missing) == 1 && missing[0] == st.Shim:
		st.Problem = fmt.Sprintf("%s is missing from %s: a PC sent through the shim cannot get it.", st.Shim, sb.Dir)
	case len(missing) > 0:
		st.Problem = fmt.Sprintf("%s is missing from %s: the shim asks for it next, so a PC that gets the shim then stops with \"Security Violation\" or an error. Copy it into the folder.", strings.Join(missing, " and "), sb.Dir)
	}
	st.Ready = st.Problem == ""
	who := "the PCs marked \"Secure Boot route\" on the PCs page"
	if sb.All {
		who = "every 64-bit UEFI PC"
	}
	if st.Ready {
		st.Message = fmt.Sprintf("The Secure Boot route is on for %s: they are told to start %s, which then fetches %s.", who, st.Shim, sb.SecondStage())
	} else {
		st.Message = st.Problem
	}
	return st
}

// Problems are the things that make the route fail although it is on and the shim is there (the shim itself is Check's business, which stops the start): at the moment, the
// second stage the shim asks for. They are told at the start and are not a reason to refuse it, because the folder is read at every request: the file can be copied in while
// the server runs, and a server that refuses to start would take the whole menu down for a folder that is only half filled.
func (sb SecureBoot) Problems() []string {
	if !sb.Enabled() {
		return nil
	}
	st := sb.Status()
	var out []string
	for _, f := range st.Files {
		if f.Required && !f.Present && f.Name != st.Shim {
			out = append(out, fmt.Sprintf("the Secure Boot folder %s has no %s, which the shim %s asks for next: a PC sent through the shim will stop with \"Security Violation\" (see docs/secure-boot.md)", sb.Dir, f.Name, st.Shim))
		}
	}
	return out
}

// SecureBootStatus is the route's status with the number of PCs marked for it.
func (s *Server) SecureBootStatus() SecureBootStatus {
	st := s.SecureBoot.Status()
	if s.Config != nil {
		for _, o := range s.Config.Get().PCOptions {
			if o.SecureBoot {
				st.Marked++
			}
		}
	}
	if st.Ready && st.Mode == "pcs" {
		if st.Marked == 0 {
			st.Message += " No PC is marked yet: press \"Secure Boot route\" on a PC's row of the PCs page (a PC appears there once it has asked for a boot file), or start with -secure-boot-all."
		} else {
			st.Message += fmt.Sprintf(" %d PC(s) are marked.", st.Marked)
		}
	}
	return st
}

// SecureBootFile is the boot file for a 64-bit UEFI PC that is to go through the shim: its name in the DHCP answer, or false when the route is off or this PC is not one
// that uses it.
func (s *Server) SecureBootFile(mac string) (string, bool) {
	if !s.SecureBoot.Enabled() {
		return "", false
	}
	m, ok := config.NormalizeMAC(mac)
	if !ok {
		return "", false
	}
	if !s.SecureBoot.All {
		if s.Config == nil || !s.Config.Get().PCOptions[m].SecureBoot {
			return "", false
		}
	}
	s.setAction(m, "secure boot: shim, then signed iPXE")
	return secureBootPrefix + s.SecureBoot.shim(), true
}

// secureBootTFTP answers a request under "sb/": a file directly in the folder, by its exact name (case does not matter).
func (s *Server) secureBootTFTP(client net.IP, rest string) (fs.File, bool) {
	if !s.SecureBoot.Enabled() || !plainName(rest) {
		return nil, false
	}
	entries, err := os.ReadDir(s.SecureBoot.Dir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.EqualFold(e.Name(), rest) {
			continue
		}
		f, err := os.Open(filepath.Join(s.SecureBoot.Dir, e.Name()))
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
// off, when no command loads a kernel, and for Windows (wimboot is not signed by anyone, a shim cannot help it).
func (s *Server) withShim(id, family string, cmds []string) []string {
	if !s.SecureBoot.Enabled() || strings.HasPrefix(family, "Windows") {
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
