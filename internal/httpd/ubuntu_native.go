// Ubuntu started by the PC's own firmware: Microsoft signs Ubuntu's shim, the shim carries Canonical's key and checks GRUB, GRUB checks the kernel, so every step passes
// with UEFI Secure Boot on. The firmware fetches the shim from the ISO by TFTP (named ubuntu/<id>/bootx64.efi), the shim fetches GRUB (grubx64.efi) from the same place,
// and GRUB reads a configuration that this server makes (it names the ISO's own kernel and initramfs, read over HTTP, with the same arguments as the iPXE way).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"bytes"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// grubConfig is what GRUB reads: one entry, started at once.
func grubConfig(fsURL, isoURL string, kernel, initrd, extra, args string) []byte {
	host := strings.TrimPrefix(strings.TrimPrefix(fsURL, "http://"), "https://")
	host, _, _ = strings.Cut(host, "/")
	var b bytes.Buffer
	b.WriteString("# made by Gola Bootlace\nset timeout=0\nset default=0\n")
	more := ""
	if extra != "" { // a second archive, loaded after the ISO's own
		more = " (http," + host + ")" + strings.TrimPrefix(extra, "http://"+host)
	}
	fmt.Fprintf(&b, "menuentry \"Ubuntu (Gola Bootlace)\" {\n\tlinux (http,%s)%s%s %s ---\n\tinitrd (http,%s)%s%s%s\n}\n", host, strings.TrimPrefix(fsURL, "http://"+host), kernel, args, host, strings.TrimPrefix(fsURL, "http://"+host), initrd, more)
	_ = isoURL
	return b.Bytes()
}

// ubuntuName turns what shim or GRUB asks for into the path inside the ISO.
func ubuntuName(name string) string {
	n := strings.TrimLeft(strings.ReplaceAll(name, `\`, "/"), "/")
	switch strings.ToLower(path.Base(n)) {
	case "bootx64.efi", "shimx64.efi":
		return "EFI/boot/bootx64.efi"
	case "grubx64.efi":
		return "EFI/boot/grubx64.efi"
	case "mmx64.efi":
		return "EFI/boot/mmx64.efi"
	}
	return n
}

// ubuntuTFTP answers the requests of shim and GRUB from the ISO the PC chose.
func (s *Server) ubuntuTFTP(client net.IP, id, name string) (fs.File, bool) {
	list, err := s.entryInfos(s.settings())
	if err != nil {
		return nil, false
	}
	for _, e := range list {
		if e.ISO.ID != id {
			continue
		}
		base := strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
		if base == "grub.cfg" || strings.HasPrefix(base, "grub.cfg-") {
			cfg := grubConfig(s.Base+"/fs/"+id, s.Base+isoPath(e.ISO.Name), e.Boot.Kernel, e.Boot.Initrd, e.Boot.ExtraInitrd, e.Boot.Args)
			s.logf("tftp: %s gets the GRUB configuration for %s (asked for %s)", client, e.Title, name)
			return &memFile{Reader: bytes.NewReader(cfg), name: "grub.cfg", size: int64(len(cfg))}, true
		}
		if base == "grubx64.efi" || base == "grubnetx64.efi" { // the network-capable GRUB, not the media-only one of the ISO
			s.logf("tftp: %s gets Ubuntu's signed network GRUB (asked for %s)", client, name)
			g := assets.UbuntuGrub()
			return &memFile{Reader: bytes.NewReader(g), name: "grubx64.efi", size: int64(len(g))}, true
		}
		want := ubuntuName(name)
		f, err := os.Open(e.ISO.Path)
		if err != nil {
			return nil, false
		}
		img, err := disc.Open(f, e.ISO.Size)
		if err != nil {
			f.Close()
			return nil, false
		}
		file, err := img.Open(want)
		if err != nil {
			f.Close()
			s.logf("tftp: %s asks for %s: not in %s", client, name, e.Title)
			return nil, false
		}
		s.logf("tftp: %s gets %s from %s", client, name, e.Title)
		return &isoFile{ReadSeeker: file.Reader(), name: path.Base(want), size: file.Size(), closer: f}, true
	}
	return nil, false
}

var _ = isos.ISO{}
