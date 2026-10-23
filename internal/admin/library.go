// The library (the folder with the ISOs) and the services PCs talk to: choosing the folder, starting and stopping the server.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"net/http"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func (a *Admin) libraryInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"library": a.o.Library.Info()})
}

func (a *Admin) libraryBrowse(w http.ResponseWriter, r *http.Request) {
	l, err := library.Browse(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *Admin) libraryInspect(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, library.Inspect(r.URL.Query().Get("path")))
}

// librarySelect makes a folder the library. A folder that is not a library yet is only made one when the request says so (the page asks the person first), and the
// services that are running are restarted on the new folder, which needs a "force" when PCs are using them.
func (a *Admin) librarySelect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path   string `json:"path"`
		Create bool   `json:"create"` // the person agreed that this folder becomes a library
		Force  bool   `json:"force"`  // the person agreed to interrupt the PCs that are using the server
	}
	if !readJSON(w, r, &in) {
		return
	}
	dir, err := library.Clean(in.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	check := library.Inspect(dir)
	if check.Problem != "" {
		fail(w, http.StatusBadRequest, check.Problem)
		return
	}
	if !check.IsLibrary && !in.Create {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "this folder is not a library yet", "needsCreate": true})
		return
	}
	if dir == a.o.Library.Path() {
		writeJSON(w, http.StatusOK, map[string]any{"library": a.o.Library.Info()})
		return
	}
	if sessions := a.o.Services.Sessions(); len(sessions) > 0 && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "PCs are using the server", "sessions": sessions})
		return
	}
	id, err := library.Prepare(dir)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	wanted := a.o.Services.Report().Wanted
	a.o.Services.Stop()
	if err := a.o.Store.Update(func(c *config.Config) error { c.Library = config.Library{Path: dir, ID: id}; return nil }); err != nil {
		if wanted {
			_ = a.o.Services.Start()
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.o.Library.Set(dir, id)
	a.o.Backend.Rescan()
	if a.o.Logf != nil {
		a.o.Logf("web interface: the library is now %s", dir)
	}
	if wanted {
		_ = a.o.Services.Start() // (a library that cannot be used is reported below, in its own state)
	}
	writeJSON(w, http.StatusOK, map[string]any{"library": a.o.Library.Info()})
}

func (a *Admin) servicesReport(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.servicesData())
}

func (a *Admin) servicesData() map[string]any {
	return map[string]any{"report": a.o.Services.Report(), "sessions": a.o.Services.Sessions(), "autoStart": !a.o.Store.Get().NoAutoStart}
}

func (a *Admin) servicesStart(w http.ResponseWriter, _ *http.Request) {
	err := a.o.Services.Start()
	if a.o.Logf != nil {
		a.o.Logf("web interface: the services were started")
	}
	data := a.servicesData()
	if err != nil {
		data["warning"] = err.Error() // wanted, but waiting for the library
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *Admin) servicesStop(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if sessions := a.o.Services.Sessions(); len(sessions) > 0 && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "PCs are using the server", "sessions": sessions})
		return
	}
	a.o.Services.Stop()
	if a.o.Logf != nil {
		a.o.Logf("web interface: the services were stopped")
	}
	writeJSON(w, http.StatusOK, a.servicesData())
}

func (a *Admin) servicesAutoStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AutoStart bool `json:"autoStart"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.o.Store.Update(func(c *config.Config) error { c.NoAutoStart = !in.AutoStart; return nil }); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.servicesData())
}
