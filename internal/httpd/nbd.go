// A live Linux that reads its ISO over the network a piece at a time instead of copying it into memory: the NBD server that shows an ISO as a disk, and the extra
// initramfs archive that attaches it (see packages nbd and initrd).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/iscsi"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/libreelec"
	"github.com/ghazichaieb/gola-bootlace/internal/nbd"
	"github.com/ghazichaieb/gola-bootlace/internal/pve"
	"github.com/ghazichaieb/gola-bootlace/internal/tails"
)

// familyURLs says where the web server offers an ISO.
func (s *Server) familyURLs(iso isos.ISO) family.URLs { return s.familyURLsFor(iso, iso.ID) }

// familyURLsFor is familyURLs for what a spec names: an ISO, or a partition of a disk image (see diskboot.go).
func (s *Server) familyURLsFor(iso isos.ISO, spec string) family.URLs {
	u := family.URLs{ISO: s.Base + isoPath(iso.Name), FS: s.Base + "/fs/" + spec, Win: s.Base + "/w/" + iso.ID, Base: s.Base}
	u.ISCSI = s.iscsiURL(iso)
	if u.ISCSI != "" {
		if pu, err := url.Parse(s.Base); err == nil {
			u.ISCSIHost, u.ISCSIPort, u.ISCSIIQN = pu.Hostname(), s.ISCSIPort, iscsi.IQNPrefix+iso.ID
		}
	}
	if s.NBDPort != 0 {
		if pu, err := url.Parse(s.Base); err == nil {
			u.NBD = pu.Hostname() + ":/" + spec // what casper's nfsroot= shows; the helper gets the real address from the archive
			u.NBDHost, u.NBDPort, u.NBDName = pu.Hostname(), s.NBDPort, spec
			u.Extra = s.Base + "/x/" + spec + "/extra.cpio"
		}
	}
	return u
}

// NBDExport is the NBD server's way to a file: the ISO whose id is the export's name.
func (s *Server) NBDExport(name string) (nbd.Export, bool) {
	src, err := s.openSource(name)
	if err != nil {
		return nbd.Export{}, false
	}
	if _, part := splitSpec(name); part != 0 {
		s.logf("nbd: a PC reads partition %d of %s as a disk", part, src.ISO.Title)
	} else {
		s.logf("nbd: a PC reads %s as a disk", src.ISO.Title)
	}
	return nbd.Export{R: src.R, Size: src.Size, Done: src.Close}, true
}

// NBDNames lists the ISOs.
func (s *Server) NBDNames() []string {
	list, _ := isos.Scan(s.ISODir)
	var out []string
	for _, iso := range list {
		out = append(out, iso.ID)
	}
	return out
}

// ServeNBD runs the network block disk server on a listener.
func (s *Server) ServeNBD(l net.Listener) error {
	return (&nbd.Server{Open: s.NBDExport, Names: s.NBDNames, Logf: s.logf}).Serve(l)
}

// extraArchive is the archive for one ISO or partition: the one for Proxmox VE (its installer finds its ISO in its own way, see package pve), the one for Tails (its start-up
// disk has no network card drivers, see package tails) or the usual one.
func (s *Server) extraArchive(spec string, set initrd.Settings) ([]byte, error) {
	src, err := s.openSource(spec)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if src.FS != nil && libreelec.Is(src.FS) {
		return libreelec.Extra(set, assets.NBDHelper()), nil
	}
	if src.FS == nil || !(pve.Is(src.FS) || tails.Is(src.FS)) {
		return initrd.Extra(set, assets.NBDHelper()), nil
	}
	// those two archives take seconds to make (drivers are read out of the system image and unpacked), so the last few are kept
	key := fmt.Sprintf("%s|%s|%d|%d|%s", spec, src.ISO.Path, src.Size, src.ISO.ModTime.UnixNano(), set.Host)
	s.mu.Lock()
	for _, c := range s.extras {
		if c.key == key {
			s.mu.Unlock()
			return c.data, nil
		}
	}
	s.mu.Unlock()
	var data []byte
	if pve.Is(src.FS) {
		data, err = pve.Extra(src.FS, pve.Settings{Host: set.Host, Port: set.Port, Name: set.Name}, assets.NBDHelper())
	} else {
		data, err = tails.Extra(src.FS, set, assets.NBDHelper())
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.extras = append(s.extras, extraCache{key, data})
	if len(s.extras) > 3 {
		s.extras = s.extras[1:]
	}
	s.mu.Unlock()
	return data, nil
}

type extraCache struct {
	key  string
	data []byte
}

// extraInitrd is /x/<id>/extra.cpio: the archive that makes a live Linux read this ISO over the network.
func (s *Server) extraInitrd(w http.ResponseWriter, r *http.Request) {
	spec, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/x/"), "/")
	id, _ := splitSpec(spec)
	if file != "extra.cpio" || s.NBDPort == 0 {
		http.NotFound(w, r)
		return
	}
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		http.Error(w, "cannot read the ISO folder", http.StatusInternalServerError)
		return
	}
	pu, err := url.Parse(s.Base)
	if err != nil {
		http.Error(w, "bad server address", http.StatusInternalServerError)
		return
	}
	for _, iso := range list {
		if iso.ID == id {
			set := initrd.Settings{Host: pu.Hostname(), Port: s.NBDPort, Name: spec}
			if _, part := splitSpec(spec); part != 0 {
				set.Block = 512 // a disk image's partition (an ISO's blocks are 2048 bytes)
			}
			data, err := s.extraArchive(spec, set)
			if err != nil {
				s.logf("cannot make the network-disk archive for %s: %v", iso.Title, err)
				http.Error(w, "cannot make the archive", http.StatusInternalServerError)
				return
			}
			s.logf("%s fetches the network-disk archive for %s", r.RemoteAddr, iso.Title)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			_, _ = w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}
