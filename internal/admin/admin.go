// Package admin is the web interface: a login, then pages for the server's state, the ISOs, the PCs and their rules, the menu settings and the log.
// The pages are one file built into the program; they talk to a small JSON API under /api/. Everything except the login and the page itself needs
// the password.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

//go:embed web/index.html web/mark.svg
var webFiles embed.FS

// Backend is what the pages ask of the running server.
type Backend interface {
	ISOInfos() ([]httpd.ISOInfo, error)
	Clients() []httpd.Client
	Rescan()
	ForgetPC(mac string) bool
	ForgetPCsNotSeenFor(d time.Duration) int
	StartedAt() time.Time
	InUse(path string) bool // a PC is using this file (or a file in this folder) right now
	CacheUsage() httpd.CacheUsage
}

// Options for New.
type Options struct {
	Store    *config.Store
	Log      *logbuf.Buffer
	Backend  Backend
	Facts    [][2]string // what the status page shows besides the numbers: name and value, in order (addresses, ports, DHCP mode ...)
	Library  *library.Library
	Services *service.Supervisor
	Access   *access.Policy
	Importer *importer.Manager
	Indexer  *library.Indexer
	ServerIP string // the address PCs are told to use
	Logf     func(format string, args ...any)
}

// Admin serves the pages and the API.
type Admin struct {
	o Options

	mu       sync.Mutex
	sessions map[string]*session
	fails    map[string]*failure
	uploads  chan struct{}
}

type failure struct {
	count int
	until time.Time
}

const (
	cookieName   = "bootlace_session"
	sessionFor   = 12 * time.Hour   // the longest a login lasts, whatever is done
	idleFor      = 30 * time.Minute // a login ends after this long without anything done in the page
	maxFailures  = 5
	lockFor      = 5 * time.Minute
	maxBodyBytes = 64 << 10
)

// New makes the interface. It does not set a password: see EnsurePassword.
func New(o Options) *Admin {
	return &Admin{o: o, sessions: map[string]*session{}, fails: map[string]*failure{}, uploads: make(chan struct{}, 2)}
}

// Mount is what to add to the web server: the pages, the API and a redirect from the address of the server itself.
func (a *Admin) Mount() map[string]http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/session", a.session)
	api.HandleFunc("POST /api/login", a.login)
	api.HandleFunc("POST /api/logout", a.authed(a.logout))
	api.HandleFunc("POST /api/password", a.authed(a.password))
	api.HandleFunc("GET /api/status", a.authed(a.status))
	api.HandleFunc("GET /api/isos", a.authed(a.isos))
	api.HandleFunc("PUT /api/isos/{id}", a.authed(a.isoUpdate))
	api.HandleFunc("POST /api/rescan", a.authed(a.rescan))
	api.HandleFunc("GET /api/settings", a.authed(a.settings))
	api.HandleFunc("PUT /api/settings", a.authed(a.settingsUpdate))
	api.HandleFunc("GET /api/pcs", a.authed(a.pcs))
	api.HandleFunc("DELETE /api/pcs/{mac}", a.authed(a.pcForget))
	api.HandleFunc("POST /api/pcs/clean", a.authed(a.pcClean))
	api.HandleFunc("POST /api/rules", a.authed(a.ruleSet))
	api.HandleFunc("DELETE /api/rules/{mac}", a.authed(a.ruleDelete))
	api.HandleFunc("GET /api/log", a.authed(a.logLines))
	api.HandleFunc("GET /api/library", a.authed(a.libraryInfo))
	api.HandleFunc("GET /api/library/browse", a.authed(a.libraryBrowse))
	api.HandleFunc("GET /api/library/inspect", a.authed(a.libraryInspect))
	api.HandleFunc("PUT /api/library", a.authed(a.librarySelect))
	api.HandleFunc("GET /api/library/files", a.authed(a.filesList))
	api.HandleFunc("POST /api/library/folder", a.authed(a.folderMake))
	api.HandleFunc("POST /api/library/rename", a.authed(a.fileRename))
	api.HandleFunc("POST /api/library/delete", a.authed(a.fileDelete))
	api.HandleFunc("POST /api/library/import/check", a.authed(a.importCheck))
	api.HandleFunc("POST /api/library/import", a.authed(a.importStart))
	api.HandleFunc("GET /api/library/jobs", a.authed(a.jobsList))
	api.HandleFunc("POST /api/library/jobs/{id}/cancel", a.authed(a.jobAction((*importer.Manager).Cancel, "running")))
	api.HandleFunc("POST /api/library/jobs/{id}/resume", a.authed(a.jobAction((*importer.Manager).Resume, "stopped")))
	api.HandleFunc("POST /api/library/jobs/{id}/dismiss", a.authed(a.jobAction((*importer.Manager).Dismiss, "finished")))
	api.HandleFunc("PUT /api/library/limit", a.authed(a.limitSet))
	api.HandleFunc("POST /api/library/upload/check", a.authed(a.uploadCheck))
	api.HandleFunc("PUT /api/library/upload", a.authed(a.upload))
	api.HandleFunc("GET /api/cache", a.authed(a.cacheGet))
	api.HandleFunc("PUT /api/cache", a.authed(a.cacheSet))
	api.HandleFunc("GET /api/access", a.authed(a.accessGet))
	api.HandleFunc("PUT /api/access", a.authed(a.accessSet))
	api.HandleFunc("GET /api/services", a.authed(a.servicesReport))
	api.HandleFunc("POST /api/services/start", a.authed(a.servicesStart))
	api.HandleFunc("POST /api/services/stop", a.authed(a.servicesStop))
	api.HandleFunc("PUT /api/services/autostart", a.authed(a.servicesAutoStart))

	pages := http.NewServeMux()
	pages.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		a.file(w, r, "web/index.html", "text/html; charset=utf-8")
	})
	pages.HandleFunc("GET /admin/mark.svg", func(w http.ResponseWriter, r *http.Request) { a.file(w, r, "web/mark.svg", "image/svg+xml") })
	return map[string]http.Handler{
		"/api/":   api,
		"/admin/": pages,
		"/{$}":    http.RedirectHandler("/admin/", http.StatusFound),
	}
}

