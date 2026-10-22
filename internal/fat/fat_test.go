package fat

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
)

// image is a small FAT volume written by the test itself (so that the reader is tested against something it did not make up on its own).
type image struct {
	kind        int // 16 or 32
	secPerClust int
	data        []byte
	fat         []int // next cluster of each cluster
	nextFree    int
}

const testSector = 512

func newImage(kind, secPerClust, clusters int) *image {
	im := &image{kind: kind, secPerClust: secPerClust, fat: make([]int, clusters+2), nextFree: 3} // cluster 2 is the FAT32 root
	return im
}

type tfile struct {
	name string
	data []byte
	dir  []tfile
	isd  bool
}

func lfnEntries(name string, short [11]byte) [][]byte {
	u := utf16.Encode([]rune(name))
	u = append(u, 0)
	for len(u)%13 != 0 {
		u = append(u, 0xFFFF)
	}
	n := len(u) / 13
	var sum byte
	for _, c := range short {
		sum = (sum>>1 | sum<<7) + c
	}
	var out [][]byte
	for i := n; i >= 1; i-- {
		e := make([]byte, 32)
		e[0] = byte(i)
		if i == n {
			e[0] |= 0x40
		}
		e[11], e[13] = 0x0F, sum
		part := u[(i-1)*13 : i*13]
		for k := 0; k < 5; k++ {
			binary.LittleEndian.PutUint16(e[1+2*k:], part[k])
		}
		for k := 0; k < 6; k++ {
			binary.LittleEndian.PutUint16(e[14+2*k:], part[5+k])
		}
		for k := 0; k < 2; k++ {
			binary.LittleEndian.PutUint16(e[28+2*k:], part[11+k])
		}
		out = append(out, e)
	}
	return out
}

// alloc writes data into clusters (scattered on purpose when scatter is set) and returns the first cluster.
func (im *image) alloc(data []byte, scatter bool, rng *rand.Rand) int {
	cb := im.secPerClust * testSector
	n := (len(data) + cb - 1) / cb
	if n == 0 {
		return 0
	}
	var list []int
	for i := 0; i < n; i++ {
		c := im.nextFree
		im.nextFree++
		list = append(list, c)
	}
	if scatter {
		rng.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	}
	for i, c := range list {
		lo := i * cb
		hi := lo + cb
		if hi > len(data) {
			hi = len(data)
		}
		copy(im.cluster(c), data[lo:hi])
		if i+1 < len(list) {
			im.fat[c] = list[i+1]
		} else {
			im.fat[c] = -1
		}
	}
	return list[0]
}

func (im *image) cluster(c int) []byte {
	cb := im.secPerClust * testSector
	return im.data[im.dataStart()+(c-2)*cb : im.dataStart()+(c-1)*cb]
}

func (im *image) fatSectors() int {
	per := testSector / 2
	if im.kind == 32 {
		per = testSector / 4
	}
	return (len(im.fat) + per - 1) / per
}

func (im *image) reserved() int {
	if im.kind == 32 {
		return 32
	}
	return 1
}

func (im *image) rootSectors() int {
	if im.kind == 32 {
		return 0
	}
	return 32 // 512 entries
}

func (im *image) dataStart() int {
	return (im.reserved() + 2*im.fatSectors() + im.rootSectors()) * testSector
}

// folderBytes writes the entries of a folder.
func (im *image) folderBytes(files []tfile, rng *rand.Rand) []byte {
	var out []byte
	for i, f := range files {
		short := [11]byte{}
		for k := range short {
			short[k] = ' '
		}
		copy(short[:], fmt.Sprintf("FILE%04d", i))
		copy(short[8:], "BIN")
		var content []byte
		attr := byte(0x20)
		if f.isd {
			attr = 0x10
			content = im.folderBytes(f.dir, rng)
		} else {
			content = f.data
		}
		first := im.alloc(content, len(content) > 3*testSector*im.secPerClust, rng)
		for _, e := range lfnEntries(f.name, short) {
			out = append(out, e...)
		}
		e := make([]byte, 32)
		copy(e, short[:])
		e[11] = attr
		binary.LittleEndian.PutUint16(e[20:], uint16(first>>16))
		binary.LittleEndian.PutUint16(e[26:], uint16(first))
		if !f.isd {
			binary.LittleEndian.PutUint32(e[28:], uint32(len(f.data)))
		}
		out = append(out, e...)
	}
	return out
}

