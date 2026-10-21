package disc

import (
	"bytes"
	"math/rand"
	"os"
	"testing"
)

// TestAgainstAMountedCopy compares what the reader serves with the same file of a copy that Windows has mounted. Set BOOTLACE_VERIFY_ISO and
// BOOTLACE_VERIFY_FILE (for example F:\sources\install.wim) and BOOTLACE_VERIFY_PATH (/sources/install.wim); without them the test is skipped.
func TestAgainstAMountedCopy(t *testing.T) {
	isoPath, ref, in := os.Getenv("BOOTLACE_VERIFY_ISO"), os.Getenv("BOOTLACE_VERIFY_FILE"), os.Getenv("BOOTLACE_VERIFY_PATH")
	if isoPath == "" || ref == "" || in == "" {
		t.Skip("needs BOOTLACE_VERIFY_ISO, BOOTLACE_VERIFY_FILE and BOOTLACE_VERIFY_PATH")
	}
	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	fsys, err := Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	got, err := fsys.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer want.Close()
	ws, _ := want.Stat()
	t.Logf("size %d", ws.Size())
	buf1, buf2 := make([]byte, 1<<20), make([]byte, 1<<20)
	bad := 0
	check := func(off int64, n int) {
		a, _ := got.ReadAt(buf1[:n], off)
		b, _ := want.ReadAt(buf2[:n], off)
		if !bytes.Equal(buf1[:a], buf2[:b]) || a != b {
			bad++
			if bad < 10 {
				t.Errorf("differs at %d (+%d), got %d bytes, want %d", off, n, a, b)
			}
		}
	}
	for off := int64(0); off < ws.Size(); off += 64 << 20 { // a megabyte of every 64
		check(off, 1<<20)
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 400; i++ {
		check(r.Int63n(ws.Size()-4096), 4096)
	}
	check(ws.Size()-4096, 4096)
}
