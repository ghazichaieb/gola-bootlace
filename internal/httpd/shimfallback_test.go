package httpd

import (
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/handoff"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// winISO is a Windows installation ISO as the library lists it.
func winISO(id, title string) entryInfo {
	return entryInfo{ISO: isos.ISO{ID: id, Name: title + ".iso", Title: title}, Title: title, Known: true, Boot: family.Boot{Family: "Windows setup"}}
}

// fallbackServer is shimServer with a library of the given ISOs and the hand-off registry the native start needs.
func fallbackServer(t *testing.T, library ...entryInfo) (*Server, *clock) {
	t.Helper()
	s, clk := shimServer(t)
	s.Handoff = &handoff.Registry{Now: clk.now}
	s.entryList = func(config.Config) ([]entryInfo, error) { return library, nil }
	return s, clk
}

// flagPC makes the PC with pcMAC a PC that took the whole shim and was flagged after the grace time.
func flagPC(t *testing.T, s *Server, clk *clock) {
	t.Helper()
	ip := net.ParseIP("10.0.0.50")
	s.NoteDHCP(pcMAC, true, false)
	s.NoteTFTP(ip, shimFile, false)
	s.NoteTFTP(ip, shimFile, true)
	clk.advance(shimGrace)
	if !refuses(s, pcMAC) {
		t.Fatal("the PC is not flagged")
	}
}

func fallbackOf(s *Server) *ShimFallback { return listed(s, pcMAC).ShimFallback }

// With exactly one Windows ISO the flagged PC's next boot is the native start of it, once, and the row says so.
func TestAFlaggedPCIsOfferedTheOnlyWindowsISOOnce(t *testing.T) {
	s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 11"))
	flagPC(t, s, clk)
	if fb := fallbackOf(s); fb == nil || fb.State != fallbackOffered || fb.ISO != "a1b2c3d4e5" || fb.Title != "Windows 11" {
		t.Fatalf("the row says %+v", fb)
	}
	file, ok := s.NativeBootFile(pcMAC, true)
	if !ok || file != "win/a1b2c3d4e5/bootmgfw.efi" {
		t.Fatalf("boot file %q %v", file, ok)
	}
	if c := listed(s, pcMAC); c.ShimRefused || c.ShimFallback != nil || c.LastAction != "native: Windows 11" {
		t.Fatalf("not used up: %+v", c)
	}
	if file, ok := s.NativeBootFile(pcMAC, true); ok {
		t.Fatalf("offered again: %q", file)
	}
	clk.advance(time.Minute)
	flagPC(t, s, clk) // refusing the shim again flags it again
	if fb := fallbackOf(s); fb == nil || fb.State != fallbackOffered {
		t.Fatalf("not offered after a new refusal: %+v", fb)
	}
}

// The boot is changed for no PC that is not flagged, whatever the library holds, nor for a BIOS boot.
func TestAPCThatIsNotFlaggedIsNeverOffered(t *testing.T) {
	s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 11"))
	s.NoteDHCP(pcMAC, true, false)
	s.NoteTFTP(net.ParseIP("10.0.0.50"), shimFile, false)
	s.NoteTFTP(net.ParseIP("10.0.0.50"), shimFile, true)
	if file, ok := s.NativeBootFile(pcMAC, true); ok { // (the shim's transfer is only just over)
		t.Fatalf("offered inside the grace time: %q", file)
	}
	if file, ok := s.NativeBootFile("d8:43:ae:d6:2f:99", true); ok {
		t.Fatalf("offered to an unknown PC: %q", file)
	}
	clk.advance(shimGrace)
	if file, ok := s.NativeBootFile(pcMAC, false); ok {
		t.Fatalf("offered to a BIOS boot: %q", file)
	}
	if fb := fallbackOf(s); fb == nil {
		t.Fatal("the row says nothing about a flagged PC")
	}
	if c := listed(s, "d8:43:ae:d6:2f:99"); c != nil {
		t.Fatalf("an unknown PC was listed: %+v", c)
	}
}

// With several Windows ISOs, or none, nothing changes for the boot and the row says why; a hidden ISO or another family does not count.
func TestSeveralOrNoWindowsISOsChangeNothingAndTheRowSaysSo(t *testing.T) {
	hidden := winISO("c3d4e5f6a1", "Hidden one")
	hidden.Hidden = true
	linux := entryInfo{ISO: isos.ISO{ID: "d4e5f6a1b2", Title: "Ubuntu"}, Title: "Ubuntu", Known: true, Boot: family.Boot{Family: "Ubuntu (casper)"}}
	pe := entryInfo{ISO: isos.ISO{ID: "p1", Title: "PE"}, Title: "PE", Known: true, Boot: family.Boot{Family: "Windows PE"}}
	for _, tc := range []struct {
		name    string
		library []entryInfo
		state   string
		count   int
	}{
		{"two", []entryInfo{winISO("a1b2c3d4e5", "Windows 10"), winISO("b2c3d4e5f6", "Windows 11")}, fallbackChoose, 2},
		{"none", []entryInfo{linux, pe, hidden}, fallbackNone, 0},
		{"one visible and one hidden", []entryInfo{winISO("a1b2c3d4e5", "Windows 11"), hidden}, fallbackOffered, 0},
	} {
		s, clk := fallbackServer(t, tc.library...)
		flagPC(t, s, clk)
		if fb := fallbackOf(s); fb == nil || fb.State != tc.state || fb.Count != tc.count {
			t.Errorf("%s: the row says %+v", tc.name, fb)
		}
		file, ok := s.NativeBootFile(pcMAC, true)
		if (tc.state == fallbackOffered) != ok {
			t.Errorf("%s: boot file %q %v", tc.name, file, ok)
		}
	}
}

// A rule of the person's is never overridden, whatever it says, and a rule for the kind of PC counts too.
func TestARuleOfThePersonDecidesAndTheOfferStaysOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*config.Config)
	}{
		{"own disk", func(c *config.Config) { c.Rules = []config.Rule{{MAC: pcMAC, Action: "local"}} }},
		{"menu", func(c *config.Config) { c.Rules = []config.Rule{{MAC: pcMAC, Action: "menu"}} }},
		{"a Linux ISO", func(c *config.Config) { c.Rules = []config.Rule{{MAC: pcMAC, Action: "iso:d4e5f6a1b2"}} }},
		{"a kind of PC", func(c *config.Config) {
			c.GroupRules = []config.GroupRule{{Kind: "platform", Match: "efi", Action: "local"}}
		}},
	} {
		s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 11"))
		if err := s.Config.Update(func(c *config.Config) error { tc.set(c); return nil }); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		s.NoteDHCP(pcMAC, true, false)
		flagPC(t, s, clk)
		if fb := fallbackOf(s); fb != nil {
			t.Errorf("%s: the row offers %+v", tc.name, fb)
		}
		if file, ok := s.NativeBootFile(pcMAC, true); ok {
			t.Errorf("%s: the rule was overridden: %q", tc.name, file)
		}
	}
}