func (im *image) build(files []tfile, rng *rand.Rand) []byte {
	clusters := len(im.fat) - 2
	total := im.reserved() + 2*im.fatSectors() + im.rootSectors() + clusters*im.secPerClust
	im.data = make([]byte, total*testSector)
	root := im.folderBytes(files, rng)
	b := im.data[:testSector]
	b[0], b[1], b[2] = 0xEB, 0x3C, 0x90
	copy(b[3:], "MSDOS5.0")
	binary.LittleEndian.PutUint16(b[11:], testSector)
	b[13] = byte(im.secPerClust)
	binary.LittleEndian.PutUint16(b[14:], uint16(im.reserved()))
	b[16] = 2
	if im.kind == 16 {
		binary.LittleEndian.PutUint16(b[17:], 512)
	}
	if total < 65536 && im.kind == 16 {
		binary.LittleEndian.PutUint16(b[19:], uint16(total))
	} else {
		binary.LittleEndian.PutUint32(b[32:], uint32(total))
	}
	b[21] = 0xF8
	if im.kind == 16 {
		binary.LittleEndian.PutUint16(b[22:], uint16(im.fatSectors()))
		copy(b[43:], "TESTVOL    ")
		copy(b[54:], "FAT16   ")
	} else {
		binary.LittleEndian.PutUint32(b[36:], uint32(im.fatSectors()))
		binary.LittleEndian.PutUint32(b[44:], 2)
		copy(b[71:], "TESTVOL    ")
		copy(b[82:], "FAT32   ")
	}
	b[510], b[511] = 0x55, 0xAA
	if im.kind == 16 {
		copy(im.data[(im.reserved()+2*im.fatSectors())*testSector:], root)
	} else {
		im.fat[2] = -1
		copy(im.cluster(2), root)
		if len(root) > len(im.cluster(2)) {
			panic("test root folder too big")
		}
	}
	// the allocation tables
	table := make([]byte, im.fatSectors()*testSector)
	for c, nx := range im.fat {
		v := uint32(nx)
		if nx == -1 {
			v = 0xFFFFFFFF
		}
		switch {
		case c == 0:
			v = 0xFFFFFFF8
		case c == 1:
			v = 0xFFFFFFFF
		}
		if im.kind == 16 {
			binary.LittleEndian.PutUint16(table[c*2:], uint16(v))
		} else {
			binary.LittleEndian.PutUint32(table[c*4:], v&0x0FFFFFFF)
		}
	}
	copy(im.data[im.reserved()*testSector:], table)
	copy(im.data[(im.reserved()+im.fatSectors())*testSector:], table)
	return im.data
}

func sample(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	rng.Read(b)
	return b
}

func testFiles(rng *rand.Rand) ([]tfile, map[string][]byte) {
	want := map[string][]byte{}
	mk := func(path string, n int) tfile {
		d := sample(rng, n)
		want[path] = d
		return tfile{name: path[strings.LastIndex(path, "/")+1:], data: d}
	}
	files := []tfile{
		mk("/KERNEL", 3000),
		mk("/a very long file name with spaces and ünïcode.squashfs", 200*1024),
		mk("/empty.txt", 0),
		{name: "live", isd: true, dir: []tfile{mk("/live/vmlinuz", 70000), mk("/live/initrd.img", 33*1024+17), mk("/live/Tails.module", 20)}},
		{name: "EFI", isd: true, dir: []tfile{{name: "BOOT", isd: true, dir: []tfile{mk("/EFI/BOOT/BOOTX64.EFI", 5000)}}}},
	}
	return files, want
}

func TestReadVolumes(t *testing.T) {
	for _, tc := range []struct {
		kind, spc, clusters int
	}{
		{16, 4, 5000},
		{32, 1, 70000},
		{32, 8, 70000},
	} {
		t.Run(fmt.Sprintf("FAT%d/%d", tc.kind, tc.spc), func(t *testing.T) {
			rng := rand.New(rand.NewSource(7))
			files, want := testFiles(rng)
			data := newImage(tc.kind, tc.spc, tc.clusters).build(files, rng)
			v, err := Open(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if v.kind != tc.kind {
				t.Fatalf("kind %d, want %d", v.kind, tc.kind)
			}
			if v.Label != "TESTVOL" {
				t.Errorf("label %q", v.Label)
			}
			names, err := v.List("/")
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(names)
			if got := strings.Join(names, "|"); got != "EFI/|KERNEL|a very long file name with spaces and ünïcode.squashfs|empty.txt|live/" {
				t.Errorf("root lists %q", got)
			}
			for path, d := range want {
				if !v.Exists(path) || !v.Exists(strings.ToLower(path)) {
					t.Fatalf("%s is not found", path)
				}
				f, err := v.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if f.Size() != int64(len(d)) {
					t.Fatalf("%s: size %d, want %d", path, f.Size(), len(d))
				}
				got, err := io.ReadAll(f.Reader())
				if err != nil || !bytes.Equal(got, d) {
					t.Fatalf("%s: contents differ (%v)", path, err)
				}
				// reads from the middle, over cluster borders, and past the end
				if len(d) > 2000 {
					buf := make([]byte, 1500)
					if _, err := f.ReadAt(buf, 1000); err != nil || !bytes.Equal(buf, d[1000:2500]) {
						t.Fatalf("%s: ReadAt in the middle failed (%v)", path, err)
					}
					tail := make([]byte, 100)
					n, err := f.ReadAt(tail, int64(len(d)-50))
					if n != 50 || err != io.EOF || !bytes.Equal(tail[:50], d[len(d)-50:]) {
						t.Fatalf("%s: ReadAt at the end gave %d, %v", path, n, err)
					}
				}
			}
			if _, err := v.Open("/live"); err == nil {
				t.Error("a folder opened as a file")
			}
			if v.Exists("/nothing/here") {
				t.Error("a missing path exists")
			}
			es, err := v.ReadDir("/live")
			if err != nil || len(es) != 3 || es[0].Name != "initrd.img" || es[0].Size != 33*1024+17 {
				t.Errorf("ReadDir(/live) = %v, %v", es, err)
			}
			if e, err := v.Stat("/EFI"); err != nil || !e.Dir || e.Name != "EFI" {
				t.Errorf("Stat(/EFI) = %v, %v", e, err)
			}
		})
	}
}

func TestNotFAT(t *testing.T) {
	if _, err := Open(bytes.NewReader(make([]byte, 4096))); err == nil {
		t.Error("zeros opened as FAT")
	}
	b := make([]byte, 4096)
	copy(b[3:], "EXFAT   ")
	b[510], b[511] = 0x55, 0xAA
	if _, err := Open(bytes.NewReader(b)); err == nil {
		t.Error("exFAT opened as FAT")
	}
}
