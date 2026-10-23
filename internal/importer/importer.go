// The import jobs: checking an address before it is fetched (name, size, what it would repeat), the download itself (resumed after an interruption, checked for what
// it is and for its checksum while it arrives), and the list of jobs the web interface shows.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

// Request is what the person fills in.
type Request struct {
	URL         string `json:"url"`
	Name        string `json:"name"`        // the file name in the library; by default the one in the address
	Folder      string `json:"folder"`      // the folder of the library it goes to ("" is the top)
	Checksum    string `json:"checksum"`    // SHA-256 or SHA-512, as hex
	ChecksumURL string `json:"checksumUrl"` // or the address of a file with the sums of several files (SHA256SUMS)
}

// Check is what is known about a request before it is started.
type Check struct {
	Name     string          `json:"name"`
	Dest     string          `json:"dest"`
	Size     int64           `json:"size"` // -1 when the server does not say
	Expected string          `json:"expected,omitempty"`
	Problems []string        `json:"problems"` // what stops it
	Matches  []library.Match `json:"matches"`  // what it would repeat
}

// States of a job.
const (
	Queued      = "queued"
	Running     = "running"
	Done        = "done"
	Failed      = "failed"
	Canceled    = "canceled"
	Interrupted = "interrupted" // the program stopped while it ran: it can be resumed
)

