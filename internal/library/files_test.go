// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ready is a library in a new folder.
func ready(t *testing.T) (*Library, string) {
	t.Helper()
	dir := t.TempDir()
	id, err := Prepare(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(dir, id), dir
}

func put(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestPathsCannotLeaveTheLibraryOrReachItsOwnFolder(t *testing.T) {
	for _, bad := range []string{"..", "../x", "a/../../x", "a/./b", ".bootlace", ".bootlace/library.json", "a/.hidden", "a:b", "a/b?c", "a\x00b", "/../x"} {
		if got, err := CleanRel(bad); err == nil {
			t.Errorf("CleanRel(%q) = %q, accepted", bad, got)
		}
	}
	for in, want := range map[string]string{"": "", "/": "", "a/b": "a/b", "/a//b/": "a/b", `a\b`: "a/b"} {
		if got, err := CleanRel(in); err != nil || got != want {
			t.Errorf("CleanRel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ".", "..", ".hidden", "a/b", `a\b`, "x:y", "x*", "trailing.", "trailing ", "tab\there", strings.Repeat("a", 201)} {
		if ValidName(bad) == nil {
			t.Errorf("ValidName(%q) accepted", bad)
		}
	}
	for _, ok := range []string{"ubuntu-24.04.iso", "Windows 11 (x64).iso", "ünï.img"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("ValidName(%q): %v", ok, err)
		}
	}
}

func TestListingShowsFoldersFirstAndHidesTheLibrarysOwnFiles(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "b.iso"), "x")
	put(t, filepath.Join(dir, "a.iso"), "xx")
	put(t, filepath.Join(dir, "notes.txt"), "x")
	put(t, filepath.Join(dir, ".hidden.iso"), "x")
	put(t, filepath.Join(dir, "linux", "c.iso"), "x")
	got, err := l.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "linux,a.iso,b.iso,notes.txt" {
		t.Fatalf("listing: %v", names)
	}
	if !got[1].Managed || got[3].Managed || !got[0].Dir || got[1].Size != 2 {
		t.Fatalf("flags: %+v", got)
	}
	sub, err := l.ListDir("linux")
	if err != nil || len(sub) != 1 || sub[0].Path != "linux/c.iso" {
		t.Fatalf("sub: %+v %v", sub, err)
	}
	if _, err := l.ListDir(".bootlace"); err == nil {
		t.Fatal("the library's own folder was listed")
	}
	if _, err := l.ListDir("nowhere"); err != ErrNotFound {
		t.Fatalf("a folder that is not there: %v", err)
	}
}

func TestMakeRenameAndMove(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "a.iso"), "1")
	put(t, filepath.Join(dir, "b.iso"), "2")
	if err := l.Mkdir("linux"); err != nil {
		t.Fatal(err)
	}
	if err := l.Mkdir("linux"); err != ErrExists {
		t.Fatalf("a folder that is there: %v", err)
	}
	if err := l.Mkdir("nowhere/sub"); err == nil {
		t.Fatal("a folder inside a folder that is not there was made")
	}
	if err := l.Mkdir(".secret"); err == nil {
		t.Fatal("a hidden folder was made")
	}
	if err := l.Rename("a.iso", "linux/a.iso"); err != nil { // a move
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "linux", "a.iso")) || exists(filepath.Join(dir, "a.iso")) {
		t.Fatal("not moved")
	}
	if err := l.Rename("b.iso", "linux/a.iso"); err != ErrExists {
		t.Fatalf("a name that is taken must not be replaced: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "linux", "a.iso")); string(b) != "1" {
		t.Fatal("the file that was there was changed")
	}
	if err := l.Rename("b.iso", "b.txt"); err == nil {
		t.Fatal("a file lost the extension that makes it an ISO")
	}
	if err := l.Rename("b.iso", "../b.iso"); err == nil {
		t.Fatal("a file was moved out of the library")
	}
	if err := l.Rename("linux", "linux/inside"); err == nil {
		t.Fatal("a folder was moved into itself")
	}
	if err := l.Rename("linux", "distros"); err != nil || !exists(filepath.Join(dir, "distros", "a.iso")) {
		t.Fatalf("renaming a folder: %v", err)
	}
	if err := l.Rename("", "x"); err == nil {
		t.Fatal("the library was renamed")
	}
}

