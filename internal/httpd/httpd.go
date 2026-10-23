// Package httpd is the web side of the server: the boot menu (an iPXE script written for the PC that asks), the ISO files with range requests,
// and the files inside an ISO (kernel, start-up disk, packages) so that a Linux can start from them over the network.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/diskimg"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/gfx"
	"github.com/ghazichaieb/gola-bootlace/internal/handoff"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
	"github.com/ghazichaieb/gola-bootlace/internal/winpe"
)

// Server builds the handler. ISODir is read at every request, so an ISO copied into the folder shows up in the next menu without a restart.
type Server struct {
	ISODir string
	// Library, when set, is the folder the ISOs are in and replaces ISODir; it can be changed while the server runs.
	Library *library.Library
	// Active, when set, says whether PCs are served: while it is false everything but the web interface answers "stopped".
	Active func() bool
	// Access, when set, decides which addresses are answered (the local network by default).
	Access      *access.Policy
	Base        string // how PCs reach this server, for example http://192.168.88.1:8080 (no trailing slash)
	Logf        func(format string, args ...any)
	Menu        menu.Options // which entry is chosen when nobody chooses, and after how many seconds
	SMBPort     int          // the port of the share that shows an ISO to Windows' set-up environment (445 unless something else holds it)
	SMBPassword string       // what Windows' set-up environment logs in with (made up when the server starts)
	Pick        string       // when not empty: the first entry whose name contains this text is the one chosen when nobody chooses (a test bench's shortcut)

	// Config holds the settings made in the web interface; without it the fields above are the settings.
	Config *config.Store
	// PCFile is where the list of PCs is kept between runs; empty keeps it in memory only.
	PCFile string
	// Wimlib is wimlib-imagex (empty: looked for on the PATH) and CacheDir where the copies of boot.wim with Bootlace's files in them are kept: both are for starting
	// Windows with the firmware's own boot manager (UEFI Secure Boot on).
	Wimlib, CacheDir string
	// NBDPort is the port of the network block disk server (0: none, a live Linux then downloads its ISO into memory).
	NBDPort int
	// ISCSIPort is the port of the iSCSI target that shows an ISO as a CD-ROM (0: none, the ISO is then downloaded whole by iPXE).
	ISCSIPort int

	disks   *diskimg.Manager // unpacks compressed disk images (see package diskimg)
	patchMu sync.Mutex
	// Mount adds handlers (the web interface) to the paths of the server.
	Mount map[string]http.Handler

	started     time.Time
	useMu       sync.Mutex
	using       map[string]int // ISOs that PCs are reading, by name in the library
	busyMu      sync.Mutex
	busy        map[string]int // PCs with a request in progress, by address
	clientsMu   sync.Mutex
	clients     map[string]*Client
	saveMu      sync.Mutex
	savePending bool
	mu          sync.Mutex
	extras      []extraCache          // the last few extra initramfs archives that take long to make (see extraArchive)
	known       map[string]recognised // what each ISO was found to be, by path, size and time (reading an ISO's folders takes a moment)
	looking     map[string]bool       // the ISOs that are being looked into for what no family is known by (see recogniseGeneric)
	lookMu      sync.Mutex            // one such look at a time: it reads the whole start-up disk
	findings    family.Findings       // what those looks found
	// Handoff holds the PCs that iPXE sent to FreeBSD's loader; their TFTP requests are answered from the ISO they chose.
	Handoff *handoff.Registry
}

type recognised struct {
	boot family.Boot
	ok   bool
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Handler serves /boot.ipxe (the menu), /iso/<name> (the files), /fs/<id>/<path> (what is inside an ISO) and /health.
func (s *Server) Handler() http.Handler {
	if s.started.IsZero() {
		s.started = time.Now()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/boot.ipxe", s.bootScript)
	mux.HandleFunc("/menu", s.menu)
	mux.HandleFunc("/x/", s.extraInitrd)
	mux.HandleFunc("/bg.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(gfx.Background())
	})
	mux.HandleFunc("/iso/", s.iso)
	mux.HandleFunc("/fs/", s.inside)
	mux.HandleFunc("/ipxe/", s.loader)
	mux.HandleFunc("/w/", s.windows)
	mux.HandleFunc("/next", s.next)
	mux.HandleFunc("/unpack/", s.unpack)
	for path, h := range s.Mount {
		mux.Handle(path, h)
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	return s.gated(mux)
}

// isoDir is the folder the ISOs are in now.
func (s *Server) isoDir() string {
	if s.Library != nil {
		return s.Library.Path()
	}
	return s.ISODir
}

// gated lets the web interface through always, and everything else only while the server is running; the PCs that are being served right now are counted.
func (s *Server) gated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Access != nil && !s.Access.AllowedAddr(addrOf(r.RemoteAddr)) { // every page, the web interface included
			http.Error(w, "Gola Bootlace answers the local network only", http.StatusForbidden)
			return
		}
		p := r.URL.Path
		if p == "/" || p == "/health" || strings.HasPrefix(p, "/admin/") || strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if s.Active != nil && !s.Active() {
			http.Error(w, "Gola Bootlace is stopped", http.StatusServiceUnavailable)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			s.busyMu.Lock()
			if s.busy == nil {
				s.busy = map[string]int{}
			}
			s.busy[host]++
			s.busyMu.Unlock()
			defer func() {
				s.busyMu.Lock()
				if s.busy[host]--; s.busy[host] <= 0 {
					delete(s.busy, host)
				}
				s.busyMu.Unlock()
			}()
		}
		next.ServeHTTP(w, r)
	})
}

