package selftest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/smbshare"
	tftpd "github.com/ghazichaieb/gola-bootlace/internal/tftp"
)

func port(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

func byID(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

func TestEveryStepPassesAgainstRealServices(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/boot.ipxe" || r.URL.Path == "/menu":
			w.Write([]byte("#!ipxe\necho hi\n"))
		case r.URL.Path == "/iso/a.iso":
			http.ServeContent(w, r, "a.iso", time.Time{}, strings.NewReader(strings.Repeat("x", 100)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer web.Close()

	nbdL, _ := net.Listen("tcp", "127.0.0.1:0")
	defer nbdL.Close()
	go func() {
		for {
			c, err := nbdL.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("NBDMAGICIHAVEOPT"))
			c.Close()
		}
	}()
	iscsiL, _ := net.Listen("tcp", "127.0.0.1:0")
	defer iscsiL.Close()
	go func() {
		for {
			c, err := iscsiL.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	smbL, _ := net.Listen("tcp", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go smbshare.Serve(ctx, smbshare.Options{Listener: smbL, ISODir: t.TempDir(), User: "u", Password: "p"})

	tftpC, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer tftpC.Close()
	go (&tftpd.Server{FS: fstest.MapFS{"snponly.efi": {Data: []byte("efi")}}}).Serve(tftpC)

	rs := Run(ctx, Target{IP: "127.0.0.1", HTTP: port(t, web.Listener.Addr().String()), SMB: port(t, smbL.Addr().String()), NBD: port(t, nbdL.Addr().String()),
		ISCSI: port(t, iscsiL.Addr().String()), TFTP: port(t, tftpC.LocalAddr().String()), ISOPath: "/iso/a.iso"})
	if len(rs) != 6 {
		t.Fatalf("%d results", len(rs))
	}
	for _, r := range rs {
		if r.Status != "ok" {
			t.Errorf("%s: %s: %s (%s)", r.ID, r.Status, r.Detail, r.Hint)
		}
	}
}

func TestFailuresSayWhatToLookAt(t *testing.T) {
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := port(t, closed.Addr().String())
	closed.Close() // (nothing listens there now)
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) }))
	defer forbidden.Close()
	rs := byID(Run(context.Background(), Target{IP: "127.0.0.1", HTTP: port(t, forbidden.Listener.Addr().String()), SMB: closedPort, NBD: closedPort, ISCSI: 0, ISOPath: ""}))
	if r := rs["menu"]; r.Status != "fail" || !strings.Contains(r.Detail, "403") || !strings.Contains(r.Hint, "allow") && !strings.Contains(r.Hint, "Allow") {
		t.Errorf("menu: %+v", r)
	}
	for _, id := range []string{"smb", "nbd"} {
		if r := rs[id]; r.Status != "fail" || r.Hint == "" {
			t.Errorf("%s: %+v", id, r)
		}
	}
	if rs["iscsi"].Status != "skipped" || rs["iso"].Status != "skipped" {
		t.Errorf("a service that is off and a library without ISOs are skipped: %+v %+v", rs["iscsi"], rs["iso"])
	}
}
