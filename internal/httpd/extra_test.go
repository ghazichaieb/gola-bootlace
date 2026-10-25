// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"net/http/httptest"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

func TestTheExtraArchiveIsOfferedWithOnlyTheIscsiServerOn(t *testing.T) {
	iso := isos.ISO{ID: "abc123", Name: "debian-netinst.iso"}
	for _, c := range []struct {
		name         string
		nbd, iscsi   int
		wantExtra    bool
		wantNBD      bool
		wantISCSIURL bool
	}{
		{"nothing", 0, 0, false, false, false},
		{"network disk only", 10809, 0, true, true, false},
		{"iSCSI only (the Debian installer's CD)", 0, 3260, true, false, true},
		{"both", 10809, 3260, true, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{Base: "http://10.0.0.1:8080", NBDPort: c.nbd, ISCSIPort: c.iscsi}
			u := s.familyURLs(iso)
			if (u.Extra != "") != c.wantExtra || (u.NBD != "") != c.wantNBD || (u.ISCSI != "") != c.wantISCSIURL {
				t.Errorf("extra %q, nbd %q, iscsi %q", u.Extra, u.NBD, u.ISCSI)
			}
			if c.wantExtra && u.Extra != "http://10.0.0.1:8080/x/abc123/extra.cpio" {
				t.Errorf("extra at %q", u.Extra)
			}
		})
	}
}

func TestTheExtraArchiveIs404WhenNeitherServerIsOn(t *testing.T) {
	ts := httptest.NewServer((&Server{Base: "http://10.0.0.1:8080", ISODir: t.TempDir(), CacheDir: t.TempDir()}).Handler())
	defer ts.Close()
	if code, _ := get(t, ts.URL+"/x/abc123/extra.cpio", nil); code != 404 {
		t.Errorf("status %d", code)
	}
}
