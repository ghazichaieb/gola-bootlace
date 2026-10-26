package diskimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ulikunitz/xz"
)

// xzHeaders builds the start of an XZ file: the stream header and a first block header whose LZMA2 filter declares the dictionary size code d. Nothing follows, which is
// enough: the size check must refuse the file before any data is decoded.
func xzHeaders(d byte) []byte {
	flags := []byte{0x00, 0x01}
	out := []byte{0xFD, '7', 'z', 'X', 'Z', 0x00}
	out = append(out, flags...)
	var crc [4]byte
	binary.LittleEndian.PutUint32(crc[:], crc32.ChecksumIEEE(flags))
	out = append(out, crc[:]...)
	block := []byte{0x02, 0x00, 0x21, 0x01, d, 0, 0, 0} // header size 12, one filter, no sizes, LZMA2, one property byte, padding
	binary.LittleEndian.PutUint32(crc[:], crc32.ChecksumIEEE(block))
	out = append(out, block...)
	return append(out, crc[:]...)
}

func runXZUnpack(t *testing.T, packed []byte) (error, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "disk.img.xz")
	if err := os.WriteFile(src, packed, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Dir: filepath.Join(dir, "cache"), turn: make(chan struct{}, 1)}
	iso := isos.ISO{ID: "abc1234568", Path: src, Name: "disk.img.xz", Size: int64(len(packed)), Packed: "xz", Disk: true}
	dst := m.target(iso)
	j := &job{dst: dst}
	m.unpack(iso, dst, j)
	return j.err, dst
}

func TestXZUnpackRefusesAHugeDictionaryBeforeDecodingAndLeavesNoOutput(t *testing.T) {
	err, dst := runXZUnpack(t, xzHeaders(32)) // 2 << 27 = 256 MiB
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("256 MiB")) {
		t.Fatalf("a 256 MiB dictionary was not refused with a clear message: %v", err)
	}
	for _, path := range []string{dst, dst + ".part"} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("output remains: %s: %v", path, statErr)
		}
	}
}

func TestXZDictionaryLimitIsTheSameAsZstdsAndAllowsTheUsualSizes(t *testing.T) {
	if err := checkXZDictionary(xzHeaders(28)); err != nil { // 64 MiB, what xz -9 writes
		t.Errorf("64 MiB refused: %v", err)
	}
	if err := checkXZDictionary(xzHeaders(30)); err != nil { // 128 MiB: the limit itself is accepted
		t.Errorf("128 MiB refused: %v", err)
	}
	if err := checkXZDictionary(xzHeaders(31)); err == nil { // 192 MiB
		t.Error("192 MiB accepted")
	}
	if err := checkXZDictionary(xzHeaders(40)); err == nil { // the largest code: 4 GiB - 1
		t.Error("4 GiB accepted")
	}
}

func TestXZCheckLeavesOtherBytesToTheDecoder(t *testing.T) {
	for _, head := range [][]byte{nil, []byte("not an xz file at all"), xzHeaders(28)[:14]} {
		if err := checkXZDictionary(head); err != nil && !errors.Is(err, errXZNotChecked) {
			t.Errorf("%q: unexpected refusal %v", head, err)
		}
	}
}

func TestXZUnpackStillDecodesOrdinaryImages(t *testing.T) {
	data := bytes.Repeat([]byte("ordinary disk data"), 10000)
	var packed bytes.Buffer
	w, err := xz.NewWriter(&packed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	err, dst := runXZUnpack(t, packed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("decoded image differs: %v", err)
	}
}
