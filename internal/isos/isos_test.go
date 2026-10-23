package isos

import (
	"archive/zip"
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

// The library's own folder (partial files, the cache inside the library) and hidden files are never offered.
func TestScanSkipsHiddenFoldersAndFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.iso"), 1)
	write(t, filepath.Join(dir, ".bootlace", "cache", "images", "b.img"), 1)
	write(t, filepath.Join(dir, ".bootlace", "incoming", "c.iso"), 1)
	write(t, filepath.Join(dir, "sub", "._a.iso"), 1)
	list, err := Scan(dir)
	if err != nil || len(list) != 1 || list[0].Name != "a.iso" {
		t.Fatalf("got %+v, %v", list, err)
	}
}

func TestNoFolderIsNotTheCurrentFolder(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "x.iso", 1)
	if list, err := Scan(""); err != nil || len(list) != 0 {
		t.Fatalf("Scan(\"\") listed the current folder: %v, %v", list, err)
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

func TestDiskImagesAreListedWithTheirCompression(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "batocera-x86_64.img.gz"), 10)
	write(t, filepath.Join(dir, "chimera.IMG.xz"), 10)
	write(t, filepath.Join(dir, "plain.raw"), 10)
	write(t, filepath.Join(dir, "other.tar.gz"), 10)
	write(t, filepath.Join(dir, "disc.iso"), 10)
	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ISO{}
	for _, i := range list {
		got[i.Name] = i
	}
	if len(list) != 4 || got["other.tar.gz"].Name != "" {
		t.Fatalf("%+v", list)
	}
	if b := got["batocera-x86_64.img.gz"]; !b.Disk || b.Packed != "gz" || b.Title != "batocera-x86_64" {
		t.Fatalf("%+v", b)
	}
	if c := got["chimera.IMG.xz"]; !c.Disk || c.Packed != "xz" || c.Title != "chimera" {
		t.Fatalf("%+v", c)
	}
	if p := got["plain.raw"]; !p.Disk || p.Packed != "" || p.Title != "plain" {
		t.Fatalf("%+v", p)
	}
	if d := got["disc.iso"]; d.Disk {
		t.Fatalf("%+v", d)
	}
}

func writeZip(t *testing.T, path string, files map[string]int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, n := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(make([]byte, n))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPackedIsosAndZipArchivesAreListedWithWhatIsInside(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "OPNsense-26.7-dvd-amd64.iso.bz2"), 10)
	writeZip(t, filepath.Join(dir, "FD14-LiveCD.zip"), map[string]int{"readme.txt": 5, "FD14LIVE.iso": 5000})
	writeZip(t, filepath.Join(dir, "supergrub2-USB.img.zip"), map[string]int{"supergrub2-USB.img": 3000})
	writeZip(t, filepath.Join(dir, "docs.zip"), map[string]int{"a.txt": 5})
	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ISO{}
	for _, i := range list {
		got[i.Name] = i
	}
	if len(list) != 3 {
		t.Fatalf("%d entries: %+v", len(list), list)
	}
	if i := got["OPNsense-26.7-dvd-amd64.iso.bz2"]; i.Disk || i.Packed != "bz2" || i.Title != "OPNsense-26.7-dvd-amd64" {
		t.Errorf("packed ISO: %+v", i)
	}
	if i := got["FD14-LiveCD.zip"]; i.Disk || i.Packed != "zip" || i.Inner != "FD14LIVE.iso" || i.Title != "FD14-LiveCD" {
		t.Errorf("zip with an ISO: %+v", i)
	}
	if i := got["supergrub2-USB.img.zip"]; !i.Disk || i.Packed != "zip" || i.Inner != "supergrub2-USB.img" || i.Title != "supergrub2-USB" {
		t.Errorf("zip with an image: %+v", i)
	}
}