func (a *Admin) file(w http.ResponseWriter, r *http.Request, name, kind string) {
	data, err := webFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(data)
}

// ---- JSON helpers

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "the request is not valid: "+err.Error())
		return false
	}
	return true
}

// ---- sessions

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func token() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// session is one login. It lasts while something is done with it (a page that only refreshes itself does not count, a transfer that is still going does), up
// to a longest time.
type session struct {
	created, last time.Time
	inflight      int // requests that have started and not ended
}

func (s *session) alive(now time.Time) bool {
	return now.Sub(s.created) < sessionFor && (s.inflight > 0 || now.Sub(s.last) < idleFor)
}

func (a *Admin) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok || !s.alive(time.Now()) {
		delete(a.sessions, c.Value)
		return false
	}
	return true
}

// begin starts a request on the login of the cookie: it returns false when there is none, and otherwise a function to call when the request ends. A request
// from the page's own refreshing (poll) neither keeps the login alive nor ends up counted as activity.
func (a *Admin) begin(r *http.Request) (end func(), ok bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	poll := r.Method == http.MethodGet && r.Header.Get("X-Bootlace-Poll") == "1"
	a.mu.Lock()
	defer a.mu.Unlock()
	s, found := a.sessions[c.Value]
	if !found || !s.alive(time.Now()) {
		delete(a.sessions, c.Value)
		return nil, false
	}
	if poll {
		return func() {}, true
	}
	s.last = time.Now()
	s.inflight++
	return func() {
		a.mu.Lock()
		s.inflight--
		s.last = time.Now()
		a.mu.Unlock()
	}, true
}

// authed lets a request through only with a session; a request that changes something must also carry the header that a page of another site
// cannot add without the browser asking this server's permission first.
func (a *Admin) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		end, ok := a.begin(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "log in first")
			return
		}
		defer end()
		if r.Method != http.MethodGet && r.Header.Get("X-Bootlace") != "1" {
			fail(w, http.StatusForbidden, "missing header")
			return
		}
		next(w, r)
	}
}

