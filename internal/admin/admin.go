// Package admin is the web interface: a login, then pages for the server's state, the ISOs, the PCs and their rules, the menu settings and the log.
// The pages are one file built into the program; they talk to a small JSON API under /api/. Everything except the login and the page itself needs
// the password.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/backup"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
	"github.com/ghazichaieb/gola-bootlace/internal/pxecheck"
	"github.com/ghazichaieb/gola-bootlace/internal/selftest"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
	"github.com/ghazichaieb/gola-bootlace/internal/speedtest"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

//go:embed web/index.html web/mark.svg
var webFiles embed.FS

// Backend is what the pages ask of the running server.
type Backend interface {
	ISOInfos() ([]httpd.ISOInfo, error)
	Clients() []httpd.Client
	Rescan()
	ReplaceClients(list []httpd.Client) // the list of PCs from a backup
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
	// SelfTest says where the services are for the self-test (see package selftest); without it the page has no self-test.
	SelfTest func() selftest.Target
	// PXE looks for other servers that answer network boots (see package pxecheck); without it the page has no such check.
	PXE *pxecheck.Checker
	// Sync keeps a copy of the settings in the library (see package backup); without it that part of the backup card is not offered.
	Sync *backup.Sync
	// Version is the program's version, written into the backups.
	Version string
	// Wake sends a Wake-on-LAN packet for a PC (a MAC address); without it the PCs page has no Wake button.
	Wake func(mac net.HardwareAddr) error
	Logf func(format string, args ...any)
}

// Admin serves the pages and the API.
type Admin struct {
	o Options

	mu       sync.Mutex
	sessions map[string]*session
	fails    map[string]*failure
	uploads  chan struct{}
	speed    sync.Mutex // one speed test at a time
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
	api.HandleFunc("PUT /api/pcs/{mac}/name", a.authed(a.pcRename))
	api.HandleFunc("POST /api/pcs/{mac}/wake", a.authed(a.pcWake))
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
	api.HandleFunc("GET /api/log/download", a.authed(a.logDownload))
	api.HandleFunc("POST /api/selftest", a.authed(a.selfTest))
	api.HandleFunc("POST /api/speedtest", a.authed(a.speedTest))
	api.HandleFunc("GET /api/backup/download", a.authed(a.backupDownload))
	api.HandleFunc("POST /api/backup/inspect", a.authed(a.backupInspect))
	api.HandleFunc("POST /api/backup/restore", a.authed(a.backupRestore))
	api.HandleFunc("GET /api/backup/library", a.authed(a.backupLibrary))
	api.HandleFunc("POST /api/backup/library", a.authed(a.backupLibraryAct))
	api.HandleFunc("GET /api/pxecheck", a.authed(a.pxeLast))
	api.HandleFunc("POST /api/pxecheck", a.authed(a.pxeRun))
	api.HandleFunc("GET /api/tuning", a.authed(a.tuningGet))
	api.HandleFunc("PUT /api/tuning", a.authed(a.tuningSet))
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

// pxeLast is the result of the last look for other PXE or DHCP servers (the one made at start, or the last button press).
func (a *Admin) pxeLast(w http.ResponseWriter, _ *http.Request) {
	if a.o.PXE == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "result": a.o.PXE.Last()})
}

// pxeRun looks now. It sends a broadcast question on the server's network and listens for a few seconds.
func (a *Admin) pxeRun(w http.ResponseWriter, r *http.Request) {
	if a.o.PXE == nil {
		fail(w, http.StatusNotFound, "the check is not available")
		return
	}
	res, err := a.o.PXE.Run(r.Context())
	if errors.Is(err, pxecheck.ErrBusy) {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: looked for other PXE or DHCP servers: %d answer(s), %d competing", len(res.Answers), res.Conflicts())
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "result": res})
}

// ---- backup of the settings

const maxBackupBytes = 16 << 20

func (a *Admin) backupDownload(w http.ResponseWriter, r *http.Request) {
	secrets := r.URL.Query().Get("secrets") == "1"
	pcs, _ := json.Marshal(a.o.Backend.Clients())
	var buf bytes.Buffer
	if err := backup.WriteZip(&buf, a.o.Store.Get(), pcs, secrets, a.o.Version, time.Now()); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := "bootlace-settings-" + time.Now().Format("20060102-150405")
	if secrets {
		name += "-with-secrets"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
	if a.o.Logf != nil {
		a.o.Logf("web interface: a backup of the settings was downloaded (secrets included: %v)", secrets)
	}
}

// readBackup reads the zip file sent as the body of a request.
func (a *Admin) readBackup(w http.ResponseWriter, r *http.Request) (*backup.Contents, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "the file is too big for a backup")
		return nil, false
	}
	c, err := backup.ReadZip(data)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return c, true
}

