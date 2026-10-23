// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// isoBytes is a small file that looks like an ISO to Sniff.
func isoBytes(size int, fill byte) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = fill
	}
	copy(b[0x8001:], "CD001")
	return b
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type rig struct {
	t   *testing.T
	lib *library.Library
	dir string
	idx *library.Index
	m   *Manager
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	id, err := library.Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(dir, id)
	idx := library.OpenIndex(lib)
	m := New(lib, func() *library.Index { return idx }, t.Logf)
	m.allow = func(net.IP) bool { return true } // the test server is on this computer
	m.idle = 2 * time.Second
	return &rig{t: t, lib: lib, dir: dir, idx: idx, m: m}
}

// run starts the worker and waits for the job to end.
func (r *rig) run(req Request) Job {
	r.t.Helper()
	stop := make(chan struct{})
	t := r.t
	t.Cleanup(func() { close(stop) })
	go r.m.Run(stop)
	j, c, err := r.m.Start(context.Background(), req, true)
	if err != nil {
		r.t.Fatalf("Start: %v %+v", err, c)
	}
	return r.wait(j.ID)
}

func (r *rig) wait(id string) Job {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range r.m.Jobs() {
			if j.ID == id && j.State != Queued && j.State != Running {
				return j
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatal("the job did not end")
	return Job{}
}

func has(path string) bool { _, err := os.Stat(path); return err == nil }

func TestPublicAddresses(t *testing.T) {
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "2606:4700::1111", "::ffff:8.8.8.8", "64:ff9b::808:808"} {
		if !PublicIP(net.ParseIP(s)) {
			t.Errorf("%s was refused", s)
		}
	}
	for _, s := range []string{"127.0.0.1", "10.1.1.1", "172.16.5.5", "172.31.255.255", "192.168.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "224.0.0.1", "255.255.255.255",
		"240.0.0.1", "192.0.2.5", "198.18.0.1", "::1", "::", "fe80::1", "fc00::1", "fd12::1", "ff02::1", "2001:db8::1", "::ffff:10.0.0.1", "::ffff:127.0.0.1", "64:ff9b::a00:1",
		"2002:c0a8:0101::1", "2002:7f00:1::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2"} {
		if PublicIP(net.ParseIP(s)) {
			t.Errorf("%s was accepted", s)
		}
	}
	if PublicIP(nil) {
		t.Error("no address was accepted")
	}
}

func TestParseURL(t *testing.T) {
	for _, bad := range []string{"", "ftp://example.org/a.iso", "file:///etc/passwd", "javascript:alert(1)", "https://user:pw@example.org/a.iso", "https://127.0.0.1/a.iso",
		"http://10.0.0.5/a.iso", "http://[::1]/a.iso", "http://169.254.169.254/latest/meta-data", "//example.org/a.iso", "not a url", strings.Repeat("a", 3000)} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, ok := range []string{"https://example.org/a.iso", "http://example.org:8080/x/a.iso?x=1", "https://8.8.8.8/a.iso"} {
		if _, err := ParseURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

// The real client (not the test's open one) refuses to connect to this computer, directly or by a redirect.
func TestTheClientRefusesLocalAddressesAtConnectionTime(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redir.Close()
	c := newClient(PublicIP)
	for _, u := range []string{target.URL, redir.URL} {
		resp, err := c.Get(u)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s was fetched", u)
		} else if !strings.Contains(err.Error(), "local network") {
			t.Errorf("%s: unexpected error %v", u, err)
		}
	}
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	if resp, err := newClient(PublicIP).Get("http://localhost:" + port + "/"); err == nil { // a name that points to this computer
		resp.Body.Close()
		t.Errorf("a name for this computer was fetched (%s)", host)
	}
}

func TestADownloadComesIntoTheLibraryWithItsChecksum(t *testing.T) {
	content := isoBytes(300<<10, 'x')
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.iso", time.Time{}, strings.NewReader(string(content)))
	}))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/dist/ubuntu.iso", Checksum: strings.ToUpper(sha(content))})
	if j.State != Done || j.Dest != "ubuntu.iso" || j.Part != "" {
		t.Fatalf("job: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "ubuntu.iso")); string(b) != string(content) {
		t.Fatal("the file in the library is not the file that was served")
	}
	rec, ok := r.idx.Get("ubuntu.iso")
	if !ok || rec.SHA256 != sha(content) || rec.Source != srv.URL+"/dist/ubuntu.iso" {
		t.Fatalf("index: %+v %v", rec, ok)
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*.part")); len(left) != 0 {
		t.Fatalf("a part file is left: %v", left)
	}
	// the same address again is noticed, by its link and by its content
	c := r.m.Check(context.Background(), Request{URL: srv.URL + "/dist/ubuntu.iso", Name: "again.iso"})
	if len(c.Matches) == 0 || c.Matches[0].Path != "ubuntu.iso" {
		t.Fatalf("duplicate not noticed: %+v", c)
	}
	if _, _, err := r.m.Start(context.Background(), Request{URL: srv.URL + "/dist/ubuntu.iso", Name: "again.iso"}, false); err != ErrNeedsAck {
		t.Fatalf("a repeated link without agreement: %v", err)
	}
	// a second copy under another name: the content is the same, which is said
	j2 := r.run(Request{URL: srv.URL + "/mirror/copy.iso"})
	if j2.State != Done || len(j2.Warnings) != 1 || !strings.Contains(j2.Warnings[0], "ubuntu.iso") {
		t.Fatalf("second copy: %+v", j2)
	}
}