func (a *Admin) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": a.valid(r), "version": version.Version})
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Bootlace") != "1" {
		fail(w, http.StatusForbidden, "missing header")
		return
	}
	ip := clientIP(r)
	a.mu.Lock()
	f := a.fails[ip]
	if f != nil && f.count >= maxFailures && time.Now().Before(f.until) {
		a.mu.Unlock()
		fail(w, http.StatusTooManyRequests, "too many wrong passwords: try again in a few minutes")
		return
	}
	a.mu.Unlock()
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !CheckPassword(a.o.Store.Get().AdminHash, in.Password) {
		a.mu.Lock()
		if f == nil || time.Now().After(f.until) {
			f = &failure{}
			a.fails[ip] = f
		}
		f.count++
		f.until = time.Now().Add(lockFor)
		a.mu.Unlock()
		time.Sleep(700 * time.Millisecond) // slows a guesser down without bothering a person
		if a.o.Logf != nil {
			a.o.Logf("web interface: a wrong password from %s", ip)
		}
		fail(w, http.StatusUnauthorized, "wrong password")
		return
	}
	t := token()
	a.mu.Lock()
	delete(a.fails, ip)
	a.sessions[t] = &session{created: time.Now(), last: time.Now()}
	for k, s := range a.sessions { // forget old sessions
		if !s.alive(time.Now()) {
			delete(a.sessions, k)
		}
	}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionFor / time.Second)})
	if a.o.Logf != nil {
		a.o.Logf("web interface: %s logged in", ip)
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (a *Admin) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !CheckPassword(a.o.Store.Get().AdminHash, in.Current) {
		fail(w, http.StatusForbidden, "the current password is wrong") // not 401: that would make the page think the login ended
		return
	}
	if len(in.New) < 8 {
		fail(w, http.StatusBadRequest, "the new password needs at least 8 characters")
		return
	}
	h, err := HashPassword(in.New)
	if err == nil {
		err = a.o.Store.Update(func(c *config.Config) error { c.AdminHash = h; return nil })
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.mu.Lock() // every other session ends: the password changed for a reason
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = c.Value
	}
	for k := range a.sessions {
		if k != keep {
			delete(a.sessions, k)
		}
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- pages' data

func (a *Admin) status(w http.ResponseWriter, _ *http.Request) {
	infos, err := a.o.Backend.ISOInfos()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	hidden := 0
	var size int64
	for _, i := range infos {
		if i.Hidden {
			hidden++
		}
		size += i.Size
	}
	facts := make([]map[string]string, 0, len(a.o.Facts))
	for _, f := range a.o.Facts {
		facts = append(facts, map[string]string{"name": f[0], "value": f[1]})
	}
	started := a.o.Backend.StartedAt()
	writeJSON(w, http.StatusOK, map[string]any{
		"version": version.Version, "started": started, "uptimeSeconds": int(time.Since(started).Seconds()),
		"isoDir": a.o.Library.Path(), "isoCount": len(infos), "isoHidden": hidden, "isoBytes": size,
		"configFile": a.o.Store.Path(), "facts": facts, "pcs": len(a.o.Backend.Clients()),
	})
}

func (a *Admin) isos(w http.ResponseWriter, _ *http.Request) {
	infos, err := a.o.Backend.ISOInfos()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"isos": infos, "default": a.o.Store.Get().Default})
}

