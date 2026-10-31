package httpd

import (
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// clock is a settable time for the tests of the detection: nothing sleeps.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

const (
	shimFile   = "sb/ipxe-shimx64.efi"
	secondFile = "sb/ipxe.efi"
	pcMAC      = "d8:43:ae:d6:2f:05"
)

// shimServer is a server whose Secure Boot route is on (an own folder), with a clock the test sets.
func shimServer(t *testing.T) (*Server, *clock) {
	t.Helper()
	s := dhcpListServer(t)
	s.SecureBoot = SecureBoot{Dir: t.TempDir(), Shim: "ipxe-shimx64.efi", All: true}
	c := &clock{t: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)}
	s.Now = c.now
	return s, c
}

func refuses(s *Server, mac string) bool {
	c := listed(s, mac)
	return c != nil && c.ShimRefused
}

// A PC that fetched the whole shim and asked for nothing more is flagged once the grace time has passed, and not a moment before.
func TestAPCThatTookTheWholeShimAndNeverAskedForIPXEIsFlaggedAfterTheGraceTime(t *testing.T) {
	s, clk := shimServer(t)
	ip := net.ParseIP("10.0.0.50")
	s.NoteDHCP(pcMAC, true, false)
	clk.advance(2 * time.Second)
	s.NoteTFTP(ip, shimFile, false)
	clk.advance(3 * time.Second)
	s.NoteTFTP(ip, shimFile, true)
	clk.advance(shimGrace - time.Second)
	if refuses(s, pcMAC) {
		t.Fatal("flagged before the grace time")
	}
	clk.advance(time.Second)
	if !refuses(s, pcMAC) {
		t.Fatal("not flagged after the grace time")
	}
	if c := listed(s, pcMAC); c.IP != "10.0.0.50" {
		t.Fatalf("the PC's address was not learnt from its TFTP request: %q", c.IP)
	}
}

// A transfer that has not ended (or was abandoned) is never a refusal, however long ago it started.
func TestAShimTransferThatIsStillRunningIsNotFlagged(t *testing.T) {
	s, clk := shimServer(t)
	s.NoteDHCP(pcMAC, true, false)
	s.NoteTFTP(net.ParseIP("10.0.0.50"), shimFile, false)
	clk.advance(time.Hour)
	if refuses(s, pcMAC) {
		t.Fatal("flagged while the shim was still being sent")
	}
}

// A PC that asks for ipxe.efi after the shim is working, whenever it asks and whether the name carries the folder or not.
func TestAPCThatAsksForIPXEAfterTheShimIsNeverFlagged(t *testing.T) {
	for _, second := range []string{secondFile, "ipxe.efi", "SB/IPXE.EFI"} {
		s, clk := shimServer(t)
		ip := net.ParseIP("10.0.0.50")
		s.NoteDHCP(pcMAC, true, false)
		s.NoteTFTP(ip, shimFile, false)
		s.NoteTFTP(ip, shimFile, true)
		clk.advance(shimGrace - time.Second)
		s.NoteTFTP(ip, second, false)
		clk.advance(time.Hour)
		if refuses(s, pcMAC) {
			t.Fatalf("flagged although it asked for %s", second)
		}
	}
}

// The flag goes away when the PC later reaches the menu, and when it starts a new attempt.
func TestTheFlagIsClearedWhenThePCReachesTheMenuOrTriesAgain(t *testing.T) {
	s, clk := shimServer(t)
	ip := net.ParseIP("10.0.0.50")
	s.NoteDHCP(pcMAC, true, false)
	s.NoteTFTP(ip, shimFile, false)
	s.NoteTFTP(ip, shimFile, true)
	clk.advance(time.Minute)
	if !refuses(s, pcMAC) {
		t.Fatal("not flagged")
	}
	s.NoteTFTP(ip, shimFile, false) // it tries again
	if refuses(s, pcMAC) {
		t.Fatal("still flagged while it fetches the shim again")
	}
	s.NoteTFTP(ip, shimFile, true)
	clk.advance(time.Minute)
	if !refuses(s, pcMAC) {
		t.Fatal("not flagged the second time")
	}
	req := httptest.NewRequest("GET", "/menu?mac="+pcMAC+"&ip=10.0.0.50", nil)
	req.RemoteAddr = "10.0.0.50:5555"
	s.noteClient(req)
	if refuses(s, pcMAC) {
		t.Fatal("still flagged after the menu")
	}
}

// Nobody is blamed when the TFTP client cannot be told from another PC, when the route is off, or for other files.
func TestNoPCIsBlamedWhenItCannotBeTold(t *testing.T) {
	s, clk := shimServer(t)
	s.NoteDHCP(pcMAC, true, false)
	s.NoteDHCP("d8:43:ae:d6:2f:06", true, false)
	s.NoteTFTP(net.ParseIP("10.0.0.50"), shimFile, true) // two PCs without an address asked a moment ago
	clk.advance(time.Hour)
	if refuses(s, pcMAC) || refuses(s, "d8:43:ae:d6:2f:06") {
		t.Fatal("a PC was blamed for another's transfer")
	}

	off := dhcpListServer(t)
	off.NoteDHCP(pcMAC, true, false)
	off.NoteTFTP(net.ParseIP("10.0.0.50"), shimFile, true)
	off.NoteTFTP(net.ParseIP("10.0.0.50"), "undionly.kpxe", true)
	if c := listed(off, pcMAC); c.ShimRefused || c.shimDone != (time.Time{}) {
		t.Fatalf("recorded with the route off: %+v", c)
	}
}
