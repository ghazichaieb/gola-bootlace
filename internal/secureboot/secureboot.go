// Package secureboot holds the iPXE project's signed boot files that are built into the program, for PCs with UEFI Secure Boot on: a shim signed by Microsoft and an
// iPXE signed by the iPXE project's own CA, which that shim trusts. They are the unchanged x86-64 files of one release of the iPXE project (assets/MANIFEST.json says
// which, with the checksum of every file), and the program checks at start that the bytes it carries are the ones the manifest lists: a build with a file that was
// changed by mistake refuses to serve it. tools/update-secure-boot-assets.sh renews them; docs/secure-boot.md says when and why.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
//
// The files are not Gola's: iPXE is GPL-2.0-or-later (see NOTICE for the exact terms and where the source is) and the shim is BSD-2-Clause.
package secureboot

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed assets/MANIFEST.json assets/ipxe-shimx64.efi assets/ipxe.efi
var embedded embed.FS

// Shim is the name the shim is served under (the iPXE project's file shimx64.efi). The shim chooses its second stage from its own file name, by taking "shim" out of it, so
// this name is what makes it ask for SecondStage.
const Shim = "ipxe-shimx64.efi"

// SecondStage is the iPXE the shim starts next.
const SecondStage = "ipxe.efi"

// Prefix is the TFTP folder the files are served from, shared with the folder route of a key of the owner's own.
const Prefix = "sb/"

// File is one entry of the manifest.
type File struct {
	Name   string `json:"name"`   // the name it is served under
	From   string `json:"from"`   // its name in the release archive
	Size   int64  `json:"size"`   // bytes
	SHA256 string `json:"sha256"` // of the file, lower-case hexadecimal
}

// Manifest says where the files came from. It is assets/MANIFEST.json, written by tools/update-secure-boot-assets.sh.
type Manifest struct {
	Source        string `json:"source"`        // the address of the release archive
	Release       string `json:"release"`       // the iPXE release, for example v2.0.0
	Released      string `json:"released"`      // its date
	Added         string `json:"added"`         // the day the files were put into this program
	TarballSHA256 string `json:"tarballSha256"` // of the release archive as it was downloaded
	Folder        string `json:"folder"`        // the folder of the archive the files come from (x86_64-sb)
	Files         []File `json:"files"`
}

// Set is a manifest and the files it lists, with the result of checking one against the other. The zero value has no files.
type Set struct {
	Manifest Manifest
	files    map[string][]byte
	err      error
}

// NewSet checks files against a manifest. A set that does not match is still returned, with Err set: it serves nothing.
func NewSet(m Manifest, files map[string][]byte) *Set {
	s := &Set{Manifest: m, files: files}
	s.err = s.verify()
	return s
}

func (s *Set) verify() error {
	m := s.Manifest
	if m.Release == "" || m.Source == "" {
		return fmt.Errorf("the manifest does not say which release the files are from")
	}
	if !isSHA256(m.TarballSHA256) {
		return fmt.Errorf("the manifest has no checksum for the release archive")
	}
	listed := map[string]bool{}
	for _, f := range m.Files {
		if listed[f.Name] {
			return fmt.Errorf("the manifest lists %s twice", f.Name)
		}
		listed[f.Name] = true
		if !isSHA256(f.SHA256) {
			return fmt.Errorf("the manifest has no checksum for %s", f.Name)
		}
		b, ok := s.files[f.Name]
		if !ok {
			return fmt.Errorf("%s is in the manifest but not in the program", f.Name)
		}
		if int64(len(b)) != f.Size {
			return fmt.Errorf("%s is %d bytes, the manifest says %d", f.Name, len(b), f.Size)
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != f.SHA256 {
			return fmt.Errorf("%s does not match the checksum in the manifest", f.Name)
		}
	}
	for _, need := range []string{Shim, SecondStage} {
		if !listed[need] {
			return fmt.Errorf("the manifest does not list %s", need)
		}
	}
	for name := range s.files {
		if !listed[name] {
			return fmt.Errorf("%s is in the program but not in the manifest", name)
		}
	}
	return nil
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// Err is nil when the files are the ones the manifest lists. Otherwise it says what is wrong, and the set serves nothing.
func (s *Set) Err() error {
	if s == nil {
		return fmt.Errorf("no built-in Secure Boot files")
	}
	return s.err
}

// Open is the content of the file served under this exact name: nothing for any other name (no case folding, no path), and nothing at all when the set does not match its manifest.
func (s *Set) Open(name string) ([]byte, bool) {
	if s.Err() != nil {
		return nil, false
	}
	b, ok := s.files[name]
	return b, ok
}

// Names are the files the set serves, in a fixed order (empty when the set does not match its manifest).
func (s *Set) Names() []string {
	if s.Err() != nil {
		return nil
	}
	names := make([]string, 0, len(s.files))
	for n := range s.files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Info is a file of the set as the web interface shows it.
type Info struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}

// Infos are the manifest's files with what each is for, in the order the shim and iPXE are fetched.
func (s *Set) Infos() []Info {
	var out []Info
	for _, name := range []string{Shim, SecondStage} {
		for _, f := range s.Manifest.Files {
			if f.Name != name {
				continue
			}
			role := "the boot file the PC is told to start (the shim, signed by Microsoft)"
			if name == SecondStage {
				role = "what the shim starts next (iPXE, signed by the iPXE project)"
			}
			out = append(out, Info{Name: f.Name, From: f.From, Size: f.Size, SHA256: f.SHA256, Role: role})
		}
	}
	return out
}

var (
	builtinOnce sync.Once
	builtinSet  *Set
)

// Builtin is the set that is part of the program, checked once at its first use.
func Builtin() *Set {
	builtinOnce.Do(func() {
		builtinSet = load()
	})
	return builtinSet
}

func load() *Set {
	var m Manifest
	raw, err := embedded.ReadFile("assets/MANIFEST.json")
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		return &Set{err: fmt.Errorf("cannot read the manifest of the built-in Secure Boot files: %w", err)}
	}
	files := map[string][]byte{}
	entries, err := embedded.ReadDir("assets")
	if err != nil {
		return &Set{Manifest: m, err: err}
	}
	for _, e := range entries {
		if e.Name() == "MANIFEST.json" {
			continue
		}
		b, err := embedded.ReadFile("assets/" + e.Name())
		if err != nil {
			return &Set{Manifest: m, err: err}
		}
		files[e.Name()] = b
	}
	return NewSet(m, files)
}
