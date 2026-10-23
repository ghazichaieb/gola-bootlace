// Managing the library from the web interface: the files and folders, adding by upload or from a web address, and the jobs that are running.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// fileView is one line of the library page.
type fileView struct {
	library.Entry
	SHA256 string `json:"sha256,omitempty"`
	Source string `json:"source,omitempty"`
	InUse  bool   `json:"inUse"` // a PC is using it right now
}

func (a *Admin) libraryError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, library.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, library.ErrExists):
		code = http.StatusConflict
	}
	fail(w, code, err.Error())
}

func (a *Admin) logf(format string, args ...any) {
	if a.o.Logf != nil {
		a.o.Logf(format, args...)
	}
}

func (a *Admin) filesList(w http.ResponseWriter, r *http.Request) {
	rel, err := library.CleanRel(r.URL.Query().Get("path"))
	if err != nil {
		a.libraryError(w, err)
		return
	}
	entries, err := a.o.Library.ListDir(rel)
	if err != nil {
		a.libraryError(w, err)
		return
	}
	idx := a.o.Indexer.Get()
	out := make([]fileView, 0, len(entries))
	for _, e := range entries {
		v := fileView{Entry: e, InUse: a.o.Backend.InUse(e.Path)}
		if idx != nil && e.Managed {
			if rec, ok := idx.Get(e.Path); ok {
				v.SHA256, v.Source = rec.SHA256, rec.Source
			}
		}
		out = append(out, v)
	}
	parent := ""
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		parent = rel[:i]
	}
	d := map[string]any{"path": rel, "parent": parent, "hasParent": rel != "", "entries": out, "info": a.o.Library.Info(), "limitMBps": a.o.Store.Get().ImportLimitMBps}
	if idx != nil {
		d["hashing"] = idx.Status()
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *Admin) folderMake(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.o.Library.Mkdir(in.Path); err != nil {
		a.libraryError(w, err)
		return
	}
	a.logf("web interface: folder %s made in the library", in.Path)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// filesUnder lists the ISOs and images at or under a path of the library (a file is itself).
func (a *Admin) filesUnder(rel string, depth int) []string {
	if depth > 6 {
		return nil
	}
	entries, err := a.o.Library.ListDir(rel)
	if err != nil { // not a folder: a file
		return []string{rel}
	}
	var out []string
	for _, e := range entries {
		if e.Dir {
			out = append(out, a.filesUnder(e.Path, depth+1)...)
		} else if e.Managed {
			out = append(out, e.Path)
		}
	}
	return out
}

func (a *Admin) fileRename(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Force bool   `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	from, ferr := library.CleanRel(in.From)
	to, terr := library.CleanRel(in.To)
	if ferr != nil || terr != nil {
		a.libraryError(w, errors.Join(ferr, terr))
		return
	}
	if a.o.Backend.InUse(from) && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "a PC is using this file right now", "inUse": true})
		return
	}
	var files []string // what moves, by its old path
	if _, err := a.o.Library.ListDir(from); err == nil {
		files = a.filesUnder(from, 0)
	} else if library.Managed(from) {
		files = []string{from}
	}
	if err := a.o.Library.Rename(from, to); err != nil {
		a.libraryError(w, err)
		return
	}
	moved := map[string]string{}
	for _, f := range files {
		moved[isos.IDOf(f)] = isos.IDOf(to + strings.TrimPrefix(f, from))
	}
	a.followSettings(moved, nil)
	if idx := a.o.Indexer.Get(); idx != nil {
		idx.Move(from, to)
	}
	a.o.Backend.Rescan()
	a.logf("web interface: %s renamed to %s", from, to)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) fileDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	rel, err := library.CleanRel(in.Path)
	if err != nil {
		a.libraryError(w, err)
		return
	}
	if a.o.Backend.InUse(rel) && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "a PC is using this file right now", "inUse": true})
		return
	}
	if err := a.o.Library.Delete(rel); err != nil {
		a.libraryError(w, err)
		return
	}
	if idx := a.o.Indexer.Get(); idx != nil {
		idx.Forget(rel)
	}
	a.followSettings(nil, []string{isos.IDOf(rel)})
	a.o.Backend.Rescan()
	a.logf("web interface: %s deleted from the library", rel)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// followSettings moves what the settings say about an ISO (hidden, its name in the menu, its arguments, the rules and the default that point to it) to its new
// id after a rename, and drops what belonged to ISOs that were deleted. A rule for a deleted ISO is left for the person to see.
func (a *Admin) followSettings(moved map[string]string, gone []string) {
	_ = a.o.Store.Update(func(c *config.Config) error {
		for old, id := range moved {
			if v, ok := c.Hidden[old]; ok {
				delete(c.Hidden, old)
				c.Hidden[id] = v
			}
			if v, ok := c.Titles[old]; ok {
				delete(c.Titles, old)
				c.Titles[id] = v
			}
			if v, ok := c.Args[old]; ok {
				delete(c.Args, old)
				c.Args[id] = v
			}
			for i := range c.Rules {
				for _, p := range []string{"iso:", "native:"} {
					if c.Rules[i].Action == p+old {
						c.Rules[i].Action = p + id
					}
				}
			}
			if c.Default == "iso:"+old {
				c.Default = "iso:" + id
			}
		}
		for _, id := range gone {
			delete(c.Hidden, id)
			delete(c.Titles, id)
			delete(c.Args, id)
			if c.Default == "iso:"+id {
				c.Default = ""
			}
		}
		return nil
	})
}

// ---- adding: from a web address

func (a *Admin) importCheck(w http.ResponseWriter, r *http.Request) {
	var in importer.Request
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, a.o.Importer.Check(ctx, in))
}

func (a *Admin) importStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		importer.Request
		Acknowledged bool `json:"acknowledged"` // the person has seen what it would repeat and wants it anyway
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	job, check, err := a.o.Importer.Start(ctx, in.Request, in.Acknowledged)
	switch {
	case errors.Is(err, importer.ErrNeedsAck):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "needsAck": true, "check": check})
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "check": check})
	default:
		a.logf("web interface: import of %s started (%s)", job.Dest, job.URL)
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	}
}

func (a *Admin) jobsList(w http.ResponseWriter, _ *http.Request) {
	d := map[string]any{"jobs": a.o.Importer.Jobs(), "active": a.o.Importer.Active()}
	if idx := a.o.Indexer.Get(); idx != nil {
		d["hashing"] = idx.Status()
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *Admin) jobAction(do func(*importer.Manager, string) bool, word string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !do(a.o.Importer, r.PathValue("id")) {
			fail(w, http.StatusNotFound, "there is no such job, or it is not "+word)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *Admin) limitSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MBps int `json:"mbps"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.o.Store.Update(func(c *config.Config) error { c.ImportLimitMBps = in.MBps; return nil }); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- adding: by upload

func (a *Admin) uploadCheck(w http.ResponseWriter, r *http.Request) {
	var in importer.UploadRequest
	if !readJSON(w, r, &in) {
		return
	}
	writeJSON(w, http.StatusOK, a.o.Importer.CheckUpload(in))
}

// upload takes the file as the body of the request: /api/library/upload?name=&folder=&size=&sha256=. It is written to disk as it arrives.
func (a *Admin) upload(w http.ResponseWriter, r *http.Request) {
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		fail(w, http.StatusTooManyRequests, "two uploads are running already: wait for one of them to end")
		return
	}
	q := r.URL.Query()
	size, _ := strconv.ParseInt(q.Get("size"), 10, 64)
	if r.ContentLength >= 0 && size > 0 && r.ContentLength != size {
		fail(w, http.StatusBadRequest, "the size of the upload is not the size that was announced")
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{}) // (a big file takes as long as it takes)
	_ = rc.SetWriteDeadline(time.Time{})
	name, _ := url.QueryUnescape(q.Get("name"))
	dest, warnings, err := a.o.Importer.Upload(importer.UploadRequest{Name: name, Folder: q.Get("folder"), Size: size, SHA256: q.Get("sha256")}, r.Body)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.logf("web interface: %s uploaded to the library", dest)
	a.o.Backend.Rescan()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dest": dest, "warnings": warnings})
}
