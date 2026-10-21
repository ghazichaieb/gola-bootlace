package isos

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanListsIsoFilesSortedAndSkipsOthers(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b-ubuntu.ISO"), 10)
	write(t, filepath.Join(dir, "windows", "Win11.iso"), 20)
	write(t, filepath.Join(dir, "notes.txt"), 5)
	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "b-ubuntu.ISO" || list[1].Name != "windows/Win11.iso" || list[1].Title != "Win11" || list[1].Size != 20 {
		t.Fatalf("unexpected list %+v", list)
	}
}

func TestAMissingFolderIsAnEmptyListAndNamesCannotLeaveTheFolder(t *testing.T) {
	list, err := Scan(filepath.Join(t.TempDir(), "nothing-here"))
	if err != nil || len(list) != 0 {
		t.Fatalf("got %v, %v", list, err)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.iso"), 1)
	if _, ok := Find(dir, "../a.iso"); ok {
		t.Fatal("a name with .. was found")
	}
	if _, ok := Find(dir, "a.iso"); !ok {
		t.Fatal("a.iso was not found")
	}
}