func TestAWrongChecksumLeavesNothing(t *testing.T) {
	content := isoBytes(100<<10, 'y')
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(content) }))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/a.iso", Checksum: sha([]byte("another file"))})
	if j.State != Failed || !strings.Contains(j.Error, "checksum does not match") {
		t.Fatalf("job: %+v", j)
	}
	if has(filepath.Join(r.dir, "a.iso")) {
		t.Fatal("a file with the wrong checksum was kept")
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*.part")); len(left) != 0 {
		t.Fatalf("a part file is left: %v", left)
	}
}

func TestSHA512AndAFileOfSums(t *testing.T) {
	content := isoBytes(100<<10, 'z')
	sums := fmt.Sprintf("# the sums\n%s *other.iso\n%s *debian.iso\n", sha([]byte("x")), sha(content))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			w.Write([]byte(sums))
			return
		}
		w.Write(content)
	}))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/debian.iso", ChecksumURL: srv.URL + "/SHA256SUMS"})
	if j.State != Done || j.Expected != sha(content) {
		t.Fatalf("with a list of sums: %+v", j)
	}
	if c := r.m.Check(context.Background(), Request{URL: srv.URL + "/missing.iso", ChecksumURL: srv.URL + "/SHA256SUMS"}); len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "not in that list") {
		t.Fatalf("a file that is not in the list: %+v", c)
	}
	if _, err := normalizeSum("abc"); err == nil {
		t.Fatal("a short checksum was accepted")
	}
	if text := "SHA256 (a.iso) = " + sha([]byte("q")) + "\n"; func() bool { s, ok := parseSums(text, "a.iso"); return !ok || s != sha([]byte("q")) }() {
		t.Fatal("the BSD form of the sums was not read")
	}
}

