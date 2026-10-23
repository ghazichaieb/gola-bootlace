// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func old(t *testing.T, path string) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
}

func TestRecordsAreOnlyTrueWhileTheFileIsTheSame(t *testing.T) {
	l, dir := ready(t)
	x := OpenIndex(l)
	p := filepath.Join(dir, "a.iso")
	put(t, p, "one")
	if err := x.Set("a.iso", Record{SHA256: sum("one"), Source: "https://example.org/a.iso"}); err != nil {
		t.Fatal(err)
	}
	if r, ok := x.Get("a.iso"); !ok || r.SHA256 != sum("one") {
		t.Fatalf("%+v %v", r, ok)
	}
	put(t, p, "changed!") // a new file under the same name
	if _, ok := x.Get("a.iso"); ok {
		t.Fatal("a record of an older file is still believed")
	}
	// kept between runs
	put(t, p, "one")
	if err := x.Set("a.iso", Record{SHA256: sum("one")}); err != nil {
		t.Fatal(err)
	}
	if r, ok := OpenIndex(l).Get("a.iso"); !ok || r.SHA256 != sum("one") {
		t.Fatal("the index was not kept in the library")
	}
}

func TestDuplicates(t *testing.T) {
	l, dir := ready(t)
	x := OpenIndex(l)
	put(t, filepath.Join(dir, "ubuntu.iso"), "content-A")
	put(t, filepath.Join(dir, "sub", "other.iso"), "content-B")
	_ = x.Set("ubuntu.iso", Record{SHA256: sum("content-A"), Source: "https://example.org/ubuntu.iso"})
	_ = x.Set("sub/other.iso", Record{SHA256: sum("content-B")})
	reasons := func(m []Match) string {
		s := ""
		for _, e := range m {
			s += e.Path + ":" + e.Reason + " "
		}
		return s
	}
	if got := x.Duplicates("ubuntu.iso", "", 0, ""); reasons(got) != "ubuntu.iso:name " {
		t.Errorf("name: %s", reasons(got))
	}
	if got := x.Duplicates("new.iso", "https://example.org/ubuntu.iso", 0, ""); reasons(got) != "ubuntu.iso:source " {
		t.Errorf("source: %s", reasons(got))
	}
	if got := x.Duplicates("new.iso", "", 0, sum("content-B")); reasons(got) != "sub/other.iso:content " {
		t.Errorf("content: %s", reasons(got))
	}
	if got := x.Duplicates("folder/ubuntu.iso", "", int64(len("content-A")), ""); reasons(got) != "ubuntu.iso:size " {
		t.Errorf("size and name: %s", reasons(got))
	}
	if got := x.Duplicates("other.iso", "", 5, sum("nothing")); len(got) != 0 {
		t.Errorf("a new file is a duplicate: %s", reasons(got))
	}
	// a record that no longer fits its file finds nothing
	put(t, filepath.Join(dir, "ubuntu.iso"), "content-A, but longer")
	if got := x.Duplicates("new.iso", "", 0, sum("content-A")); len(got) != 0 {
		t.Errorf("an old record matched: %s", reasons(got))
	}
}

func TestMovingAndForgetting(t *testing.T) {
	l, dir := ready(t)
	x := OpenIndex(l)
	put(t, filepath.Join(dir, "a", "x.iso"), "x")
	_ = x.Set("a/x.iso", Record{SHA256: "h"})
	if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	x.Move("a", "b")
	if _, ok := x.Get("b/x.iso"); !ok {
		t.Fatal("the record did not follow the folder")
	}
	if _, ok := x.Get("a/x.iso"); ok {
		t.Fatal("the old path still has a record")
	}
	x.Forget("b/x.iso")
	if _, ok := x.Get("b/x.iso"); ok {
		t.Fatal("not forgotten")
	}
}

func TestHashingFindsFilesCopiedInByHandAndLeavesNewOnesAlone(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "old.iso"), "settled")
	old(t, filepath.Join(dir, "old.iso"))
	put(t, filepath.Join(dir, "sub", "old2.img"), "settled 2")
	old(t, filepath.Join(dir, "sub", "old2.img"))
	put(t, filepath.Join(dir, "copying.iso"), "still being copied") // just written: not stable yet
	put(t, filepath.Join(dir, "notes.txt"), "no")
	x := OpenIndex(l)
	x.HashPending(make(chan struct{}))
	for rel, want := range map[string]string{"old.iso": sum("settled"), "sub/old2.img": sum("settled 2")} {
		if r, ok := x.Get(rel); !ok || r.SHA256 != want {
			t.Errorf("%s: %+v %v", rel, r, ok)
		}
	}
	if _, ok := x.Get("copying.iso"); ok {
		t.Error("a file that is still being copied was hashed")
	}
	if _, ok := x.Get("notes.txt"); ok {
		t.Error("a file that is not an ISO was hashed")
	}
	if st := x.Status(); st.Running || st.Waiting != 0 {
		t.Errorf("status after the work: %+v", st)
	}
}

func TestHashingWaitsWhileInstallsRun(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "a.iso"), "data")
	old(t, filepath.Join(dir, "a.iso"))
	x := OpenIndex(l)
	var busy atomic.Bool
	busy.Store(true)
	x.Idle = func() bool { return !busy.Load() }
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { x.HashPending(stop); close(done) }()
	time.Sleep(300 * time.Millisecond)
	if _, ok := x.Get("a.iso"); ok {
		t.Fatal("hashed while PCs were using the server")
	}
	if !x.Status().Paused {
		t.Errorf("not shown as paused: %+v", x.Status())
	}
	busy.Store(false)
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("did not carry on when the server was quiet")
	}
	if _, ok := x.Get("a.iso"); !ok {
		t.Fatal("not hashed after the PCs were gone")
	}
}

func TestIndexIsNotWrittenWhereThereIsNoLibrary(t *testing.T) {
	l, dir := ready(t)
	x := OpenIndex(l)
	put(t, filepath.Join(dir, "a.iso"), "x")
	if err := os.RemoveAll(filepath.Join(dir, MetaDir)); err != nil {
		t.Fatal(err)
	}
	if err := x.Set("a.iso", Record{}); err == nil {
		t.Fatal("an index was written to a folder that lost its marker")
	}
	if exists(filepath.Join(dir, MetaDir)) {
		t.Fatal("the library's folder was made again")
	}
}
