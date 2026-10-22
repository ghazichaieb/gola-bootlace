package partition

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"unicode/utf16"
)

func putMBR(img []byte, slot int, typ byte, start, count uint32) {
	e := img[446+16*slot:]
	e[4] = typ
	binary.LittleEndian.PutUint32(e[8:], start)
	binary.LittleEndian.PutUint32(e[12:], count)
	img[510], img[511] = 0x55, 0xAA
}

func TestMBRWithLogicalPartitions(t *testing.T) {
	img := make([]byte, 100*sector)
	putMBR(img, 0, 0x0C, 2, 10)
	putMBR(img, 1, 0x05, 20, 60) // extended, from sector 20
	// first boot record of the extended partition: one logical partition at +2, and a link to the next record at +30
	ebr := img[20*sector:]
	putMBR(ebr, 0, 0x83, 2, 8)
	putMBR(ebr, 1, 0x05, 30, 30)
	ebr2 := img[(20+30)*sector:]
	putMBR(ebr2, 0, 0x83, 2, 6)
	list, err := Read(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("%d partitions: %+v", len(list), list)
	}
	if p := list[0]; p.Number != 1 || p.Start != 2*sector || p.Size != 10*sector || p.MBR != 0x0C {
		t.Errorf("first: %+v", p)
	}
	if p := list[1]; p.Number != 5 || p.Start != 22*sector || p.Size != 8*sector {
		t.Errorf("second: %+v", p)
	}
	if p := list[2]; p.Number != 6 || p.Start != 52*sector || p.Size != 6*sector {
		t.Errorf("third: %+v", p)
	}
}

func TestGPT(t *testing.T) {
	img := make([]byte, 200*sector)
	putMBR(img, 0, 0xEE, 1, 199) // the protective record
	h := img[sector:]
	copy(h, "EFI PART")
	binary.LittleEndian.PutUint64(h[72:], 2)
	binary.LittleEndian.PutUint32(h[80:], 4)
	binary.LittleEndian.PutUint32(h[84:], 128)
	efi := []byte{0x28, 0x73, 0x2a, 0xc1, 0x1f, 0xf8, 0xd2, 0x11, 0xba, 0x4b, 0x00, 0xa0, 0xc9, 0x3e, 0xc9, 0x3b}
	e := img[2*sector:]
	copy(e, efi)
	binary.LittleEndian.PutUint64(e[32:], 34)
	binary.LittleEndian.PutUint64(e[40:], 99)
	for i, c := range utf16.Encode([]rune("système")) {
		binary.LittleEndian.PutUint16(e[56+2*i:], c)
	}
	e2 := img[2*sector+128*2:] // the third slot; the second is empty
	copy(e2, efi)
	e2[0] = 0xa2
	binary.LittleEndian.PutUint64(e2[32:], 100)
	binary.LittleEndian.PutUint64(e2[40:], 150)
	list, err := Read(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("%d partitions: %+v", len(list), list)
	}
	if p := list[0]; p.Number != 1 || p.Start != 34*sector || p.Size != 66*sector || p.GUID != GUIDEFISystem || p.Name != "système" {
		t.Errorf("first: %+v", p)
	}
	if p := list[1]; p.Number != 3 || p.GUID != "c12a73a2-f81f-11d2-ba4b-00a0c93ec93b" {
		t.Errorf("second: %+v", p)
	}
}

func TestSuperfloppy(t *testing.T) {
	img := make([]byte, 64*sector)
	img[0], img[1], img[2] = 0xEB, 0x3C, 0x90
	binary.LittleEndian.PutUint16(img[11:], sector)
	img[13], img[16] = 1, 2
	copy(img[54:], "FAT16   ")
	img[510], img[511] = 0x55, 0xAA
	list, err := Read(bytes.NewReader(img), int64(len(img)))
	if err != nil || len(list) != 1 || list[0].Start != 0 || list[0].Size != int64(len(img)) {
		t.Fatalf("%+v, %v", list, err)
	}
	if _, err := Read(bytes.NewReader(make([]byte, 4096)), 4096); err == nil {
		t.Error("an empty disk has a partition table")
	}
}

// a real disk image, when one is at hand: BOOTLACE_TEST_DISK=<path> (and optionally BOOTLACE_TEST_PARTS=<number of partitions expected>)
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
	list, err := Read(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		t.Logf("%+v", p)
	}
}