// A web page saved as an ISO is refused as soon as its first bytes have arrived, not after the whole download.
func TestAWebPageIsRefusedEarly(t *testing.T) {
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodHead { // (a handler that goes on after its headers would hold up the next request on the same connection)
			return
		}
		page := []byte("<!DOCTYPE html><html><body>sorry, the download failed</body></html>")
		chunk := append(page, make([]byte, 100<<10)...)
		for i := 0; i < 400; i++ { // 40 MB
			if _, err := w.Write(chunk); err != nil {
				return
			}
			sent.Add(int64(len(chunk)))
			time.Sleep(10 * time.Millisecond) // (a slow server: the program has time to refuse the page and hang up)
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/a.iso"})
	if j.State != Failed || !strings.Contains(j.Error, "not an ISO") {
		t.Fatalf("job: %+v", j)
	}
	if sent.Load() > 3<<20 {
		t.Errorf("%d bytes were fetched before the page was refused", sent.Load())
	}
	if has(filepath.Join(r.dir, "a.iso")) {
		t.Fatal("kept")
	}
	c := r.m.Check(context.Background(), Request{URL: srv.URL + "/a.iso"})
	_ = c
	htmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html></html>"))
	}))
	defer htmlSrv.Close()
	if c := r.m.Check(context.Background(), Request{URL: htmlSrv.URL + "/a.iso"}); len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "web page") {
		t.Fatalf("a web page at the address: %+v", c)
	}
}

// The connection is cut half way: the rest is asked for with a range, and the result is the whole file.
func TestAnInterruptedDownloadIsResumed(t *testing.T) {
	content := isoBytes(600<<10, 'r')
	var requests, ranged atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead { // (the check before the download asks with HEAD)
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			return
		}
		n := requests.Add(1)
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("Range") != "" {
			ranged.Add(1)
			http.ServeContent(w, r, "a.iso", time.Time{}, strings.NewReader(string(content)))
			return
		}
		if n == 1 { // the first answer promises everything and stops in the middle
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			w.WriteHeader(200)
			w.Write(content[:250<<10])
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		http.ServeContent(w, r, "a.iso", time.Time{}, strings.NewReader(string(content)))
	}))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/a.iso", Checksum: sha(content)})
	if j.State != Done {
		t.Fatalf("job: %+v", j)
	}
	if ranged.Load() == 0 {
		t.Error("the rest was not asked for with a range")
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "a.iso")); sha(b) != sha(content) {
		t.Fatal("the resumed file is not the whole file")
	}
}

// A server that ignores the range sends the whole file again: the part is started over, and the result is still right.
func TestAServerThatIgnoresRangesStartsOver(t *testing.T) {
	content := isoBytes(400<<10, 'q')
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			return
		}
		if n.Add(1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			w.WriteHeader(200)
			w.Write(content[:100<<10])
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Write(content) // (no Accept-Ranges, the Range header is not looked at)
	}))
	defer srv.Close()
	r := newRig(t)
	j := r.run(Request{URL: srv.URL + "/a.iso", Checksum: sha(content)})
	if j.State != Done {
		t.Fatalf("job: %+v", j)
	}
}

func TestNameAndPlaceChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone.iso":
			http.NotFound(w, r)
		case "/private.iso":
			http.Error(w, "no", http.StatusForbidden)
		case "/download":
			w.Header().Set("Content-Disposition", `attachment; filename="Fedora-42.iso"`)
			w.Header().Set("Content-Length", "10")
			w.Write(make([]byte, 10))
		default:
			w.Write(make([]byte, 10))
		}
	}))
	defer srv.Close()
	r := newRig(t)
	ctx := context.Background()
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/gone.iso"}); len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "no such file") {
		t.Errorf("404: %+v", c)
	}
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/private.iso"}); len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "login") {
		t.Errorf("403: %+v", c)
	}
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/download"}); len(c.Problems) != 0 || c.Name != "Fedora-42.iso" || c.Size != 10 {
		t.Errorf("a name from the server: %+v", c)
	}
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/download"}); c.Dest != "Fedora-42.iso" {
		t.Errorf("dest: %+v", c)
	}
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/readme.txt"}); len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "has to end in") {
		t.Errorf("a name that is not an ISO's: %+v", c)
	}
	for _, folder := range []string{"../x", ".bootlace", "nowhere"} {
		if c := r.m.Check(ctx, Request{URL: srv.URL + "/a.iso", Folder: folder}); len(c.Problems) == 0 {
			t.Errorf("folder %q was accepted", folder)
		}
	}
	for _, name := range []string{"../a.iso", ".a.iso", "a/b.iso", "x:y.iso"} {
		if c := r.m.Check(ctx, Request{URL: srv.URL + "/a.iso", Name: name}); len(c.Problems) == 0 {
			t.Errorf("name %q was accepted", name)
		}
	}
	if err := os.WriteFile(filepath.Join(r.dir, "taken.iso"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/taken.iso"}); len(c.Matches) != 1 || c.Matches[0].Reason != "name" {
		t.Errorf("a name that is taken: %+v", c)
	}
	// a library that is not there
	os.RemoveAll(filepath.Join(r.dir, library.MetaDir))
	if c := r.m.Check(ctx, Request{URL: srv.URL + "/a.iso"}); len(c.Problems) == 0 {
		t.Error("a request was accepted without a library")
	}
}

