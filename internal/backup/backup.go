// Package backup packs the settings that cannot be rebuilt from the library (rules, PC names, ISO titles and hidden flags, arguments, menu settings) into one zip file, reads such a
// file back, and keeps a copy without secrets next to the library so that a new container given the same library can offer to restore it.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// FormatVersion is the version of the file layout. A file with a higher number was made by a newer program and is refused rather than half applied.
const FormatVersion = 1

// Names of the files in the zip.
const (
	manifestName = "manifest.json"
	settingsName = "settings.json"
	pcsName      = "pcs.json"

	maxPart = 8 << 20 // the largest file inside a backup that is read
)

// Manifest says what a backup is.
type Manifest struct {
	Format  int       `json:"format"`
	Program string    `json:"program"`
	Version string    `json:"version"` // of the program that made it
	Created time.Time `json:"created"`
	Secrets bool      `json:"secrets"` // the admin password hash and the share's password are in it
}

// Contents is a backup read back.
type Contents struct {
	Manifest Manifest
	Config   config.Config
	PCs      json.RawMessage // the list of PCs as the program kept it; empty when the backup has none
}

// Strip is the settings as they are saved: without the library (it belongs to the machine) and without the secrets unless they are asked for.
func Strip(c config.Config, secrets bool) config.Config {
	c.Library = config.Library{}
	c.LibrarySync = ""
	c.PasswordChosen, c.FirstStartDone = false, false // (belong to this container)
	if !secrets {
		c.AdminHash, c.SMBPassword = "", ""
		c.BootPasswords = nil // (hashes of the passwords the menu asks for: kept out of the library copy and of a backup without secrets)
	}
	return c
}

// WriteZip packs the settings (and the list of PCs, when there is one) into a zip file.
func WriteZip(w io.Writer, c config.Config, pcs json.RawMessage, secrets bool, programVersion string, now time.Time) error {
	zw := zip.NewWriter(w)
	put := func(name string, v any) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	if err := put(manifestName, Manifest{Format: FormatVersion, Program: "Gola Bootlace", Version: programVersion, Created: now.UTC(), Secrets: secrets}); err != nil {
		return err
	}
	if err := put(settingsName, Strip(c, secrets)); err != nil {
		return err
	}
	if len(pcs) > 0 {
		if err := put(pcsName, pcs); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ReadZip reads a backup. It refuses a file that is not a backup of this program, one made by a newer version, and one that holds anything unexpected.
func ReadZip(data []byte) (*Contents, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("that is not a zip file")
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		switch f.Name {
		case manifestName, settingsName, pcsName:
		default:
			return nil, fmt.Errorf("the file holds %q, which a backup does not", f.Name)
		}
		if f.UncompressedSize64 > maxPart {
			return nil, fmt.Errorf("%s is too big for a backup", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxPart+1))
		rc.Close()
		if err != nil || len(b) > maxPart {
			return nil, fmt.Errorf("%s cannot be read", f.Name)
		}
		parts[f.Name] = b
	}
	var c Contents
	if parts[manifestName] == nil || json.Unmarshal(parts[manifestName], &c.Manifest) != nil || c.Manifest.Program != "Gola Bootlace" {
		return nil, errors.New("that is not a backup made by Gola Bootlace")
	}
	if c.Manifest.Format > FormatVersion {
		return nil, fmt.Errorf("this backup was made by a newer version of Bootlace (%s) and cannot be read by this one (%s); update the program first", c.Manifest.Version, "format "+fmt.Sprint(FormatVersion))
	}
	if c.Manifest.Format < 1 {
		return nil, errors.New("the backup has no format number")
	}
	if parts[settingsName] == nil || json.Unmarshal(parts[settingsName], &c.Config) != nil {
		return nil, errors.New("the backup has no readable settings")
	}
	if err := c.Config.Validate(); err != nil {
		return nil, fmt.Errorf("the settings in the backup are not valid: %w", err)
	}
	c.PCs = parts[pcsName]
	return &c, nil
}

// Merge is what the settings become when a backup is restored: everything from the backup except what belongs to this machine (the library) and, unless the backup holds
// them, the secrets.
func Merge(current config.Config, in *Contents) config.Config {
	next := in.Config
	next.Library, next.LibrarySync = current.Library, current.LibrarySync
	next.PasswordChosen, next.FirstStartDone = current.PasswordChosen, current.FirstStartDone
	if !in.Manifest.Secrets {
		next.AdminHash, next.SMBPassword = current.AdminHash, current.SMBPassword
		next.BootPasswords = current.BootPasswords
	}
	return next
}

// Counts are the numbers a person compares before agreeing to a restore.
type Counts struct {
	Rules       int `json:"rules"`
	PCNames     int `json:"pcNames"`
	Titles      int `json:"titles"`
	Hidden      int `json:"hidden"`
	Args        int `json:"args"`
	HidePattern int `json:"hidePatterns"`
	PCs         int `json:"pcs"`
}

// CountsOf counts what a set of settings holds; pcs is the number of PCs in the list that goes with it.
func CountsOf(c config.Config, pcs int) Counts {
	return Counts{Rules: len(c.Rules), PCNames: len(c.PCNames), Titles: len(c.Titles), Hidden: len(c.Hidden), Args: len(c.Args), HidePattern: len(c.HidePatterns), PCs: pcs}
}
