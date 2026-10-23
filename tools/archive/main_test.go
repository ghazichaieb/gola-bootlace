// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func folder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bootlace-1.2.3-linux-amd64")
	for name, body := range map[string]string{"bootlace": "binary", "README.md": "read me", "docs/more.md": "more"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTarHasOwnerZeroTheProgramExecutableAndOneTopFolder(t *testing.T) {
	dir, out := folder(t), filepath.Join(t.TempDir(), "x.tar.gz")
	when := time.Unix(1_700_000_000, 0).UTC()
	if err := pack(dir, out, when); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(out)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]int64{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || !h.ModTime.Equal(when) {
			t.Errorf("%s: owner %d/%d %q time %v", h.Name, h.Uid, h.Gid, h.Uname, h.ModTime)
		}
		got[h.Name] = h.Mode
	}
	want := map[string]int64{"bootlace-1.2.3-linux-amd64/": 0o755, "bootlace-1.2.3-linux-amd64/bootlace": 0o755, "bootlace-1.2.3-linux-amd64/README.md": 0o644,
		"bootlace-1.2.3-linux-amd64/docs/": 0o755, "bootlace-1.2.3-linux-amd64/docs/more.md": 0o644}
	for name, mode := range want {
		if g, ok := got[name]; !ok || g != mode {
			t.Errorf("%s: mode %o (present %v), want %o", name, g, ok, mode)
		}
	}
	if len(got) != len(want) {
		t.Errorf("entries: %v", got)
	}
}

func TestTheSameSourcesGiveTheSameArchive(t *testing.T) {
	dir := folder(t)
	a, b := filepath.Join(t.TempDir(), "a.tar.gz"), filepath.Join(t.TempDir(), "b.tar.gz")
	when := time.Unix(1_700_000_000, 0)
	if err := pack(dir, a, when); err != nil {
		t.Fatal(err)
	}
	if err := pack(dir, b, when); err != nil {
		t.Fatal(err)
	}
	x, _ := os.ReadFile(a)
	y, _ := os.ReadFile(b)
	if !bytes.Equal(x, y) {
		t.Fatal("two archives of the same folder differ")
	}
}

func TestZipHasTheProgramExecutable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bootlace-1.2.3-windows-amd64")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bootlace.exe"), []byte("exe"), 0o600)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o600)
	out := filepath.Join(t.TempDir(), "x.zip")
	if err := pack(dir, out, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	modes := map[string]os.FileMode{}
	for _, f := range r.File {
		modes[f.Name] = f.Mode().Perm()
	}
	if modes["bootlace-1.2.3-windows-amd64/bootlace.exe"] != 0o755 || modes["bootlace-1.2.3-windows-amd64/README.md"] != 0o644 || len(modes) != 2 {
		t.Fatalf("%v", modes)
	}
}

func TestAnOutputOfAnotherKindIsRefused(t *testing.T) {
	if err := pack(folder(t), filepath.Join(t.TempDir(), "x.rar"), time.Now()); err == nil {
		t.Fatal("accepted")
	}
}