func TestTheSpaceIsChecked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(int64(1)<<50)) // a petabyte
		w.WriteHeader(200)
	}))
	defer srv.Close()
	r := newRig(t)
	c := r.m.Check(context.Background(), Request{URL: srv.URL + "/huge.iso"})
	if len(c.Problems) == 0 || !strings.Contains(c.Problems[0], "not enough room") {
		t.Fatalf("%+v", c)
	}
}

func TestCancelRemovesWhatWasReceived(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(10<<20))
		w.WriteHeader(200)
		w.Write(isoBytes(64<<10, 'c'))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)
	r := newRig(t)
	stop := make(chan struct{})
	defer close(stop)
	go r.m.Run(stop)
	j, _, err := r.m.Start(context.Background(), Request{URL: srv.URL + "/a.iso"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ { // wait until it has started to receive
		if got := r.m.copyOf(j.ID); got != nil && got.Done > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !r.m.Cancel(j.ID) {
		t.Fatal("cancel was refused")
	}
	end := r.wait(j.ID)
	if end.State != Canceled {
		t.Fatalf("job: %+v", end)
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*.part")); len(left) != 0 {
		t.Fatalf("a part file is left: %v", left)
	}
	if has(filepath.Join(r.dir, "a.iso")) {
		t.Fatal("a canceled file is in the library")
	}
}

// A job that was running when the program stopped is found again, as interrupted, with its part file, and goes on.
func TestJobsSurviveARestart(t *testing.T) {
	content := isoBytes(300<<10, 's')
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"s"`)
		http.ServeContent(w, r, "a.iso", time.Time{}, strings.NewReader(string(content)))
	}))
	defer srv.Close()
	r := newRig(t)
	part, err := r.lib.NewPart("a.iso")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content[:100<<10])
	part.Close()
	saved := fmt.Sprintf(`[{"id":"old1","url":%q,"dest":"a.iso","state":"running","size":%d,"done":102400,"part":%q,"etag":"\"s\"","added":"2026-10-23T10:00:00Z"}]`,
		srv.URL+"/a.iso", len(content), filepath.Base(part.Name()))
	incoming, _ := r.lib.IncomingDir()
	if err := os.WriteFile(filepath.Join(incoming, "jobs.json"), []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs := r.m.Jobs()
	if len(jobs) != 1 || jobs[0].State != Interrupted {
		t.Fatalf("after a restart: %+v", jobs)
	}
	stop := make(chan struct{})
	defer close(stop)
	go r.m.Run(stop)
	if !r.m.Resume("old1") {
		t.Fatal("resume was refused")
	}
	if j := r.wait("old1"); j.State != Done {
		t.Fatalf("after resume: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "a.iso")); sha(b) != sha(content) {
		t.Fatal("the file is not whole")
	}
}

func TestUpload(t *testing.T) {
	r := newRig(t)
	content := isoBytes(500<<10, 'u')
	up := UploadRequest{Name: "mine.iso", Size: int64(len(content)), SHA256: sha(content)}
	dest, warn, err := r.m.Upload(up, strings.NewReader(string(content)))
	if err != nil || dest != "mine.iso" || len(warn) != 0 {
		t.Fatalf("%q %v %v", dest, warn, err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "mine.iso")); sha(b) != sha(content) {
		t.Fatal("the file is not whole")
	}
	if rec, ok := r.idx.Get("mine.iso"); !ok || rec.SHA256 != sha(content) {
		t.Fatalf("index: %+v %v", rec, ok)
	}
	// the same name again, and the same content under another name
	if c := r.m.CheckUpload(up); len(c.Matches) == 0 {
		t.Fatalf("a repeated upload was not noticed: %+v", c)
	}
	_, warn, err = r.m.Upload(UploadRequest{Name: "copy.iso", Size: up.Size}, strings.NewReader(string(content)))
	if err != nil || len(warn) != 1 || !strings.Contains(warn[0], "mine.iso") {
		t.Fatalf("a copy: %v %v", warn, err)
	}
}

func TestBadUploadsLeaveNothing(t *testing.T) {
	r := newRig(t)
	content := isoBytes(200<<10, 'b')
	left := func() []string {
		m, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*.part"))
		return m
	}
	cases := map[string]struct {
		req  UploadRequest
		body string
		want string
	}{
		"wrong checksum":   {UploadRequest{Name: "a.iso", Size: int64(len(content)), SHA256: sha([]byte("x"))}, string(content), "checksum does not match"},
		"cut":              {UploadRequest{Name: "a.iso", Size: int64(len(content)) + 10}, string(content), "incomplete"},
		"more than said":   {UploadRequest{Name: "a.iso", Size: 1000}, string(content), "more data"},
		"a web page":       {UploadRequest{Name: "a.iso", Size: 21 + 100<<10}, "<!DOCTYPE html><html>" + strings.Repeat(" ", 100<<10), "not an ISO"},
		"no size":          {UploadRequest{Name: "a.iso"}, string(content), "size"},
		"not an iso name":  {UploadRequest{Name: "a.txt", Size: 10}, "0123456789", "has to end in"},
		"name with a path": {UploadRequest{Name: "../a.iso", Size: 10}, "0123456789", "name"},
		"a bad folder":     {UploadRequest{Name: "a.iso", Folder: "../x", Size: 10}, "0123456789", "not a path"},
	}
	for name, c := range cases {
		if _, _, err := r.m.Upload(c.req, strings.NewReader(c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (wanted %q)", name, err, c.want)
		}
		if l := left(); len(l) != 0 {
			t.Errorf("%s left %v", name, l)
		}
		if has(filepath.Join(r.dir, "a.iso")) {
			t.Errorf("%s: a file was placed", name)
		}
	}
	// a taken name is not replaced
	os.WriteFile(filepath.Join(r.dir, "a.iso"), []byte("mine"), 0o644)
	if _, _, err := r.m.Upload(UploadRequest{Name: "a.iso", Size: int64(len(content))}, strings.NewReader(string(content))); err == nil {
		t.Error("a file was replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "a.iso")); string(b) != "mine" {
		t.Error("the file that was there changed")
	}
}

// A big upload goes through without the program holding it in memory.
func TestAnUploadIsStreamed(t *testing.T) {
	r := newRig(t)
	const size = 200 << 20
	head := isoBytes(64<<10, 'h')
	body := io.MultiReader(strings.NewReader(string(head)), io.LimitReader(zeroReader{}, size-int64(len(head))))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, _, err := r.m.Upload(UploadRequest{Name: "big.iso", Size: size}, body); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if grown := int64(after.TotalAlloc - before.TotalAlloc); grown > 64<<20 {
		t.Errorf("%d MB were allocated for a 200 MB upload", grown>>20)
	}
	if st, err := os.Stat(filepath.Join(r.dir, "big.iso")); err != nil || st.Size() != size {
		t.Fatalf("%v %v", st, err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