func (a *Admin) isoUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Hidden *bool   `json:"hidden"`
		Title  *string `json:"title"`
		Args   *string `json:"args"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	infos, err := a.o.Backend.ISOInfos()
	found := false
	for _, i := range infos {
		found = found || i.ID == id
	}
	if err != nil || !found {
		fail(w, http.StatusNotFound, "no such ISO")
		return
	}
	err = a.o.Store.Update(func(c *config.Config) error {
		if in.Hidden != nil {
			if *in.Hidden {
				c.Hidden[id] = true
			} else {
				delete(c.Hidden, id)
			}
		}
		if in.Title != nil {
			c.Titles[id] = *in.Title // an empty title is dropped by the check: the file name is shown again
		}
		if in.Args != nil {
			if c.Args == nil {
				c.Args = map[string]string{}
			}
			c.Args[id] = *in.Args // empty: dropped by the check
		}
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) rescan(w http.ResponseWriter, _ *http.Request) {
	a.o.Backend.Rescan()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) settings(w http.ResponseWriter, _ *http.Request) {
	c := a.o.Store.Get()
	writeJSON(w, http.StatusOK, map[string]any{"menuTimeout": c.MenuTimeout, "default": c.Default, "pcDays": c.PCDays})
}

func (a *Admin) settingsUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MenuTimeout int    `json:"menuTimeout"`
		Default     string `json:"default"`
		PCDays      *int   `json:"pcDays"` // left out by the pages that do not show it
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := a.o.Store.Update(func(c *config.Config) error {
		c.MenuTimeout, c.Default = in.MenuTimeout, in.Default
		if in.PCDays != nil {
			c.PCDays = *in.PCDays
		}
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: menu settings changed (timeout %d s, default %q)", in.MenuTimeout, in.Default)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) pcs(w http.ResponseWriter, r *http.Request) {
	c := a.o.Store.Get()
	rules := map[string]config.Rule{}
	for _, r := range c.Rules {
		rules[r.MAC] = r
	}
	type pc struct {
		httpd.Client
		Rule *config.Rule `json:"rule,omitempty"`
	}
	out := []pc{}
	seen := map[string]bool{}
	for _, cl := range a.o.Backend.Clients() {
		p := pc{Client: cl}
		if r, ok := rules[cl.MAC]; ok {
			r := r
			p.Rule = &r
		}
		seen[cl.MAC] = true
		out = append(out, p)
	}
	for _, r := range c.Rules { // a rule for a PC that has not asked since the server started
		if !seen[r.MAC] {
			r := r
			out = append(out, pc{Client: httpd.Client{MAC: r.MAC}, Rule: &r})
		}
	}
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		kept := out[:0]
		for _, p := range out {
			hay := strings.ToLower(strings.Join([]string{p.MAC, p.IP, p.Manufacturer, p.Product, p.Serial, p.LastAction, noteOf(p.Rule)}, "\n"))
			if strings.Contains(hay, q) {
				kept = append(kept, p)
			}
		}
		out = kept
	}
	total := len(out)
	limit, offset := queryInt(r, "limit", 25, 1, 500), queryInt(r, "offset", 0, 0, total)
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, map[string]any{"pcs": out[offset:end], "total": total, "offset": offset, "limit": limit})
}

func noteOf(r *config.Rule) string {
	if r == nil {
		return ""
	}
	return r.Note
}

// queryInt is a number in the web address, kept between lo and hi; def when it is missing or not a number.
func queryInt(r *http.Request, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		v = def
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

func (a *Admin) pcForget(w http.ResponseWriter, r *http.Request) {
	mac, ok := config.NormalizeMAC(r.PathValue("mac"))
	if !ok {
		mac = r.PathValue("mac") // a PC listed by its address only (an old script without a MAC)
	}
	if !a.o.Backend.ForgetPC(mac) {
		fail(w, http.StatusNotFound, "that PC is not in the list")
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: %s removed from the list of PCs", mac)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) pcClean(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days int `json:"days"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Days < 0 || in.Days > 3650 {
		fail(w, http.StatusBadRequest, "the number of days must be between 0 and 3650")
		return
	}
	n := a.o.Backend.ForgetPCsNotSeenFor(time.Duration(in.Days) * 24 * time.Hour)
	if a.o.Logf != nil {
		a.o.Logf("web interface: %d PC(s) not seen for %d day(s) removed from the list", n, in.Days)
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}

func (a *Admin) ruleSet(w http.ResponseWriter, r *http.Request) {
	var in config.Rule
	if !readJSON(w, r, &in) {
		return
	}
	mac, ok := config.NormalizeMAC(in.MAC)
	if !ok {
		fail(w, http.StatusBadRequest, "that is not a MAC address (like aa:bb:cc:dd:ee:ff)")
		return
	}
	in.MAC = mac
	err := a.o.Store.Update(func(c *config.Config) error {
		in.Added = time.Now()
		for i := range c.Rules {
			if c.Rules[i].MAC == mac {
				in.Added = c.Rules[i].Added
				c.Rules[i] = in
				return nil
			}
		}
		c.Rules = append(c.Rules, in)
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: rule for %s set to %s", mac, in.Action)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) ruleDelete(w http.ResponseWriter, r *http.Request) {
	mac, ok := config.NormalizeMAC(r.PathValue("mac"))
	if !ok {
		fail(w, http.StatusBadRequest, "that is not a MAC address")
		return
	}
	err := a.o.Store.Update(func(c *config.Config) error {
		out := c.Rules[:0]
		for _, r := range c.Rules {
			if r.MAC != mac {
				out = append(out, r)
			}
		}
		c.Rules = out
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) logLines(w http.ResponseWriter, r *http.Request) {
	after, limit := atoi(r.URL.Query().Get("after")), atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	lines, last := a.o.Log.After(after, limit)
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "last": last})
}

func atoi(s string) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' || n > 1<<30 {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ErrNoPassword is returned when the interface has no password and none was given.
var ErrNoPassword = errors.New("no admin password is set")

// equal compares two secrets without leaking where they differ.
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