// The person's own "native" rule still works and is used up as before, flagged or not.
func TestANativeRuleStillWinsOverTheOffer(t *testing.T) {
	s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 10"), winISO("b2c3d4e5f6", "Windows 11"))
	if err := s.Config.Update(func(c *config.Config) error {
		c.Rules = []config.Rule{{MAC: pcMAC, Action: "native:b2c3d4e5f6"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	flagPC(t, s, clk)
	if file, ok := s.NativeBootFile(pcMAC, true); !ok || file != "win/b2c3d4e5f6/bootmgfw.efi" {
		t.Fatalf("boot file %q %v", file, ok)
	}
	if len(s.Config.Get().Rules) != 0 {
		t.Fatalf("the rule was not used up: %v", s.Config.Get().Rules)
	}
}

// Turning the offer off for a PC keeps it out, and turning it on again brings it back.
func TestTheOfferCanBeTurnedOffForAPC(t *testing.T) {
	s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 11"))
	flagPC(t, s, clk)
	set := func(noFallback bool) {
		if err := s.Config.Update(func(c *config.Config) error {
			c.PCOptions = map[string]config.PCOptions{pcMAC: {NoShimFallback: noFallback}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(true)
	if fb := fallbackOf(s); fb == nil || fb.State != fallbackOff {
		t.Fatalf("the row says %+v", fb)
	}
	if file, ok := s.NativeBootFile(pcMAC, true); ok {
		t.Fatalf("offered although it is off: %q", file)
	}
	set(false)
	if file, ok := s.NativeBootFile(pcMAC, true); !ok || file != "win/a1b2c3d4e5/bootmgfw.efi" {
		t.Fatalf("not offered after turning it on: %q %v", file, ok)
	}
}

// The offer stops when the PC starts iPXE: it asks for the second stage, asks DHCP as iPXE, or loads the menu.
func TestTheOfferStopsWhenThePCStartsIPXE(t *testing.T) {
	for name, started := range map[string]func(s *Server){
		"second stage": func(s *Server) { s.NoteTFTP(net.ParseIP("10.0.0.50"), secondFile, false) },
		"iPXE's DHCP":  func(s *Server) { s.NoteDHCP(pcMAC, true, true) },
		"menu": func(s *Server) {
			req := httptest.NewRequest("GET", "/menu?mac="+pcMAC+"&ip=10.0.0.50", nil)
			req.RemoteAddr = "10.0.0.50:5555"
			s.noteClient(req)
		},
	} {
		s, clk := fallbackServer(t, winISO("a1b2c3d4e5", "Windows 11"))
		flagPC(t, s, clk)
		started(s)
		if file, ok := s.NativeBootFile(pcMAC, true); ok {
			t.Errorf("%s: still offered: %q", name, file)
		}
		if c := listed(s, pcMAC); c.ShimRefused || c.ShimFallback != nil {
			t.Errorf("%s: the row still says %+v", name, c.ShimFallback)
		}
	}
}
