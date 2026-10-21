// Package httpd is the web side of the server: the boot menu (an iPXE script written for the PC that asks), the ISO files with range requests,
// and the files inside an ISO (kernel, start-up disk, packages) so that a Linux can start from them over the network.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/assets"
	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
	"github.com/ghazichaieb/gola-bootlace/internal/smbshare"
	"github.com/ghazichaieb/gola-bootlace/internal/winpe"
)

// Server builds the handler. ISODir is read at every request, so an ISO copied into the folder shows up in the next menu without a restart.
type Server struct {
	ISODir      string
	Title       string // the heading of the menu
	Base        string // how PCs reach this server, for example http://192.168.88.1:8080 (no trailing slash)
	Logf        func(format string, args ...any)
	Menu        menu.Options // which entry is chosen when nobody chooses, and after how many seconds
	SMBPort     int          // the port of the share that shows an ISO to Windows' set-up environment (445 unless something else holds it)
	SMBPassword string       // what Windows' set-up environment logs in with (made up when the server starts)
	Pick        string       // when not empty: the first entry whose name contains this text is the one chosen when nobody chooses (a test bench's shortcut)

	mu    sync.Mutex
	known map[string]recognised // what each ISO was found to be, by path, size and time (reading an ISO's folders takes a moment)
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
	mux := http.NewServeMux()
	mux.HandleFunc("/boot.ipxe", s.menu)
	mux.HandleFunc("/iso/", s.iso)
	mux.HandleFunc("/fs/", s.inside)
	mux.HandleFunc("/ipxe/", s.loader)
	mux.HandleFunc("/w/", s.windows)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	return mux
}

// recognise looks inside the ISO (once) and says whether it is a Linux this server knows how to start over the network.
func (s *Server) recognise(iso isos.ISO) (family.Boot, bool) {
	key := fmt.Sprintf("%s|%d|%d", iso.Path, iso.Size, iso.ModTime.UnixNano())
	s.mu.Lock()
	if r, ok := s.known[key]; ok {
		s.mu.Unlock()
		return r.boot, r.ok
	}
	s.mu.Unlock()
	var result recognised
	if f, err := os.Open(iso.Path); err == nil {
		if img, err := disc.Open(f, iso.Size); err == nil {
			result.boot, result.ok = family.Detect(img, family.URLs{ISO: s.Base + isoPath(iso.Name), FS: s.Base + "/fs/" + iso.ID, Win: s.Base + "/w/" + iso.ID, Base: s.Base})
		}
		f.Close()
	}
	s.mu.Lock()
	if s.known == nil {
		s.known = map[string]recognised{}
	}
	s.known[key] = result
	s.mu.Unlock()
	return result.boot, result.ok
}

// Entries are the menu lines: one per ISO. An ISO of a known Linux is started from its own kernel; any other is handed to the PC as a disk.
func (s *Server) Entries() ([]menu.Entry, error) {
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return nil, err
	}
	entries := make([]menu.Entry, 0, len(list))
	for _, iso := range list {
		size := fmt.Sprintf("%.1f GB", float64(iso.Size)/(1<<30))
		if b, ok := s.recognise(iso); ok {
			entries = append(entries, menu.Entry{Name: fmt.Sprintf("%s  [%s, %s]", iso.Title, b.Family, size), Commands: b.Commands})
			continue
		}
		entries = append(entries, menu.Entry{
			Name: fmt.Sprintf("%s  [as a disk, %s]", iso.Title, size),
			Commands: []string{
				"echo Loading " + oneWord(iso.Title) + " ... the whole ISO is read into memory first, this takes a while",
				"sanboot --no-describe --drive 0x80 " + s.Base + isoPath(iso.Name),
			},
		})
	}
	return entries, nil
}

func (s *Server) menu(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Entries()
	if err != nil {
		http.Error(w, "cannot read the ISO folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	opts := s.Menu
	if s.Pick != "" {
		for i, e := range entries {
			if strings.Contains(strings.ToLower(e.Name), strings.ToLower(s.Pick)) {
				opts.Default = i + 1
				break
			}
		}
	}
	s.logf("menu for %s: %d ISO file(s)", r.RemoteAddr, len(entries))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, menu.ScriptWith(s.Title, entries, opts))
}

func (s *Server) iso(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/iso/")
	iso, ok := isos.Find(s.ISODir, name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(iso.Path)
	if err != nil {
		http.Error(w, "cannot open the file", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
		s.logf("%s downloads %s", r.RemoteAddr, iso.Name)
	}
	w.Header().Set("Content-Type", "application/x-iso9660-image")
	http.ServeContent(w, r, iso.Name, iso.ModTime, f) // answers range requests and HEAD itself
}

// inside serves one file from the inside of an ISO: /fs/<id>/<path in the ISO>.
func (s *Server) inside(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/fs/")
	id, name, _ := strings.Cut(rest, "/")
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		http.Error(w, "cannot read the ISO folder", http.StatusInternalServerError)
		return
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			http.Error(w, "cannot open the ISO", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		img, err := disc.Open(f, iso.Size)
		if err != nil {
			http.Error(w, "this ISO has no readable file system", http.StatusUnprocessableEntity)
			return
		}
		file, err := img.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Header.Get("Range") == "" || strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
			s.logf("%s fetches %s from inside %s", r.RemoteAddr, name, iso.Title)
		}
		http.ServeContent(w, r, name, iso.ModTime, file.Reader())
		return
	}
	http.NotFound(w, r)
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
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		http.Error(w, "cannot read the ISO folder", http.StatusInternalServerError)
		return
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		u, err := url.Parse(s.Base)
		if err != nil {
			http.Error(w, "bad server address", http.StatusInternalServerError)
			return
		}
		set := winpe.Settings{Base: s.Base, FS: s.Base + "/fs/" + iso.ID, Win: s.Base + "/w/" + iso.ID, Host: u.Hostname(), Share: smbshare.ShareName, Folder: iso.ID, User: "bootlace", Password: s.SMBPassword, Port: s.SMBPort, Hold: os.Getenv("BOOTLACE_WINPE_HOLD") != ""}
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
