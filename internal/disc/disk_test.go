package disc

import (
	"os"
	"testing"
)

// a real disk image, when one is at hand: BOOTLACE_TEST_DISK=<path>
func TestRealDisk(t *testing.T) {
	path := os.Getenv("BOOTLACE_TEST_DISK")
	if path == "" {
		t.Skip("BOOTLACE_TEST_DISK is not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	parts, err := OpenDisk(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		names, _ := p.FS.List("/")
		t.Logf("partition %d (%s) at %d, %d bytes: %v", p.Number, p.FS.Kind(), p.Start, p.Size, names)
	}
}
