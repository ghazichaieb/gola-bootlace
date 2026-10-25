// A live Linux that reads its ISO over the network a piece at a time instead of copying it into memory: the NBD server that shows an ISO as a disk, and the extra
// initramfs archive that attaches it (see packages nbd, initrd and family).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/debinst"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/iscsi"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/nbd"
)

// familyURLs says where the web server offers an ISO.
func (s *Server) familyURLs(iso isos.ISO) family.URLs { return s.familyURLsFor(iso, iso.ID) }

// familyURLsFor is familyURLs for what a spec names: an ISO, or a partition of a file (see source.go).
func (s *Server) familyURLsFor(iso isos.ISO, spec string) family.URLs {
	u := family.URLs{ISO: s.Base + isoPath(iso.Name), FS: s.Base + "/fs/" + spec, Win: s.Base + "/w/" + iso.ID, Base: s.Base}
	pu, err := url.Parse(s.Base)
	if err != nil {
		return u
	}
	if u.ISCSI = s.iscsiURL(iso); u.ISCSI != "" {
		u.ISCSIHost, u.ISCSIPort, u.ISCSIIQN = pu.Hostname(), s.ISCSIPort, iscsi.IQNPrefix+iso.ID
	}
	if s.NBDPort != 0 {
		u.NBD = pu.Hostname() + ":/" + spec // what casper's nfsroot= shows; the helper gets the real address from the archive
		u.NBDHost, u.NBDPort, u.NBDName = pu.Hostname(), s.NBDPort, spec
	}
	if s.NBDPort != 0 || s.ISCSIPort != 0 {
		u.Extra = s.Base + "/x/" + spec + "/extra.cpio" // the archive of a start-up disk that needs one: for the network disk, or to log in to the network CD-ROM (the Debian installer)
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
	list, _ := s.scan()
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

// extraArchive is the extra initramfs archive for one ISO or partition: whatever its family needs (package family), or the usual one for a file that is not recognised.
func (s *Server) extraArchive(spec string, set initrd.Settings) ([]byte, error) {
	src, err := s.openSource(spec)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	env := family.ExtraEnv{Settings: set, Helper: assets.NBDHelper()}
	if s.ISCSIPort != 0 {
		if pu, err := url.Parse(s.Base); err == nil {
			id, _ := splitSpec(spec)
			env.ISCSI = debinst.Target{Host: pu.Hostname(), Port: s.ISCSIPort, IQN: iscsi.IQNPrefix + id}
		}
	}
	if src.FS == nil {
		return family.Boot{}.BuildExtra(env)
	}
	c := s.context(src, spec)
	b, ok := family.DetectIn(c)
	if !ok {
		b, _ = family.DetectGeneric(c) // (what it found is remembered: this does not read the start-up disk again)
	}
	if !b.HasOwnExtra() {
		return b.BuildExtra(env)
	}
	// An archive of its own takes seconds to make (drivers are read out of the system image and unpacked), so the last few are kept.
	key := fmt.Sprintf("%s|%s|%d|%d|%s", spec, src.ISO.Path, src.Size, src.ISO.ModTime.UnixNano(), set.Host)
	s.mu.Lock()
	for _, c := range s.extras {
		if c.key == key {
			s.mu.Unlock()
			return c.data, nil
		}
	}
	s.mu.Unlock()
	data, err := b.BuildExtra(env)
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
	id, part := splitSpec(spec)
	if file != "extra.cpio" || (s.NBDPort == 0 && s.ISCSIPort == 0) {
		http.NotFound(w, r)
		return
	}
	list, err := s.scan()
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
		if iso.ID != id {
			continue
		}
		set := initrd.Settings{Host: pu.Hostname(), Port: s.NBDPort, Name: spec}
		if part != 0 {
			set.Block = 512 // a partition of a disk image (an ISO's blocks are 2048 bytes)
		}
		data, err := s.extraArchive(spec, set)
		if err != nil {
			s.logf("cannot make the start-up archive for %s: %v", iso.Title, err)
			http.Error(w, "cannot make the archive", http.StatusInternalServerError)
			return
		}
		s.logf("%s fetches the start-up archive for %s", r.RemoteAddr, iso.Title)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
}
