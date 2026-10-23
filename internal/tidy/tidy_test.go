package tidy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func write(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestOnlyWhatIsLeftOverIsRemoved(t *testing.T) {
	root := t.TempDir()
	id, err := library.Prepare(root)
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(root, id)
	cache := t.TempDir()

	iso := filepath.Join(root, "keep.iso")
	write(t, iso, 72*time.Hour)
	incoming := filepath.Join(root, library.MetaDir, "incoming")
	oldPart, ownedPart, freshPart := filepath.Join(incoming, "aaa-old.part"), filepath.Join(incoming, "bbb-owned.part"), filepath.Join(incoming, "ccc-fresh.part")
	write(t, oldPart, 30*time.Hour)
	write(t, ownedPart, 30*time.Hour)
	write(t, freshPart, time.Hour)
	jobs := filepath.Join(incoming, "jobs.json")
	write(t, jobs, 30*time.Hour) // not a .part file

	kept, gone := "0123456789", "abcdefabcd"
	keptWim, goneWim := filepath.Join(cache, "boot-"+kept+"-0123456789.wim"), filepath.Join(cache, "boot-"+gone+"-0123456789.wim")
	keptImg, goneImg := filepath.Join(cache, "images", "disk-"+kept+"-0123456789.img"), filepath.Join(cache, "images", "disk-"+gone+"-0123456789.iso")
	stranger := filepath.Join(cache, "notes.txt") // not a name the server gives
	for _, p := range []string{keptWim, goneWim, keptImg, goneImg, stranger} {
		write(t, p, time.Hour)
	}

	idx := library.OpenIndex(lib)
	if err := idx.Set("keep.iso", library.Record{SHA256: "x"}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "temp.iso"), time.Hour)
	if err := idx.Set("temp.iso", library.Record{SHA256: "y"}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, "temp.iso"))

	var log []string
	tdy := &Tidier{Library: lib, Index: func() *library.Index { return idx }, Caches: func() []string { return []string{cache} },
		ISOIDs: func() (map[string]bool, error) { return map[string]bool{kept: true}, nil },
		Owned:  func() map[string]bool { return map[string]bool{"bbb-owned.part": true} },
		Logf:   func(f string, a ...any) { log = append(log, f) }}
	r := tdy.Run()

	if r.Parts != 1 || r.Cache != 2 || r.Index != 1 || r.Bytes != 30 {
		t.Fatalf("%+v", r)
	}
	for _, p := range []string{oldPart, goneWim, goneImg} {
		if exists(p) {
			t.Errorf("%s should have been removed", p)
		}
	}
	for _, p := range []string{iso, ownedPart, freshPart, jobs, keptWim, keptImg, stranger} {
		if !exists(p) {
			t.Errorf("%s must be left alone", p)
		}
	}
	if _, ok := idx.Get("keep.iso"); !ok {
		t.Error("the record of a file that is there was dropped")
	}
	if len(log) != 4 {
		t.Errorf("every removal is logged: %v", log)
	}
}

func TestNothingIsDoneWhileTheLibraryIsNotThere(t *testing.T) {
	root := t.TempDir()
	id, _ := library.Prepare(root)
	cache := t.TempDir()
	entry := filepath.Join(cache, "boot-abcdefabcd-0123456789.wim")
	write(t, entry, time.Hour)
	// the folder is gone (a network drive that is not mounted): the library looks empty, which must not make every cache entry look left over
	gone := library.New(filepath.Join(root, "unmounted"), id)
	tdy := &Tidier{Library: gone, Caches: func() []string { return []string{cache} }, ISOIDs: func() (map[string]bool, error) { return map[string]bool{}, nil }}
	if r := tdy.Run(); r != (Report{}) || !exists(entry) {
		t.Fatalf("%+v", r)
	}
	// and an ISO list that cannot be read removes nothing either
	lib := library.New(root, id)
	tdy = &Tidier{Library: lib, Caches: func() []string { return []string{cache} }, ISOIDs: func() (map[string]bool, error) { return nil, os.ErrPermission }}
	if r := tdy.Run(); r.Cache != 0 || !exists(entry) {
		t.Fatalf("%+v", r)
	}
}
