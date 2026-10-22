// Windows started by the PC's own firmware, the way Windows Deployment Services does it: the firmware fetches Microsoft's boot manager by TFTP, and the
// boot manager fetches its boot configuration (BCD), boot.sdi and boot.wim from the same server. Every file of that chain is signed by Microsoft, so this
// works with UEFI Secure Boot on, which iPXE and wimboot (not signed by anyone the firmware knows) cannot.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"io/fs"
	"net"
	"os"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// windowsName turns what the boot manager asks for into the path of the file inside the ISO: it asks with backslashes and in any case, and the media's
// own BCD for UEFI is not where a PXE boot manager looks.
func windowsName(name string) string {
	n := strings.ToLower(strings.TrimLeft(strings.ReplaceAll(name, `\`, "/"), "/"))
	switch n {
	case "bootmgfw.efi":
		return "efi/boot/bootx64.efi"
	case "boot/bcd", "boot/x64/default.bcd", "boot/default.bcd":
		return "efi/microsoft/boot/bcd"
	}
	return n
}

// windowsTFTP answers the boot manager's requests from the ISO the PC chose.
func (s *Server) windowsTFTP(client net.IP, id, name string) (fs.File, bool) {
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return nil, false
	}
	want := windowsName(name)
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			return nil, false
		}
		img, err := disc.Open(f, iso.Size)
		if err != nil {
			f.Close()
			return nil, false
		}
		file, err := img.Open(want)
		if err != nil {
			f.Close()
			s.logf("tftp: %s asks for %s: not in %s", client, name, iso.Title)
			return nil, false
		}
		s.logf("tftp: %s gets %s (%s) from %s", client, name, path.Base(want), iso.Title)
		return &isoFile{ReadSeeker: file.Reader(), name: path.Base(want), size: file.Size(), closer: f}, true
	}
	return nil, false
}