// Job is one download.
type Job struct {
	ID       string    `json:"id"`
	URL      string    `json:"url"`
	Dest     string    `json:"dest"`
	State    string    `json:"state"`
	Error    string    `json:"error,omitempty"`
	Size     int64     `json:"size"`
	Done     int64     `json:"done"`
	Speed    float64   `json:"speed"` // bytes a second
	Expected string    `json:"expected,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
	Added    time.Time `json:"added"`
	Finished time.Time `json:"finished,omitempty"`

	Part  string `json:"part,omitempty"` // the name of the partial file in the incoming folder
	ETag  string `json:"etag,omitempty"`
	LastM string `json:"lastModified,omitempty"`
}

// ErrNeedsAck is returned by Start when the file would repeat one that is there and the person has not said to go on.
var ErrNeedsAck = errors.New("this looks like a file that is already in the library")

// Manager runs the jobs, one at a time.
type Manager struct {
	Lib   *library.Library
	Index func() *library.Index
	Logf  func(format string, args ...any)
	// Installing says whether PCs are using the server now; Limit is the most bytes a second a download may use then (0: no limit). A download never slows
	// anything down while nothing is installing.
	Installing func() bool
	Limit      func() int64

	allow  func(net.IP) bool // which addresses may be connected to (PublicIP; the tests open it)
	client *http.Client
	idle   time.Duration // how long a download may go without a byte before it is tried again

	mu      sync.Mutex
	jobs    []*Job
	cancels map[string]context.CancelFunc
	loaded  string
	wake    chan struct{}
	seq     int
}

// New makes a manager. Run has to be started for jobs to be carried out.
func New(lib *library.Library, index func() *library.Index, logf func(string, ...any)) *Manager {
	m := &Manager{Lib: lib, Index: index, Logf: logf, allow: PublicIP, idle: 60 * time.Second, cancels: map[string]context.CancelFunc{}, wake: make(chan struct{}, 1)}
	m.client = newClient(func(ip net.IP) bool { return m.allow(ip) })
	return m
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

// ---- checking a request

var fileNameInURL = regexp.MustCompile(`(?i)\.(iso|img|raw|xz|gz|zst|bz2|zip)$`)

func (m *Manager) get(ctx context.Context, method, raw string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Gola-Bootlace/"+version.Version)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return m.client.Do(req)
}

// Check looks at a request without fetching the file.
func (m *Manager) Check(ctx context.Context, r Request) Check {
	c := Check{Size: -1, Problems: []string{}, Matches: []library.Match{}}
	problem := func(s string) Check { c.Problems = append(c.Problems, s); return c }
	if err := m.Lib.Check(); err != nil {
		return problem(err.Error())
	}
	u, err := parseURL(r.URL, m.allow)
	if err != nil {
		return problem(err.Error())
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		if n, err := url.PathUnescape(path.Base(u.Path)); err == nil && n != "." && n != "/" {
			name = n
		}
	}
	// what the server says about the file (many do not answer HEAD: that is not a reason to refuse)
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if resp, err := m.get(hctx, http.MethodHead, u.String(), nil); err != nil {
		if errors.Is(err, errLocal) || strings.Contains(err.Error(), errLocal.Error()) {
			return problem(errLocal.Error())
		}
	} else {
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			return problem("the server says there is no such file (error " + strconv.Itoa(resp.StatusCode) + ")")
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
			return problem("the server does not give this file to everyone (error " + strconv.Itoa(resp.StatusCode) + "): a file that needs a login cannot be fetched here")
		case resp.StatusCode == 200:
			if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
				return problem("this address is a web page, not the file itself")
			}
			c.Size = resp.ContentLength
			if name == "" || !library.Managed(name) {
				if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" && (r.Name == "") {
					name = path.Base(strings.ReplaceAll(params["filename"], "\\", "/"))
				}
			}
		}
	}
	if err := library.ValidName(name); err != nil {
		return problem("choose a file name: " + err.Error())
	}
	if !library.Managed(name) {
		return problem("the file name has to end in .iso, .img, .raw, .xz, .gz, .zst, .bz2 or .zip: give a name")
	}
	folder, err := library.CleanRel(r.Folder)
	if err != nil {
		return problem(err.Error())
	}
	c.Name, c.Dest = name, strings.TrimPrefix(folder+"/"+name, "/")
	if _, err := m.Lib.ListDir(folder); err != nil {
		return problem(err.Error())
	}
	if c.Size > 0 {
		if err := m.Lib.EnoughSpace(c.Size); err != nil {
			return problem(err.Error())
		}
	}
	// the checksum: given, or looked up in a file of sums
	switch {
	case strings.TrimSpace(r.Checksum) != "":
		sum, err := normalizeSum(r.Checksum)
		if err != nil {
			return problem(err.Error())
		}
		c.Expected = sum
	case strings.TrimSpace(r.ChecksumURL) != "":
		sum, err := m.sumFromFile(ctx, r.ChecksumURL, name, u.Path)
		if err != nil {
			return problem(err.Error())
		}
		c.Expected = sum
	}
	sha := ""
	if len(c.Expected) == 64 {
		sha = c.Expected
	}
	if idx := m.Index(); idx != nil {
		c.Matches = idx.Duplicates(c.Dest, u.String(), c.Size, sha)
	}
	return c
}

var hexSum = regexp.MustCompile(`^[0-9a-f]+$`)

func normalizeSum(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if (len(s) != 64 && len(s) != 128) || !hexSum.MatchString(s) {
		return "", errors.New("the checksum has to be a SHA-256 (64 characters) or SHA-512 (128 characters) in hexadecimal")
	}
	return s, nil
}

var (
	gnuSum = regexp.MustCompile(`^([0-9a-fA-F]{64}|[0-9a-fA-F]{128})\s+\*?(.+?)\s*$`)
	bsdSum = regexp.MustCompile(`^SHA(?:256|512) \((.+)\) = ([0-9a-fA-F]{64}|[0-9a-fA-F]{128})\s*$`)
)

// parseSums finds the sum of a file in the text of a SHA256SUMS file (the GNU form "hash  name" and the BSD form "SHA256 (name) = hash").
func parseSums(text, name string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := gnuSum.FindStringSubmatch(line); m != nil && path.Base(strings.TrimPrefix(m[2], "./")) == name {
			return strings.ToLower(m[1]), true
		}
		if m := bsdSum.FindStringSubmatch(line); m != nil && path.Base(m[1]) == name {
			return strings.ToLower(m[2]), true
		}
	}
	return "", false
}

func (m *Manager) sumFromFile(ctx context.Context, raw, name, urlPath string) (string, error) {
	u, err := parseURL(raw, m.allow)
	if err != nil {
		return "", fmt.Errorf("the address of the checksums: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := m.get(cctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("the checksums cannot be fetched: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("the checksums cannot be fetched (error %d)", resp.StatusCode)
	}
	text, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if sum, ok := parseSums(string(text), name); ok {
		return sum, nil
	}
	if alt := path.Base(urlPath); alt != name {
		if sum, ok := parseSums(string(text), alt); ok {
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s is not in that list of checksums", name)
}

// ---- starting and following jobs

// Start queues a download. A request that has a problem is refused with the problems in the error; one that would repeat a file is refused with ErrNeedsAck
// until the person has agreed (acknowledged), and the check is returned either way.
func (m *Manager) Start(ctx context.Context, r Request, acknowledged bool) (*Job, Check, error) {
	c := m.Check(ctx, r)
	if len(c.Problems) > 0 {
		return nil, c, errors.New(strings.Join(c.Problems, "; "))
	}
	if len(c.Matches) > 0 && !acknowledged {
		return nil, c, ErrNeedsAck
	}
	u, _ := parseURL(r.URL, m.allow)
	m.mu.Lock()
	m.load()
	m.seq++
	j := &Job{ID: fmt.Sprintf("%x%d", time.Now().UnixNano()&0xffffff, m.seq), URL: u.String(), Dest: c.Dest, State: Queued, Size: c.Size, Expected: c.Expected, Added: time.Now()}
	m.jobs = append(m.jobs, j)
	m.saveLocked()
	m.mu.Unlock()
	m.kick()
	return m.copyOf(j.ID), c, nil
}

func (m *Manager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) copyOf(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			c := *j
			return &c
		}
	}
	return nil
}

// Jobs is a copy of the list, newest last.
func (m *Manager) Jobs() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, *j)
	}
	return out
}

// Active says whether a download is queued or running.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.State == Queued || j.State == Running {
			return true
		}
	}
	return false
}

// Cancel stops a job that is queued or running and removes what it had received.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		switch j.State {
		case Running:
			if c := m.cancels[id]; c != nil {
				c()
			}
			return true
		case Queued, Interrupted, Failed:
			m.dropPart(j)
			j.State, j.Finished = Canceled, time.Now()
			m.saveLocked()
			return true
		}
	}
	return false
}

// CancelAll stops every job that is queued or running (the library is about to change).
func (m *Manager) CancelAll() {
	for _, j := range m.Jobs() {
		if j.State == Queued || j.State == Running {
			m.Cancel(j.ID)
		}
	}
}

// Resume puts a job that was interrupted, or failed, back in the queue; what was received is kept and asked for again from where it stopped.
func (m *Manager) Resume(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id && (j.State == Interrupted || j.State == Failed) {
			j.State, j.Error = Queued, ""
			m.saveLocked()
			m.kick()
			return true
		}
	}
	return false
}

// Dismiss removes a finished job from the list.
func (m *Manager) Dismiss(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.jobs {
		if j.ID == id && j.State != Running && j.State != Queued {
			m.dropPart(j)
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			m.saveLocked()
			return true
		}
	}
	return false
}

func (m *Manager) dropPart(j *Job) {
	if j.Part == "" {
		return
	}
	if dir, err := m.Lib.IncomingDir(); err == nil {
		_ = os.Remove(filepath.Join(dir, filepath.Base(j.Part)))
	}
	j.Part = ""
}

// ---- keeping the list

func (m *Manager) jobsFile() string {
	if m.Lib.Path() == "" {
		return ""
	}
	return filepath.Join(m.Lib.Path(), library.MetaDir, "incoming", "jobs.json")
}

// load reads the saved list once for each library; a job that was running when the program stopped becomes "interrupted".
func (m *Manager) load() {
	key := m.Lib.Path() + "|" + m.Lib.ID()
	if m.loaded == key {
		return
	}
	m.jobs = nil // (the jobs of another library are not this one's)
	if m.Lib.Check() != nil {
		return
	}
	m.loaded = key
	data, err := os.ReadFile(m.jobsFile())
	if err != nil {
		return
	}
	var saved []*Job
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	for _, j := range saved {
		if j.State == Running || j.State == Queued {
			j.State = Interrupted
		}
		m.jobs = append(m.jobs, j)
	}
}

func (m *Manager) saveLocked() {
	file := m.jobsFile()
	if file == "" || m.Lib.Check() != nil {
		return
	}
	if len(m.jobs) > 40 { // only the last finished ones are kept
		kept := m.jobs[:0]
		extra := len(m.jobs) - 40
		for _, j := range m.jobs {
			if extra > 0 && j.State != Running && j.State != Queued && j.State != Interrupted {
				extra--
				continue
			}
			kept = append(kept, j)
		}
		m.jobs = kept
	}
	data, err := json.Marshal(m.jobs)
	if err != nil {
		return
	}
	if _, err := m.Lib.IncomingDir(); err != nil {
		return
	}
	tmp := file + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, file)
	}
}

// ---- the worker

// Run carries out the queued jobs, one at a time, until stop is closed.
func (m *Manager) Run(stop <-chan struct{}) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		for {
			j := m.next()
			if j == nil {
				break
			}
			m.run(j, stop)
			select {
			case <-stop:
				return
			default:
			}
		}
		select {
		case <-stop:
			return
		case <-m.wake:
		case <-tick.C:
		}
	}
}

func (m *Manager) next() *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	for _, j := range m.jobs {
		if j.State == Queued {
			j.State, j.Error = Running, ""
			m.saveLocked()
			return j
		}
	}
	return nil
}

// permanent marks an error that trying again will not change.
type permanent struct{ error }

func (m *Manager) update(j *Job, f func(*Job)) {
	m.mu.Lock()
	f(j)
	m.mu.Unlock()
}

func (m *Manager) finish(j *Job, state string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.State, j.Finished, j.Speed = state, time.Now(), 0
	if err != nil {
		j.Error = err.Error()
	}
	delete(m.cancels, j.ID)
	m.saveLocked()
}

func (m *Manager) run(j *Job, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[j.ID] = cancel
	m.mu.Unlock()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := m.download(ctx, j)
	cancel()
	stopped := false
	select {
	case <-stop:
		stopped = true
	default:
	}
	switch {
	case err == nil:
		m.finish(j, Done, nil)
		m.logf("import: %s is in the library", j.Dest)
	case stopped:
		m.finish(j, Interrupted, nil) // (the program is stopping: the part is kept for the next start)
	case errors.Is(err, context.Canceled):
		m.logf("import of %s stopped: %v", j.Dest, err)
		m.mu.Lock()
		m.dropPart(j)
		m.mu.Unlock()
		m.finish(j, Canceled, nil)
	default:
		var p permanent
		m.mu.Lock()
		if errors.As(err, &p) {
			m.dropPart(j)
		}
		m.mu.Unlock()
		m.finish(j, Failed, err)
		m.logf("import of %s failed: %v", j.Dest, err)
	}
}

// download fetches the file, trying again from where it stopped when the connection fails.
func (m *Manager) download(ctx context.Context, j *Job) error {
	name := path.Base(j.Dest)
	var f *os.File
	var err error
	m.mu.Lock()
	part := j.Part
	m.mu.Unlock()
	if part != "" {
		if dir, derr := m.Lib.IncomingDir(); derr == nil {
			f, err = os.OpenFile(filepath.Join(dir, filepath.Base(part)), os.O_RDWR, 0o644)
		}
	}
	if f == nil {
		if f, err = m.Lib.NewPart(name); err != nil {
			return permanent{err}
		}
		m.update(j, func(j *Job) { j.Part, j.Done, j.ETag, j.LastM = filepath.Base(f.Name()), 0, "", "" })
	}
	defer f.Close()

	sums := newSums(j.Expected)
	var head []byte
	// what is already there (a resumed download) is read again for the checksum
	if st, _ := f.Stat(); st != nil && st.Size() > 0 {
		if _, err := io.Copy(io.MultiWriter(sums, &headWriter{buf: &head}), f); err != nil {
			return err
		}
	}
	offset, _ := f.Seek(0, io.SeekEnd)

	var lastErr error
	completed := false
	for attempt := 0; attempt < 6 && !completed; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second / 2):
			}
		}
		done, err := m.attempt(ctx, j, f, &offset, sums, &head)
		switch {
		case err == nil && done:
			completed = true
		case err == nil:
			lastErr = errors.New("the server closed the connection before the end of the file")
		default:
			var p permanent
			if errors.As(err, &p) || errors.Is(err, context.Canceled) {
				return err
			}
			lastErr = err
			m.logf("import of %s: %v (trying again)", j.Dest, err)
		}
	}
	if !completed {
		return fmt.Errorf("the download could not be completed: %w", lastErr)
	}
	if err := library.Sniff(name, head); err != nil {
		return permanent{fmt.Errorf("the file that arrived is not what its name says: %w", err)}
	}
	got := sums.expectedHex()
	if j.Expected != "" && got != j.Expected {
		return permanent{fmt.Errorf("the checksum does not match: the file has %s, expected %s (the download is damaged or it is another file)", short(got), short(j.Expected))}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := m.Lib.EnoughSpace(0); err != nil {
		return err
	}
	if err := m.Lib.Place(f.Name(), j.Dest); err != nil {
		return permanent{fmt.Errorf("the file cannot be put in the library: %w", err)}
	}
	m.update(j, func(j *Job) { j.Part = "" })
	if idx := m.Index(); idx != nil {
		sha := sums.sha256Hex()
		_ = idx.Set(j.Dest, library.Record{SHA256: sha, Source: j.URL, Added: time.Now()})
		var warn []string
		for _, d := range idx.Duplicates("", "", 0, sha) {
			if d.Path != j.Dest {
				warn = append(warn, "the content is the same as "+d.Path)
			}
		}
		if len(warn) > 0 {
			m.update(j, func(j *Job) { j.Warnings = warn })
		}
	}
	return nil
}

func short(s string) string {
	if len(s) > 16 {
		return s[:16] + "..."
	}
	return s
}

// attempt makes one request, from offset on, and copies the answer into f. done is true when the whole file has arrived.
func (m *Manager) attempt(ctx context.Context, j *Job, f *os.File, offset *int64, sums *sumSet, head *[]byte) (done bool, err error) {
	hdr := map[string]string{}
	m.mu.Lock()
	etag, lastMod := j.ETag, j.LastM
	m.mu.Unlock()
	if *offset > 0 {
		hdr["Range"] = fmt.Sprintf("bytes=%d-", *offset)
		if etag != "" {
			hdr["If-Range"] = etag
		} else if lastMod != "" {
			hdr["If-Range"] = lastMod
		}
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := time.AfterFunc(m.idle*2, cancel) // (the first answer may take a while)
	defer timer.Stop()
	resp, err := m.get(rctx, http.MethodGet, j.URL, hdr)
	if err != nil {
		if errors.Is(err, errLocal) || strings.Contains(err.Error(), errLocal.Error()) {
			return false, permanent{errLocal}
		}
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && *offset > 0:
		if j.Size > 0 && *offset == j.Size {
			return true, nil
		}
		return false, m.restart(j, f, offset, sums, head) // the file on the server changed: start again
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusOK:
		if *offset > 0 { // the server ignored the range (or the file changed): everything again
			if err := m.restart(j, f, offset, sums, head); err != nil {
				return false, err
			}
		}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return false, fmt.Errorf("the server answered with error %d", resp.StatusCode)
	default:
		return false, permanent{fmt.Errorf("the server answered with error %d: the file cannot be fetched", resp.StatusCode)}
	}
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return false, permanent{errors.New("this address is a web page, not the file itself")}
	}
	total := int64(-1)
	if resp.StatusCode == http.StatusPartialContent {
		if _, after, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
			if n, err := strconv.ParseInt(after, 10, 64); err == nil {
				total = n
			}
		}
	} else if resp.ContentLength >= 0 {
		total = resp.ContentLength
	}
	m.update(j, func(j *Job) {
		j.Size = total
		if e := resp.Header.Get("ETag"); e != "" && !strings.HasPrefix(e, "W/") {
			j.ETag = e
		}
		j.LastM = resp.Header.Get("Last-Modified")
	})
	if total > 0 {
		if err := m.Lib.EnoughSpace(total - *offset); err != nil {
			return false, permanent{err}
		}
	}
	timer.Reset(m.idle)

	buf := make([]byte, 256<<10)
	var sinceCheck int64
	last, lastBytes := time.Now(), *offset
	var next time.Time
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(m.idle)
			chunk := buf[:n]
			if _, err := f.Write(chunk); err != nil {
				return false, permanent{fmt.Errorf("cannot write to the library: %w", err)}
			}
			sums.Write(chunk)
			*offset += int64(n)
			sinceCheck += int64(n)
			if need := library.NeedBytes(j.Dest); len(*head) < need {
				(&headWriter{buf: head}).Write(chunk)
				if len(*head) >= need {
					if err := library.Sniff(j.Dest, *head); err != nil && !errors.Is(err, library.ErrTooShort) {
						return false, permanent{fmt.Errorf("the file that arrives is not what its name says: %w", err)}
					}
				}
			}
			if sinceCheck >= 256<<20 {
				sinceCheck = 0
				if err := m.Lib.EnoughSpace(0); err != nil {
					return false, permanent{err}
				}
			}
			if now := time.Now(); now.Sub(last) >= 500*time.Millisecond {
				speed := float64(*offset-lastBytes) / now.Sub(last).Seconds()
				last, lastBytes = now, *offset
				done := *offset
				m.update(j, func(j *Job) { j.Done, j.Speed = done, speed })
			}
			if m.Limit != nil && m.Installing != nil && m.Installing() {
				if limit := m.Limit(); limit > 0 { // the pace that leaves the network to the installs
					now := time.Now()
					if next.After(now) {
						select {
						case <-time.After(next.Sub(now)):
						case <-rctx.Done():
							return false, rctx.Err()
						}
					} else {
						next = now
					}
					next = next.Add(time.Duration(float64(n) / float64(limit) * float64(time.Second)))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, rerr
		}
	}
	done2 := *offset
	m.update(j, func(j *Job) { j.Done = done2 })
	if total >= 0 && *offset != total {
		return false, nil // too short: ask for the rest
	}
	return true, nil
}

// restart throws away what was received and starts the file again.
func (m *Manager) restart(j *Job, f *os.File, offset *int64, sums *sumSet, head *[]byte) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	*offset, *head = 0, nil
	*sums = *newSums(j.Expected)
	m.update(j, func(j *Job) { j.Done, j.ETag, j.LastM = 0, "", "" })
	return nil
}

// ---- checksums

// sumSet hashes what arrives: always SHA-256 (for the index), and SHA-512 too when that is what the person gave.
type sumSet struct {
	s256 hash.Hash
	s512 hash.Hash
}

func newSums(expected string) *sumSet {
	s := &sumSet{s256: sha256.New()}
	if len(expected) == 128 {
		s.s512 = sha512.New()
	}
	return s
}

func (s *sumSet) Write(p []byte) (int, error) {
	s.s256.Write(p)
	if s.s512 != nil {
		s.s512.Write(p)
	}
	return len(p), nil
}

func (s *sumSet) sha256Hex() string { return hex.EncodeToString(s.s256.Sum(nil)) }

// expectedHex is the sum of the kind that was asked for.
func (s *sumSet) expectedHex() string {
	if s.s512 != nil {
		return hex.EncodeToString(s.s512.Sum(nil))
	}
	return s.sha256Hex()
}

// headWriter keeps the first bytes that pass through it.
type headWriter struct{ buf *[]byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := library.SniffBytes - len(*h.buf); room > 0 {
		*h.buf = append(*h.buf, bytes.Clone(p[:min(len(p), room)])...)
	}
	return len(p), nil
}