// addrOf is the address of a "host:port" string, as a net.Addr.
func addrOf(hostport string) net.Addr {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return &net.IPAddr{IP: net.ParseIP(host)}
}

// Sessions are the PCs that are fetching something over the web port right now, once each.
func (s *Server) Sessions() []string {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	out := make([]string, 0, len(s.busy))
	for h := range s.busy {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// find is isos.Find with the unpacked files taken into account (see scan).
func (s *Server) find(name string) (isos.ISO, bool) {
	list, err := s.scan()
	if err != nil {
		return isos.ISO{}, false
	}
	for _, iso := range list {
		if iso.Name == name {
			return iso, true
		}
	}
	return isos.ISO{}, false
}

func (s *Server) iso(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/iso/")
	iso, ok := s.find(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if iso.Packed != "" {
		http.Error(w, "the file is not unpacked yet", http.StatusConflict)
		return
	}
	release := s.hold(iso.Name) // (a PC is downloading it: it is in use)
	defer release()
	f, err := os.Open(iso.Path)
	if err != nil {
		http.Error(w, "cannot open the file", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
		s.logf("%s downloads %s", r.RemoteAddr, iso.Name)
		s.noteFetch(r.RemoteAddr, iso.Title)
	}
	w.Header().Set("Content-Type", "application/x-iso9660-image")
	http.ServeContent(w, r, iso.Name, iso.ModTime, f) // answers range requests and HEAD itself
}

// inside serves one file from the inside of an ISO: /fs/<id>/<path in the ISO>.
func (s *Server) inside(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/fs/")
	spec, name, _ := strings.Cut(rest, "/")
	src, err := s.openSource(spec)
	if err != nil {
		if errors.Is(err, errNoSuchISO) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer src.Close()
	if src.FS == nil {
		http.Error(w, "this ISO has no readable file system", http.StatusUnprocessableEntity)
		return
	}
	file, err := src.FS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Header.Get("Range") == "" || strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
		s.logf("%s fetches %s from inside %s", r.RemoteAddr, name, src.ISO.Title)
		s.noteFetch(r.RemoteAddr, src.ISO.Title)
	}
	http.ServeContent(w, r, name, src.ISO.ModTime, file.Reader())
}

// loader serves the boot loaders embedded in the program (wimboot, for Windows).
func (s *Server) loader(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/ipxe/")
	f, err := assets.IPXE().Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "cannot read the file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, time.Time{}, rs)
}

// windows serves the files made for one Windows ISO: /w/<id>/boot.ipxe, winpeshl.ini and bootlace.cmd.
func (s *Server) windows(w http.ResponseWriter, r *http.Request) {
	id, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/w/"), "/")
	list, err := s.scan()
	if err != nil {
		http.Error(w, "cannot read the ISO folder", http.StatusInternalServerError)
		return
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		set, err := s.winSettings(iso)
		if err != nil {
			http.Error(w, "bad server address", http.StatusInternalServerError)
			return
		}
		set.PEOnly = s.windowsPEOnly(iso)
		var body string
		switch file {
		case "boot.ipxe":
			body = winpe.IPXE(set)
		case "winpeshl.ini":
			body = winpe.ShellIni()
		case "bootlace.cmd":
			body = winpe.Cmd(set)
		default:
			http.NotFound(w, r)
			return
		}
		s.logf("%s fetches %s for %s", r.RemoteAddr, file, iso.Title)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, body)
		return
	}
	http.NotFound(w, r)
}

// isoPath is the URL path of an ISO, escaped.
func isoPath(name string) string {
	return (&url.URL{Path: "/iso/" + name}).EscapedPath()
}

// oneWord keeps a title from breaking an iPXE command line.
func oneWord(s string) string {
	return strings.NewReplacer(" ", "_", "$", "", "{", "", "}", "", ";", "", "|", "", "&", "").Replace(s)
}
