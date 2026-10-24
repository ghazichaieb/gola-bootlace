package initread_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// FuzzInitramfs feeds mutated start-up disks (cpio inside gzip, xz, zstd or bzip2) to the reader that looks inside the ISO's initramfs to find out what the ISO is made of.
// An initramfs comes from an ISO that has not been seen before: the reader must come back with an error, not a panic, and not by reading without limit.
func FuzzInitramfs(f *testing.F) {
	raw := archive()
	f.Add(raw)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(raw)
	zw.Close()
	f.Add(gz.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			return
		}
		start := time.Now()
		_ = initread.Walk(bytes.NewReader(data), func(name string, size int64, body io.Reader) error {
			_, _ = io.CopyN(io.Discard, body, 4096)
			return nil
		})
		_, _ = initread.Read(bytes.NewReader(data), "init", "scripts/casper")
		_, _ = initread.Inspect(bytes.NewReader(data), "casper")
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("took %v for %d bytes", d, len(data))
		}
	})
}

func TestACpioFileNameOfFourGigabytesIsRefusedNotAllocated(t *testing.T) {
	fields := make([]string, 13)
	for i := range fields {
		fields[i] = "00000000"
	}
	fields[11] = "FFFFFFFF" // the length of the file name
	err := initread.Walk(bytes.NewReader([]byte("070701"+strings.Join(fields, ""))), func(string, int64, io.Reader) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("%v", err)
	}
}
