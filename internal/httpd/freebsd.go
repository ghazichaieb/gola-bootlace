// FreeBSD's loader reads the kernel and everything else by TFTP. What it asks for is answered from the ISO the PC chose (the one whose loader it
// fetched over HTTP), with two changes: loader.conf gets the lines that make the ISO itself the root disk, and /boot.iso is the whole ISO.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/handoff"
)

// loaderConf is the ISO's own loader.conf with the lines added that make FreeBSD load the ISO into memory as a disk and start from it.
func loaderConf(own []byte) []byte {
	var b bytes.Buffer
	b.Write(own)
	if len(own) > 0 && !bytes.HasSuffix(own, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString("\n# added by Gola Bootlace: the whole ISO is loaded as a memory disk and is the root file system\n")
	b.WriteString("mdroot_load=\"YES\"\nmdroot_type=\"md_image\"\nmdroot_name=\"/boot.iso\"\n")
	b.WriteString("vfs.root.mountfrom=\"cd9660:/dev/md0\"\nvfs.root.mountfrom.options=\"ro\"\n")
	// The kernel's call into the PC's firmware for the clock (efirtc) made the kernel of a real PC (AMI firmware, Core i9-7920X) stop in its debugger at an address
	// inside the firmware: FreeBSD has a switch to leave the firmware's run-time services alone. Verbose start-up shows where it stops if it stops elsewhere.
	b.WriteString("efi.rt.disabled=\"1\"\nboot_verbose=\"YES\"\n")
	return b.Bytes()
}

// next is what iPXE calls for an ISO that needs FreeBSD's loader started by the PC's own firmware: it says which PC (MAC and address) chose which ISO,
// then restarts the PC. The next network boot of that PC is then answered with the loader as boot file.
func (s *Server) next(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mac, ip, id := q.Get("mac"), q.Get("ip"), q.Get("iso")
	kind := handoff.FreeBSD
	switch q.Get("kind") {
	case handoff.Windows:
		kind = handoff.Windows
	case handoff.Ubuntu:
		kind = handoff.Ubuntu
	}
	if s.Handoff == nil || mac == "" || ip == "" || id == "" || net.ParseIP(ip) == nil {
		http.Error(w, "mac, ip and iso are needed", http.StatusBadRequest)
		return
	}
	list, err := s.scan()
	if err != nil {
		http.Error(w, "cannot read the ISO folder", http.StatusInternalServerError)
		return
	}
	for _, iso := range list {
		if iso.ID == id {
			s.Handoff.SetKind(mac, ip, id, kind)
			s.logf("%s (%s) will start %s with its firmware (%s) at its next network boot", mac, ip, iso.Title, kind)
			fmt.Fprintln(w, "ok")
			return
		}
	}
	http.NotFound(w, r)
}

// isLoaderName says whether a path inside an ISO is one of FreeBSD's boot loaders.
func isLoaderName(name string) bool { return name == "boot/loader.efi" || name == "boot/pxeboot" }

// TFTPWindow says how many blocks to send ahead to a client: more than one only for FreeBSD's loader once it is running (it has fetched its own loader file
// already, by the firmware's TFTP client, which does not take blocks ahead).
func (s *Server) TFTPWindow(client net.IP, name string) int {
	ahead := s.TFTPAhead
	if w := s.settings().Tuning.TFTPWindow; w > 0 { // (the setting made in the web interface wins over the command line)
		ahead = w
	}
	if s.Handoff == nil || ahead <= 1 || isLoaderName(name) || s.Handoff.Kind(client.String()) != handoff.FreeBSD {
		return 1
	}
	if _, ok := s.Handoff.ISO(client.String()); !ok {
		return 1
	}
	return ahead
}

// TFTPMaxBlock is the largest TFTP block size to agree to: the setting made in the web interface, 0 when each PC may ask for what it likes.
func (s *Server) TFTPMaxBlock() int { return s.settings().Tuning.TFTPBlockSize }

// TFTPLookup answers a TFTP request from the ISO the client chose; it has no answer for clients that did not choose a FreeBSD ISO.
func (s *Server) TFTPLookup(client net.IP, name string) (fs.File, bool) {
	if s.Handoff == nil {
		return nil, false
	}
	if rest, ok := strings.CutPrefix(name, "ubuntu/"); ok { // the boot file named by DHCP: ubuntu/<id>/bootx64.efi (shim); GRUB's requests come with the same prefix or none
		if id, file, ok := strings.Cut(rest, "/"); ok {
			s.Handoff.Bind(client.String(), id, handoff.Ubuntu)
			return s.ubuntuTFTP(client, id, file)
		}
	}
	if rest, ok := strings.CutPrefix(name, "win/"); ok { // the boot file named by DHCP: win/<id>/bootmgfw.efi
		if id, file, ok := strings.Cut(rest, "/"); ok {
			s.Handoff.Bind(client.String(), id, handoff.Windows)
			return s.windowsTFTP(client, id, file)
		}
	}
	id, ok := s.Handoff.ISO(client.String())
	if !ok {
		return nil, false
	}
	switch s.Handoff.Kind(client.String()) {
	case handoff.Windows:
		return s.windowsTFTP(client, id, name)
	case handoff.Ubuntu:
		return s.ubuntuTFTP(client, id, name)
	}
	if isLoaderName(name) {
		s.Handoff.Fetched(client.String())
	}
	list, err := s.scan()
	if err != nil {
		return nil, false
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			return nil, false
		}
		if name == "boot.iso" {
			s.logf("tftp: %s gets the whole ISO %s", client, iso.Title)
			return f, true // the *os.File is the answer and closes itself
		}
		img, err := disc.Open(f, iso.Size)
		if err != nil {
			f.Close()
			return nil, false
		}
		if name == "boot/loader.conf" {
			var own []byte
			if file, err := img.Open(name); err == nil {
				own, _ = io.ReadAll(file.Reader())
			}
			f.Close()
			return &memFile{Reader: bytes.NewReader(loaderConf(own)), name: "loader.conf", size: int64(len(loaderConf(own)))}, true
		}
		file, err := img.Open(name)
		if err != nil {
			f.Close()
			return nil, false
		}
		return &isoFile{ReadSeeker: file.Reader(), name: path.Base(name), size: file.Size(), closer: f}, true
	}
	return nil, false
}

type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (m *memFile) Stat() (fs.FileInfo, error) { return fileInfo{m.name, m.size}, nil }
func (m *memFile) Close() error               { return nil }

type isoFile struct {
	io.ReadSeeker
	name   string
	size   int64
	closer io.Closer
}

func (i *isoFile) Stat() (fs.FileInfo, error) { return fileInfo{i.name, i.size}, nil }
func (i *isoFile) Close() error               { return i.closer.Close() }

type fileInfo struct {
	name string
	size int64
}

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return 0o444 }
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Sys() any           { return nil }