// Deleting is never recursive and never touches a file that is not an ISO or an image.
func TestDeleteOnlyManagedFilesAndEmptyFolders(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "a.iso"), "1")
	put(t, filepath.Join(dir, "notes.txt"), "keep")
	put(t, filepath.Join(dir, "full", "x.iso"), "1")
	put(t, filepath.Join(dir, "other", "doc.txt"), "keep")
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete("notes.txt"); err != ErrManaged || !exists(filepath.Join(dir, "notes.txt")) {
		t.Fatalf("a file that is not an ISO: %v", err)
	}
	if err := l.Delete("full"); err != ErrNotEmpty || !exists(filepath.Join(dir, "full", "x.iso")) {
		t.Fatalf("a folder with an ISO in it: %v", err)
	}
	if err := l.Delete("other"); err != ErrNotEmpty || !exists(filepath.Join(dir, "other", "doc.txt")) {
		t.Fatalf("a folder with another file in it: %v", err)
	}
	for _, rel := range []string{"a.iso", "empty"} {
		if err := l.Delete(rel); err != nil || exists(filepath.Join(dir, filepath.FromSlash(rel))) {
			t.Fatalf("deleting %s: %v", rel, err)
		}
	}
	for _, rel := range []string{"", ".bootlace", "../x", "missing.iso"} {
		if err := l.Delete(rel); err == nil {
			t.Errorf("Delete(%q) was accepted", rel)
		}
	}
	if !exists(filepath.Join(dir, MetaDir, markerFile)) {
		t.Fatal("the marker was deleted")
	}
}

func TestLinksAreNotFollowedOutOfTheLibrary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need rights that a test does not have here")
	}
	l, dir := ready(t)
	outside := t.TempDir()
	put(t, filepath.Join(outside, "secret.iso"), "private")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.iso"), filepath.Join(dir, "link.iso")); err != nil {
		t.Skip(err)
	}
	if _, err := l.ListDir("escape"); err == nil {
		t.Error("a link out of the library was listed")
	}
	if err := l.Delete("escape/secret.iso"); err == nil || !exists(filepath.Join(outside, "secret.iso")) {
		t.Errorf("a file outside the library was reachable through a link: %v", err)
	}
	if err := l.Delete("link.iso"); err == nil || !exists(filepath.Join(outside, "secret.iso")) {
		t.Errorf("a link to a file outside was followed: %v", err)
	}
	if err := l.Rename("link.iso", "moved.iso"); err == nil {
		t.Error("a link was renamed")
	}
	list, _ := l.ListDir("")
	for _, e := range list {
		if e.Name == "escape" || e.Name == "link.iso" {
			t.Errorf("a link is listed: %+v", e)
		}
	}
}

func TestNothingWorksWithoutTheLibrary(t *testing.T) {
	l, dir := ready(t)
	put(t, filepath.Join(dir, "a.iso"), "1")
	if err := os.RemoveAll(filepath.Join(dir, MetaDir)); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete("a.iso"); err == nil || !exists(filepath.Join(dir, "a.iso")) {
		t.Fatalf("a folder that lost its marker must not be written to or deleted from: %v", err)
	}
	if err := l.Mkdir("x"); err == nil || exists(filepath.Join(dir, "x")) {
		t.Fatalf("Mkdir without the marker: %v", err)
	}
	if _, err := l.NewPart("a.iso"); err == nil {
		t.Fatal("a part file was made in a folder that is not the library")
	}
	if exists(filepath.Join(dir, MetaDir)) {
		t.Fatal("something recreated the library's folder")
	}
}

