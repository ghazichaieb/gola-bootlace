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