func pcCount(raw json.RawMessage) int {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return 0
	}
	return len(list)
}

// backupInspect says what restoring a backup file would replace, without doing anything.
func (a *Admin) backupInspect(w http.ResponseWriter, r *http.Request) {
	c, ok := a.readBackup(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": c.Manifest, "incoming": backup.CountsOf(c.Config, pcCount(c.PCs)),
		"current": backup.CountsOf(a.o.Store.Get(), len(a.o.Backend.Clients()))})
}

// backupRestore replaces the settings (and the list of PCs) with those of a backup file. Nothing is changed when the file is not valid.
func (a *Admin) backupRestore(w http.ResponseWriter, r *http.Request) {
	c, ok := a.readBackup(w, r)
	if !ok {
		return
	}
	var pcs []httpd.Client
	if len(c.PCs) > 0 {
		if err := json.Unmarshal(c.PCs, &pcs); err != nil {
			fail(w, http.StatusBadRequest, "the list of PCs in the backup cannot be read")
			return
		}
	}
	err := a.o.Store.Update(func(cur *config.Config) error {
		*cur = backup.Merge(*cur, c)
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(c.PCs) > 0 {
		a.o.Backend.ReplaceClients(pcs)
	}
	a.o.Backend.Rescan()
	if a.o.Logf != nil {
		a.o.Logf("web interface: the settings were restored from a backup made by version %s on %s", c.Manifest.Version, c.Manifest.Created.Local().Format("2006-01-02 15:04"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restart": c.Manifest.Secrets})
}

func (a *Admin) backupLibrary(w http.ResponseWriter, _ *http.Request) {
	if a.o.Sync == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, a.o.Sync.State(len(a.o.Backend.Clients())))
}

func (a *Admin) backupLibraryAct(w http.ResponseWriter, r *http.Request) {
	if a.o.Sync == nil {
		fail(w, http.StatusNotFound, "not available")
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var err error
	switch in.Action {
	case "restore":
		if err = a.o.Sync.Restore(); err == nil {
			a.o.Backend.Rescan()
		}
	case "keep":
		err = a.o.Sync.Keep()
	default:
		fail(w, http.StatusBadRequest, "the action must be restore or keep")
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: the settings copy in the library: %s", in.Action)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// speedTest writes and reads a test file in the library and in the container's own cache folder and reports the speeds. It is not run while PCs are using the server
// unless the person says so: it uses the disk and the network share they are reading from.
func (a *Admin) speedTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if pcs := a.o.Services.Sessions(); len(pcs) > 0 && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("%d PC(s) are using the server right now, and the test would slow them down", len(pcs)), "busy": true, "pcs": len(pcs)})
		return
	}
	if !a.speed.TryLock() {
		fail(w, http.StatusConflict, "a speed test is already running")
		return
	}
	defer a.speed.Unlock()
	type where struct {
		Name string `json:"name"`
		speedtest.Result
		Summary string `json:"summary"`
	}
	var out []where
	run := func(name, dir string) {
		res, err := speedtest.Folder(r.Context(), dir)
		if err != nil {
			res.Error = err.Error()
		}
		out = append(out, where{Name: name, Result: res, Summary: res.Summary()})
	}
	if err := a.o.Library.Check(); err != nil {
		out = append(out, where{Name: "The library", Result: speedtest.Result{Error: err.Error()}, Summary: err.Error()})
	} else {
		run("The library", a.o.Library.Path())
	}
	cacheDir := a.o.Backend.CacheUsage().LocalDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		out = append(out, where{Name: "The container's own cache folder", Result: speedtest.Result{Dir: cacheDir, Error: err.Error()}, Summary: err.Error()})
	} else {
		run("The container's own cache folder", cacheDir)
	}
	if a.o.Logf != nil {
		for _, x := range out {
			a.o.Logf("web interface: speed test of %s (%s): %s", x.Name, x.Dir, x.Summary)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "installMBps": speedtest.InstallMBps})
}

// selfTest checks the services from the server's own address, the way a PC uses them (see package selftest).
func (a *Admin) selfTest(w http.ResponseWriter, r *http.Request) {
	if a.o.SelfTest == nil {
		fail(w, http.StatusNotFound, "the self-test is not available")
		return
	}
	results := selftest.Run(r.Context(), a.o.SelfTest())
	failed := 0
	for _, res := range results {
		if res.Status == "fail" {
			failed++
		}
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: self-test run: %d step(s) failed", failed)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "failed": failed})
}

// tuningGet and tuningSet are the TFTP knobs and the patterns that keep ISOs out of the menu.
func (a *Admin) tuningGet(w http.ResponseWriter, _ *http.Request) {
	c := a.o.Store.Get()
	writeJSON(w, http.StatusOK, map[string]any{"tftpBlockSize": c.Tuning.TFTPBlockSize, "tftpWindow": c.Tuning.TFTPWindow, "hidePatterns": c.HidePatterns, "menuAccent": c.MenuAccent, "accents": config.AccentNames})
}

func (a *Admin) tuningSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TFTPBlockSize int      `json:"tftpBlockSize"`
		TFTPWindow    int      `json:"tftpWindow"`
		HidePatterns  []string `json:"hidePatterns"`
		MenuAccent    string   `json:"menuAccent"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := a.o.Store.Update(func(c *config.Config) error {
		c.Tuning = config.Tuning{TFTPBlockSize: in.TFTPBlockSize, TFTPWindow: in.TFTPWindow}
		c.HidePatterns = in.HidePatterns
		c.MenuAccent = in.MenuAccent
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.o.Backend.Rescan()
	if a.o.Logf != nil {
		a.o.Logf("web interface: tuning saved (TFTP block size %d, window %d, %d hide pattern(s))", in.TFTPBlockSize, in.TFTPWindow, len(in.HidePatterns))
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
		Name string       `json:"name,omitempty"` // the name a person gave the PC
		Rule *config.Rule `json:"rule,omitempty"`
	}
	out := []pc{}
	seen := map[string]bool{}
	for _, cl := range a.o.Backend.Clients() {
		p := pc{Client: cl, Name: c.PCNames[cl.MAC]}
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
			seen[r.MAC] = true
			out = append(out, pc{Client: httpd.Client{MAC: r.MAC}, Name: c.PCNames[r.MAC], Rule: &r})
		}
	}
	for mac, name := range c.PCNames { // a named PC that is not in the list (forgotten, or not seen since the server started)
		if !seen[mac] {
			out = append(out, pc{Client: httpd.Client{MAC: mac}, Name: name})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { // (the order of a map is not one: the PCs not seen go last, by name)
		seenI, seenJ := !out[i].LastSeen.IsZero(), !out[j].LastSeen.IsZero()
		if seenI != seenJ {
			return seenI
		}
		return !seenI && out[i].MAC < out[j].MAC
	})
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		kept := out[:0]
		for _, p := range out {
			hay := strings.ToLower(strings.Join([]string{p.Name, p.MAC, p.IP, p.Manufacturer, p.Product, p.Serial, p.Hostname, p.Asset, p.LastAction, noteOf(p.Rule)}, "\n"))
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

// pcRename gives a PC a name of the person's choice, or takes it away (an empty name). The name is kept with the MAC address, also when the PC is forgotten.
func (a *Admin) pcRename(w http.ResponseWriter, r *http.Request) {
	mac, ok := config.NormalizeMAC(r.PathValue("mac"))
	if !ok {
		fail(w, http.StatusBadRequest, "that is not a MAC address (like aa:bb:cc:dd:ee:ff)")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := a.o.Store.Update(func(c *config.Config) error {
		if c.PCNames == nil {
			c.PCNames = map[string]string{}
		}
		c.PCNames[mac] = in.Name // (Validate trims it and drops an empty one)
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: %s is called %q now", mac, strings.TrimSpace(in.Name))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// pcWake sends a Wake-on-LAN packet to a PC. The PC wakes up only if its network card and its firmware are set to wake on that packet.
func (a *Admin) pcWake(w http.ResponseWriter, r *http.Request) {
	mac, ok := config.NormalizeMAC(r.PathValue("mac"))
	if !ok {
		fail(w, http.StatusBadRequest, "that is not a MAC address (like aa:bb:cc:dd:ee:ff)")
		return
	}
	if a.o.Wake == nil {
		fail(w, http.StatusNotFound, "waking PCs is not available")
		return
	}
	hw, _ := net.ParseMAC(mac)
	if err := a.o.Wake(hw); err != nil {
		fail(w, http.StatusBadGateway, "the packet could not be sent: "+err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: Wake-on-LAN packet sent to %s", mac)
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

// logDownload sends the whole log as a text file: the log file (and the older one) when there is one, else what is kept in memory.
func (a *Admin) logDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="bootlace-`+time.Now().Format("20060102-150405")+`.log"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = a.o.Log.Dump(w)
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
