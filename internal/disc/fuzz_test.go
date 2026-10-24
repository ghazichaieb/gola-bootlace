package disc

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// walk reads what a hostile image offers the way the server does (a listing, the file sizes, the first bytes of a few files) and gives up after a short while: the readers
// must come back with an error, never panic and never run for ever or read a huge amount into memory.
func walk(t testing.TB, fsys FS, name string, depth int, budget *int) {
	if depth > 4 || *budget <= 0 {
		return
	}
	*budget--
	entries, err := fsys.ReadDir(name)
	if err != nil {
		return
	}
	for i, e := range entries {
		if i >= 40 {
			break
		}
		path := name + "/" + e.Name
		if e.Dir {
			walk(t, fsys, path, depth+1, budget)
			continue
		}
		if f, err := fsys.Open(path); err == nil {
			buf := make([]byte, 4096)
			_, _ = f.ReadAt(buf, 0)
			if f.Size() > 8192 {
				_, _ = f.ReadAt(buf, f.Size()-4096)
			}
		}
		_, _ = fsys.Stat(path)
		_ = fsys.Exists(path)
	}
}

func seeds(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "seed-*.gz"))
	for _, name := range files {
		raw, err := os.Open(name)
		if err != nil {
			continue
		}
		zr, err := gzip.NewReader(raw)
		if err == nil {
			if b, err := io.ReadAll(zr); err == nil {
				f.Add(b)
			}
		}
		raw.Close()
	}
	f.Add(make([]byte, 0x9000+2048))
}

// FuzzOpen feeds mutated images to the readers of ISO 9660, UDF and FAT and to the partition-table reader.
func FuzzOpen(f *testing.F) {
	seeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4<<20 {
			return
		}
		start := time.Now()
		r := bytes.NewReader(data)
		if fsys, err := Open(r, int64(len(data))); err == nil {
			budget := 200
			walk(t, fsys, "", 0, &budget)
		}
		if fsys, err := OpenFAT(bytes.NewReader(data)); err == nil {
			budget := 200
			walk(t, fsys, "", 0, &budget)
		}
		_, _ = OpenDisk(bytes.NewReader(data), int64(len(data)))
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("took %v for %d bytes", d, len(data))
		}
	})
}

// TestTheSeedImagesAreReadSoThatTheFuzzerStartsFromSomethingReal makes sure the seeds are what they say: a real UDF image and a real ISO 9660 image (made with genisoimage and
// xorriso from a small tree) whose files can be listed and read.
func TestTheSeedImagesAreReadSoThatTheFuzzerStartsFromSomethingReal(t *testing.T) {
	for name, kind := range map[string]string{"seed-iso.gz": "ISO 9660", "seed-udf.gz": "UDF"} {
		raw, err := os.Open(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		zr, err := gzip.NewReader(raw)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(zr)
		raw.Close()
		if err != nil {
			t.Fatal(err)
		}
		fsys, err := Open(bytes.NewReader(data), int64(len(data)))
		if err != nil || fsys.Kind() != kind {
			t.Fatalf("%s: %v %v", name, kind, err)
		}
		f, err := fsys.Open("/boot/grub/grub.cfg")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		buf := make([]byte, f.Size())
		if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if !bytes.Contains(buf, []byte("menuentry")) {
			t.Fatalf("%s: %q", name, buf)
		}
	}
}

// TestAFolderThatSaysItIsFourGigabytesIsRefusedQuickly is the first thing the fuzzer found: the root folder of a 98 KB image said it was 2 GB long, and the reader made a
// buffer that size (seconds of time and the memory of the computer) before it noticed that the image ends much earlier.
func TestAFolderThatSaysItIsFourGigabytesIsRefusedQuickly(t *testing.T) {
	raw, err := os.Open(filepath.Join("testdata", "seed-iso.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	zr, _ := gzip.NewReader(raw)
	data, _ := io.ReadAll(zr)
	const pvd = 16 * 2048
	root := pvd + 156 // the root folder's record in the volume descriptor: its length is at offset 10 (little-endian, then big-endian at 14)
	copy(data[root+10:root+14], []byte{0xff, 0xff, 0xff, 0x7f})
	copy(data[root+14:root+18], []byte{0x7f, 0xff, 0xff, 0xff})
	start := time.Now()
	fsys, err := Open(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		_, err = fsys.ReadDir("/")
	}
	if err == nil {
		t.Fatal("a folder that is 2 GB long in a 98 KB image must be refused")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("it took %v to refuse", d)
	}
}
