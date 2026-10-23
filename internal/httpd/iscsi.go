// An ISO that Bootlace does not know how to start piece by piece is handed to the PC as a CD-ROM over iSCSI (package iscsi): iPXE reads it a block at a time, so the
// whole file is not copied into the PC's memory first.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"net"
	"net/url"
	"os"
	"strconv"

	"github.com/ghazichaieb/gola-bootlace/internal/iscsi"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// iscsiURL is the address iPXE's sanboot takes for an ISO, or empty when the iSCSI server is off.
func (s *Server) iscsiURL(iso isos.ISO) string {
	if s.ISCSIPort == 0 {
		return ""
	}
	u, err := url.Parse(s.Base)
	if err != nil {
		return ""
	}
	return "iscsi:" + u.Hostname() + "::" + itoa(s.ISCSIPort) + ":0:" + iscsi.IQNPrefix + iso.ID
}

func itoa(n int) string { return strconv.Itoa(n) }

// ISCSIDisc is the target's way to a file: the ISO whose id is the end of the target name.
func (s *Server) ISCSIDisc(id string) (iscsi.Disc, bool) {
	list, err := s.scan()
	if err != nil {
		return iscsi.Disc{}, false
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		if iso.Packed != "" {
			return iscsi.Disc{}, false // not unpacked yet
		}
		path, size, disk := iso.Path, iso.Size, iso.Disk
		if disk {
			st := s.diskState(iso)
			if !st.Ready {
				return iscsi.Disc{}, false
			}
			path, size = st.Path, st.Size
		}
		f, err := os.Open(path)
		if err != nil {
			return iscsi.Disc{}, false
		}
		if disk {
			s.logf("iscsi: a PC reads %s as a hard disk", iso.Title)
		} else {
			s.logf("iscsi: a PC reads %s as a CD-ROM", iso.Title)
		}
		release := s.hold(iso.Name)
		return iscsi.Disc{R: f, Size: size, Done: func() { f.Close(); release() }, Disk: disk}, true
	}
	return iscsi.Disc{}, false
}

// ISCSIIDs lists the ISOs' ids.
func (s *Server) ISCSIIDs() []string {
	list, _ := s.scan()
	var out []string
	for _, iso := range list {
		out = append(out, iso.ID)
	}
	return out
}

// ServeISCSI runs the iSCSI target on a listener.
func (s *Server) ServeISCSI(l net.Listener) error {
	u, _ := url.Parse(s.Base)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	return (&iscsi.Target{Open: s.ISCSIDisc, IDs: s.ISCSIIDs, Address: host + ":" + itoa(s.ISCSIPort) + ",1", Logf: s.logf}).Serve(l)
}
