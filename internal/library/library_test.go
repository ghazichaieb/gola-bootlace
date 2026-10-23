// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPrepareMakesALibraryAndTakesItBack(t *testing.T) {
	dir := t.TempDir()
	id, err := Prepare(dir)
	if err != nil || id == "" {
		t.Fatalf("Prepare: %q, %v", id, err)
	}
	if _, err := os.Stat(filepath.Join(dir, MetaDir, markerFile)); err != nil {
		t.Fatal("no marker was written")
	}
	again, err := Prepare(dir)
	if err != nil || again != id {
		t.Fatalf("a folder that is a library must keep its id: %q then %q (%v)", id, again, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, MetaDir, probeFile+"*")); len(left) != 0 {
		t.Fatalf("the write test left files behind: %v", left)
	}
}

func TestPrepareRefusesWhatCannotBeALibrary(t *testing.T) {
	for _, p := range []string{"", "   ", string(filepath.Separator)} {
		if _, err := Prepare(p); err == nil {
			t.Errorf("Prepare(%q) was accepted", p)
		}
	}
	file := filepath.Join(t.TempDir(), "a file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(file); err == nil {
		t.Error("a file was accepted as the library")
	}
	if _, err := Prepare(filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Error("a folder that does not exist was accepted (it must not be made)")
	}
}

func TestPrepareLeavesNothingBehindInAFolderItCannotWriteTo(t *testing.T) {
	if os.Getuid() == 0 || strings.Contains(os.Getenv("OS"), "Windows") {
		t.Skip("folder permissions cannot be tried here")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip(err)
	}
	defer os.Chmod(dir, 0o755)
	if _, err := Prepare(dir); err == nil {
		t.Fatal("a read-only folder was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, MetaDir)); err == nil {
		t.Fatal("a failed Prepare left the marker folder behind")
	}
}

func TestCheck(t *testing.T) {
	l := New("", "")
	if err := l.Check(); err != ErrNone {
		t.Fatalf("no library: %v", err)
	}
	dir := t.TempDir()
	id, err := Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Set(dir, id)
	if err := l.Check(); err != nil {
		t.Fatalf("a library that is there: %v", err)
	}
	if info := l.Info(); !info.Available || info.FreeBytes == 0 {
		t.Fatalf("info: %+v", info)
	}
	// the mount is gone: the folder is there again, but empty and without the marker
	if err := os.RemoveAll(filepath.Join(dir, MetaDir)); err != nil {
		t.Fatal(err)
	}
	if err := l.Check(); err == nil || !strings.Contains(err.Error(), "mounted") {
		t.Fatalf("a folder without the marker must not count as the library: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, MetaDir)); err == nil {
		t.Fatal("Check must never write the marker itself")
	}
	// another library in the same place
	if _, err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if err := l.Check(); err == nil || !strings.Contains(err.Error(), "another library") {
		t.Fatalf("a different library at the same path: %v", err)
	}
	// the folder is gone
	l.Set(filepath.Join(dir, "gone"), id)
	if err := l.Check(); err == nil {
		t.Fatal("a folder that does not exist is available")
	}
}

func TestInspectWritesNothing(t *testing.T) {
	dir := t.TempDir()
	in := Inspect(dir)
	if in.Problem != "" || in.IsLibrary {
		t.Fatalf("a plain folder: %+v", in)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("Inspect wrote into the folder")
	}
	if _, err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if !Inspect(dir).IsLibrary {
		t.Fatal("a library is not recognised")
	}
	if Inspect(filepath.Join(dir, "nope")).Problem == "" {
		t.Fatal("a folder that does not exist has no problem")
	}
}

func TestBrowseListsFoldersOnly(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"b", "A", MetaDir} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.iso"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	l, err := Browse(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range l.Dirs {
		names = append(names, d.Name)
	}
	if strings.Join(names, ",") != "A,b" {
		t.Fatalf("folders: %v (files and the marker folder must not be listed, order is by name)", names)
	}
	if !l.Dirs[1].IsLibrary || l.Dirs[0].IsLibrary {
		t.Fatalf("library flags: %+v", l.Dirs)
	}
	if l.Parent != filepath.Dir(dir) {
		t.Fatalf("parent: %q", l.Parent)
	}
	if _, err := Browse(filepath.Join(dir, "file.iso")); err == nil {
		t.Fatal("a file was listed as a folder")
	}
}

// Many callers at once (the page refreshing, an upload, the hasher) all get the real answer, never a false "does not answer".
func TestConcurrentChecksAllGetTheRealAnswer(t *testing.T) {
	l, _ := ready(t)
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- l.Check() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a check failed while the library was fine: %v", err)
		}
	}
}
