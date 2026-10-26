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
