// Package httpd is the web side of the server: the boot menu (an iPXE script written for the PC that asks) and the ISO files, with range requests.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// Server builds the handler. ISODir is read at every request, so an ISO copied into the folder shows up in the next menu without a restart.
type Server struct {
	ISODir string
	Title  string                                   // the heading of the menu
	Base   string                                   // how PCs reach this server, for example http://192.168.88.1:8080 (no trailing slash)
	Logf   func(format string, args ...any)
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Handler serves /boot.ipxe (the menu), /iso/<name> (the files) and /health.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/boot.ipxe", s.menu)
	mux.HandleFunc("/iso/", s.iso)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	return mux
}

// Entries are the menu lines: one per ISO.
func (s *Server) Entries() ([]menu.Entry, error) {
	list, err := isos.Scan(s.ISODir)
	if err != nil {
		return nil, err
	}
	entries := make([]menu.Entry, 0, len(list))
	for _, iso := range list {
		entries = append(entries, menu.Entry{
			Name: fmt.Sprintf("%s  (%.1f GB)", iso.Title, float64(iso.Size)/(1<<30)),
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
	s.logf("menu for %s: %d ISO file(s)", r.RemoteAddr, len(entries))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, menu.Script(s.Title, entries))
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
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "cannot read the file", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, iso.Name, info.ModTime(), f) // answers range requests and HEAD itself
}

// isoPath is the URL path of an ISO, escaped.
func isoPath(name string) string {
	return (&url.URL{Path: "/iso/" + name}).EscapedPath()
}

// oneWord keeps a title from breaking an iPXE command line.
func oneWord(s string) string {
	return strings.NewReplacer(" ", "_", "$", "", "{", "", "}", "", ";", "", "|", "", "&", "").Replace(s)
}
