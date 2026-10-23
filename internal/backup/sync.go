// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// FileName is the copy of the settings next to the library: <library>/.bootlace/settings.json. It never holds the secrets, because the library may be on a NAS that others can read.
const FileName = "settings.json"

// Library is what Sync needs of the library.
type Library interface {
	Check() error
	Path() string
}

// Sync keeps a copy of the settings in the library and offers it to a new container that is given the same library. A change of the settings is written to the library
// unless the library holds a copy that this container has not taken or replaced yet: that one is the person's to decide about (restore it, or keep these settings).
type Sync struct {
	Store   *config.Store
	Lib     Library
	Version string
	Logf    func(format string, args ...any)

	mu sync.Mutex
}

type savedFile struct {
	Format   int           `json:"format"`
	Saved    time.Time     `json:"saved"`
	Version  string        `json:"version"`
	Settings config.Config `json:"settings"`
}

func (s *Sync) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Sync) path() (string, error) {
	if err := s.Lib.Check(); err != nil {
		return "", err
	}
	return filepath.Join(s.Lib.Path(), library.MetaDir, FileName), nil
}

// fingerprint says whether two sets of settings are the same, secrets and the machine's own settings left out.
func fingerprint(c config.Config) string {
	// (through the same checking as when the settings are read back, so that an empty list and a missing one are the same)
	b, _ := json.Marshal(Strip(c, false))
	var d config.Config
	_ = json.Unmarshal(b, &d)
	_ = d.Validate()
	b, _ = json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func readSaved(path string) (*savedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f savedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the copy of the settings in the library cannot be read: %w", err)
	}
	if f.Format > FormatVersion {
		return nil, fmt.Errorf("the copy of the settings in the library was made by a newer version of Bootlace (%s)", f.Version)
	}
	if err := f.Settings.Validate(); err != nil {
		return nil, fmt.Errorf("the copy of the settings in the library is not valid: %w", err)
	}
	return &f, nil
}

// write puts the settings in the library file (replacing it in one step) and notes the fingerprint.
func (s *Sync) write(path string, c config.Config) error {
	data, err := json.MarshalIndent(savedFile{Format: FormatVersion, Saved: time.Now().UTC(), Version: s.Version, Settings: Strip(c, false)}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	fp := fingerprint(c)
	return s.Store.UpdateQuiet(func(cfg *config.Config) error { cfg.LibrarySync = fp; return nil })
}

// State is what the web interface shows about the copy in the library.
type State struct {
	Available bool      `json:"available"` // the library can be used
	Pending   bool      `json:"pending"`   // the library holds a copy that this container has not decided about
	Saved     time.Time `json:"saved,omitempty"`
	Version   string    `json:"version,omitempty"`
	Library   Counts    `json:"library"` // what the copy holds
	Current   Counts    `json:"current"` // what this container has
	Error     string    `json:"error,omitempty"`
}

// State looks at the library's copy.
func (s *Sync) State(pcs int) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Store.Get()
	st := State{Current: CountsOf(cur, pcs)}
	path, err := s.path()
	if err != nil {
		return st
	}
	st.Available = true
	f, err := readSaved(path)
	if errors.Is(err, os.ErrNotExist) {
		return st
	}
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Saved, st.Version, st.Library = f.Saved, f.Version, CountsOf(f.Settings, 0)
	st.Pending = fingerprint(f.Settings) != fingerprint(cur) && cur.LibrarySync != fingerprint(f.Settings)
	return st
}

// Changed is called after every change of the settings.
func (s *Sync) Changed(c config.Config) { s.Check() }

// Check writes the settings to the library when that is safe: there is no copy yet, or the copy is the one this container wrote or took last. It does nothing when the library
// holds a copy that the person has not decided about.
func (s *Sync) Check() {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path()
	if err != nil {
		return
	}
	cur := s.Store.Get()
	f, err := readSaved(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		s.logf("settings: %v", err)
		return
	case fingerprint(f.Settings) == fingerprint(cur):
		if cur.LibrarySync != fingerprint(cur) {
			_ = s.Store.UpdateQuiet(func(c *config.Config) error { c.LibrarySync = fingerprint(cur); return nil })
		}
		return
	case cur.LibrarySync != fingerprint(f.Settings):
		s.logf("settings: the library holds a copy of the settings that is not this container's: restore it or keep these settings (Server page)")
		return
	}
	if err := s.write(path, cur); err != nil {
		s.logf("settings: cannot write the copy in the library: %v", err)
	}
}

// Restore takes the library's copy as the settings of this container. The library, the admin password and the share's password of this container stay.
func (s *Sync) Restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path()
	if err != nil {
		return err
	}
	f, err := readSaved(path)
	if err != nil {
		return err
	}
	fp := fingerprint(f.Settings)
	err = s.Store.UpdateQuiet(func(c *config.Config) error {
		*c = Merge(*c, &Contents{Manifest: Manifest{Format: f.Format}, Config: f.Settings})
		c.LibrarySync = fp
		return nil
	})
	if err == nil {
		s.logf("settings: restored from the copy in the library (saved %s)", f.Saved.Local().Format("2006-01-02 15:04"))
	}
	return err
}

// Keep writes this container's settings over the library's copy.
func (s *Sync) Keep() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path()
	if err != nil {
		return err
	}
	return s.write(path, s.Store.Get())
}