func TestReceivingAFileAndPlacingIt(t *testing.T) {
	l, dir := ready(t)
	f, err := l.NewPart("../../evil\\name?.iso")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(f.Name()) != filepath.Join(dir, MetaDir, "incoming") || !strings.HasSuffix(f.Name(), ".part") {
		t.Fatalf("part file: %s", f.Name())
	}
	f.WriteString("data")
	f.Close()
	if exists(filepath.Join(dir, "x.iso")) {
		t.Fatal("a partial file is already visible")
	}
	if err := l.Place(f.Name(), "../x.iso"); err == nil {
		t.Fatal("placed outside the library")
	}
	if err := l.Place(f.Name(), "x.txt"); err == nil {
		t.Fatal("placed with a name that is not an ISO's")
	}
	if err := l.Place(f.Name(), "nowhere/x.iso"); err == nil {
		t.Fatal("placed in a folder that is not there")
	}
	if err := l.Place(f.Name(), "x.iso"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "x.iso")); string(b) != "data" || exists(f.Name()) {
		t.Fatal("not moved into place")
	}
	g, _ := l.NewPart("y.iso")
	g.Close()
	if err := l.Place(g.Name(), "x.iso"); err != ErrExists {
		t.Fatalf("a name that is taken: %v", err)
	}
	other := filepath.Join(t.TempDir(), "o.part")
	put(t, other, "z")
	if err := l.Place(other, "z.iso"); err == nil {
		t.Fatal("a file that was not received here was placed")
	}
}

func TestSniff(t *testing.T) {
	iso := make([]byte, 0x8010)
	copy(iso[0x8001:], "CD001")
	udf := make([]byte, 0x8010)
	copy(udf[0x8001:], "BEA01")
	html := []byte("<!DOCTYPE html><html><body>Download failed</body></html>" + strings.Repeat(" ", 0x9000))
	cases := []struct {
		name string
		head []byte
		want string // "" ok, "short", or text that must be in the error
	}{
		{"a.iso", iso, ""}, {"a.ISO", udf, ""}, {"a.iso", html, "not an ISO"}, {"a.iso", iso[:100], "short"},
		{"a.iso.xz", []byte("\xfd7zXZ\x00abc"), ""}, {"a.iso.xz", html, "not a .xz"}, {"a.iso.gz", []byte("\x1f\x8bxxxxxxx"), ""},
		{"a.img.zst", []byte("\x28\xb5\x2f\xfdxxxx"), ""}, {"a.img.bz2", []byte("BZh9xxxx"), ""}, {"a.zip", []byte("PK\x03\x04xxxx"), ""}, {"a.zip", html, "not a .zip"},
		{"a.img", append([]byte{0xeb, 0x3c, 0x90}, make([]byte, 600)...), ""}, {"a.img", html, "text"}, {"a.raw", []byte(strings.Repeat("hello world\n", 100)), "text"},
		{"a.img", []byte("abc"), "short"}, {"a.txt", []byte("abc"), "keeps a name"},
	}
	for _, c := range cases {
		err := Sniff(c.name, c.head)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want == "short" && err != ErrTooShort:
			t.Errorf("%s: %v, want ErrTooShort", c.name, err)
		case c.want != "" && c.want != "short" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	if NeedBytes("x.iso") < 0x8006 || NeedBytes("x.img") != 512 {
		t.Error("NeedBytes")
	}
}

func TestReserveAndSpace(t *testing.T) {
	if Reserve(100<<30) != 1<<30 || Reserve(1<<30) != 512<<20 || Reserve(10<<40) != 5<<30 {
		t.Errorf("reserve: %d %d %d", Reserve(100<<30), Reserve(1<<30), Reserve(10<<40))
	}
	l, _ := ready(t)
	if err := l.EnoughSpace(1); err != nil {
		t.Skipf("this disk has no room to test with: %v", err)
	}
	if err := l.EnoughSpace(1 << 62); err == nil || !strings.Contains(err.Error(), "not enough room") {
		t.Fatalf("an absurd size fitted: %v", err)
	}
}
