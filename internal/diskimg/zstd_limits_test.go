package diskimg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/klauspost/compress/zstd"
)

func runZstdUnpack(t *testing.T, packed []byte) (error, string, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "disk.img.zst")
	if err := os.WriteFile(src, packed, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Dir: filepath.Join(dir, "cache"), turn: make(chan struct{}, 1)}
	iso := isos.ISO{ID: "abc1234567", Path: src, Name: "disk.img.zst", Size: int64(len(packed)), Packed: "zst", Disk: true}
	dst := m.target(iso)
	j := &job{dst: dst}
	m.unpack(iso, dst, j)
	return j.err, src, dst
}

func TestZstdUnpackRejectsExcessiveWindowAndRemovesPartialOutput(t *testing.T) {
	header := zstd.Header{WindowSize: 256 << 20}
	packed, err := header.AppendTo(nil)
	if err != nil {
		t.Fatal(err)
	}
	// A final empty raw block; the decoder must reject the window before
	// processing output, rather than trusting the tiny compressed size.
	packed = append(packed, 1, 0, 0)
	var parsed zstd.Header
	if err := parsed.Decode(packed); err != nil || parsed.WindowSize != 256<<20 {
		t.Fatalf("invalid test frame: %+v %v", parsed, err)
	}
	err, src, dst := runZstdUnpack(t, packed)
	if !errors.Is(err, zstd.ErrWindowSizeExceeded) {
		t.Fatalf("large window was not rejected: %v", err)
	}
	for _, path := range []string{dst, dst + ".part"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed output remains: %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(src); err != nil || !bytes.Equal(got, packed) {
		t.Fatalf("source changed: %v", err)
	}
}

func TestZstdUnpackStillDecodesOrdinaryImages(t *testing.T) {
	data := bytes.Repeat([]byte("ordinary disk data"), 10000)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	packed := encoder.EncodeAll(data, nil)
	encoder.Close()
	err, _, dst := runZstdUnpack(t, packed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("decoded image differs: %v", err)
	}
}
