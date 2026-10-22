// A live Linux that reads its ISO over the network a piece at a time instead of copying it into memory: the NBD server that shows an ISO as a disk, and the extra
// initramfs archive that attaches it (see packages nbd and initrd).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/nbd"
)

// familyURLs says where the web server offers an ISO.
func (s *Server) familyURLs(iso isos.ISO) family.URLs {
	u := family.URLs{ISO: s.Base + isoPath(iso.Name), FS: s.Base + "/fs/" + iso.ID, Win: s.Base + "/w/" + iso.ID, Base: s.Base}
	if s.NBDPort != 0 {
		if pu, err := url.Parse(s.Base); err == nil {
			u.NBD = pu.Hostname() + ":/" + iso.ID // what casper's nfsroot= shows; the helper gets the real address from the archive
			u.NBDHost, u.NBDPort, u.NBDName = pu.Hostname(), s.NBDPort, iso.ID
			u.Extra = s.Base + "/x/" + iso.ID + "/extra.cpio"
		}
	}
	return u
}

// NBDExport is the NBD server's way to a file: the ISO whose id is the export's name.
func (s *Server) NBDExport(name string) (nbd.Export, bool) {
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return nbd.Export{}, false
	}
	for _, iso := range list {
		if iso.ID != name {
			continue
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			return nbd.Export{}, false
		}
		s.logf("nbd: a PC reads %s as a disk", iso.Title)
		return nbd.Export{R: f, Size: iso.Size, Done: func() { f.Close() }}, true
	}
	return nbd.Export{}, false
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

// extraInitrd is /x/<id>/extra.cpio: the archive that makes a live Linux read this ISO over the network.
func (s *Server) extraInitrd(w http.ResponseWriter, r *http.Request) {
	id, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/x/"), "/")
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
			data := initrd.Extra(initrd.Settings{Host: pu.Hostname(), Port: s.NBDPort, Name: iso.ID}, assets.NBDHelper())
			s.logf("%s fetches the network-disk archive for %s", r.RemoteAddr, iso.Title)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			_, _ = w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}
