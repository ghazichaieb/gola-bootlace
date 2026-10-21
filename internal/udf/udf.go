// Package udf reads the files inside a UDF image without mounting it. Windows installation ISOs keep their real contents in UDF (the ISO 9660 side
// of the same disc only holds a stub), so the server needs this to hand out boot.wim, install.wim and the rest. Only reading is supported, of the
// plain layout Windows media use (a UDF 1.02 to 2.60 volume with ordinary partition maps, and the metadata partition of 2.50 and later); the image
// itself is never changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package udf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode/utf16"
)

const sector = 2048

// Tag identifiers of the descriptors read here (ECMA-167).
const (
	tagAVDP     = 2
	tagPD       = 5
	tagLVD      = 6
	tagTD       = 8
	tagFSD      = 256
	tagFID      = 257
	tagFE       = 261
	tagEFE      = 266
	tagPartInfo = 5
)

// Image is an opened UDF volume.
type Image struct {
	r          io.ReaderAt
	blockSize  int64
	partStart  map[uint16]int64 // partition reference number -> first block (in blocks of the volume)
	partMeta   map[uint16]*metadata
	root       *node
	rootICB    longAD
	partitions []uint16 // reference number of each partition map entry, in map order
}

type metadata struct {
	mainFile  uint32 // location (in the physical partition) of the metadata file entry
	physical  uint16 // reference number of the physical partition holding it
	extents   []extent
	extentsOK bool
}

type extent struct {
	logical int64 // offset within the file
	start   int64 // byte offset in the image (0 when `zero`)
	length  int64
	zero    bool // allocated but never written: reads as zeros
}

type longAD struct {
	length uint32
	block  uint32
	part   uint16
}

type node struct {
	img     *Image
	name    string
	dir     bool
	size    int64
	extents []extent
	loaded  bool
	kids    []*node
	embed   []byte // data stored in the file entry itself (small files)
}

// Open reads the volume structures of a UDF image.
func Open(r io.ReaderAt, size int64) (*Image, error) {
	img := &Image{r: r, blockSize: sector, partStart: map[uint16]int64{}, partMeta: map[uint16]*metadata{}}
	var avdp [sector]byte
	found := false
	for _, at := range []int64{256 * sector, size/sector*sector - 257*sector, size/sector*sector - sector} {
		if at < 0 || at+sector > size {
			continue
		}
		if _, err := r.ReadAt(avdp[:], at); err == nil && tagOf(avdp[:]) == tagAVDP {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("udf: no UDF anchor on this image")
	}
	vdsLen := int64(binary.LittleEndian.Uint32(avdp[16:]))
	vdsLoc := int64(binary.LittleEndian.Uint32(avdp[20:]))
	var fsd longAD
	var maps []byte
	var mapCount int
	buf := make([]byte, sector)
	for off := int64(0); off < vdsLen; off += sector {
		if _, err := r.ReadAt(buf, (vdsLoc)*sector+off); err != nil {
			return nil, err
		}
		switch tagOf(buf) {
		case tagPD:
			num := binary.LittleEndian.Uint16(buf[22:])
			img.partStart[num] = int64(binary.LittleEndian.Uint32(buf[188:]))
		case tagLVD:
			img.blockSize = int64(binary.LittleEndian.Uint32(buf[212:]))
			fsd = readLongAD(buf[248:])
			n := int(binary.LittleEndian.Uint32(buf[264:]))
			mapCount = int(binary.LittleEndian.Uint32(buf[268:]))
			if 440+n <= len(buf) {
				maps = append([]byte(nil), buf[440:440+n]...)
			}
		case tagTD:
			off = vdsLen
		}
	}
	if img.blockSize != sector {
		return nil, fmt.Errorf("udf: block size %d is not supported", img.blockSize)
	}
	if len(img.partStart) == 0 || mapCount == 0 {
		return nil, errors.New("udf: no partition found")
	}
	// partition maps: type 1 = a physical partition, type 2 = something special (the metadata partition names a physical one and a file)
	for i, p := 0, 0; i < mapCount && p+2 <= len(maps); i++ {
		kind, l := maps[p], int(maps[p+1])
		ref := uint16(i)
		switch kind {
		case 1:
			if p+6 <= len(maps) {
				phys := binary.LittleEndian.Uint16(maps[p+4:])
				img.partStart[ref] = img.partStart[phys]
			}
		case 2:
			if p+40 <= len(maps) && strings.Contains(string(maps[p+5:p+28]), "*UDF Metadata Partition") {
				phys := binary.LittleEndian.Uint16(maps[p+38:])
				img.partMeta[ref] = &metadata{mainFile: binary.LittleEndian.Uint32(maps[p+40:]), physical: phys}
			}
		}
		img.partitions = append(img.partitions, ref)
		p += l
	}
	// the metadata partition is a file in the physical partition: its extents say where the "partition" really is
	for ref, m := range img.partMeta {
		fe, err := img.readICB(longAD{block: m.mainFile, part: m.physical})
		if err != nil {
			return nil, fmt.Errorf("udf: metadata partition: %w", err)
		}
		m.extents, m.extentsOK = fe.extents, true
		img.partMeta[ref] = m
	}
	buf2, err := img.readBlocks(fsd, sector)
	if err != nil || tagOf(buf2) != tagFSD {
		return nil, errors.New("udf: no file set descriptor")
	}
	img.rootICB = readLongAD(buf2[400:])
	root, err := img.readICB(img.rootICB)
	if err != nil {
		return nil, fmt.Errorf("udf: root folder: %w", err)
	}
	root.name, root.dir = ".", true
	img.root = root
	return img, nil
}

func tagOf(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

func readLongAD(b []byte) longAD {
	return longAD{length: binary.LittleEndian.Uint32(b) & 0x3fffffff, block: binary.LittleEndian.Uint32(b[4:]), part: binary.LittleEndian.Uint16(b[8:])}
}

// byteOffset is where block `block` of partition `part` is in the image file.
func (img *Image) byteOffset(part uint16, block uint32) (int64, error) {
	if m, ok := img.partMeta[part]; ok {
		return mapExtents(m.extents, int64(block)*sector)
	}
	base, ok := img.partStart[part]
	if !ok {
		return 0, fmt.Errorf("udf: unknown partition %d", part)
	}
	return (base + int64(block)) * sector, nil
}

// mapExtents turns an offset inside a file made of extents into an offset in the image.
func mapExtents(ext []extent, off int64) (int64, error) {
	for _, e := range ext {
		if off >= e.logical && off < e.logical+e.length {
			return e.start + (off - e.logical), nil
		}
	}
	return 0, errors.New("udf: block outside the file")
}

func (img *Image) readBlocks(ad longAD, n int64) ([]byte, error) {
	off, err := img.byteOffset(ad.part, ad.block)
	if err != nil {
		return nil, err
	}
	b := make([]byte, n)
	if _, err := img.r.ReadAt(b, off); err != nil && err != io.EOF {
		return nil, err
	}
	return b, nil
}

// readICB reads the file entry an ICB points to and decodes its data location.
func (img *Image) readICB(ad longAD) (*node, error) {
	b, err := img.readBlocks(ad, sector)
	if err != nil {
		return nil, err
	}
	tag := tagOf(b)
	if tag != tagFE && tag != tagEFE {
		return nil, fmt.Errorf("udf: expected a file entry, found descriptor %d", tag)
	}
	n := &node{img: img, dir: b[27] == 4} // ICB tag file type 4 = folder
	n.size = int64(binary.LittleEndian.Uint64(b[56:]))
	flags := binary.LittleEndian.Uint16(b[34:]) & 7
	var lenEA, lenAD uint32
	var adStart int
	if tag == tagFE {
		lenEA, lenAD, adStart = binary.LittleEndian.Uint32(b[168:]), binary.LittleEndian.Uint32(b[172:]), 176
	} else {
		lenEA, lenAD, adStart = binary.LittleEndian.Uint32(b[208:]), binary.LittleEndian.Uint32(b[212:]), 216
	}
	start := adStart + int(lenEA)
	if start+int(lenAD) > len(b) {
		return nil, errors.New("udf: file entry too long")
	}
	area := b[start : start+int(lenAD)]
	switch flags {
	case 3: // the data is in the file entry
		n.embed = append([]byte(nil), area...)
	case 0, 1: // short or long allocation descriptors
		pos := int64(0)
		for len(area) >= 8 {
			var length uint32
			var block uint32
			part := ad.part
			if flags == 0 {
				length, block = binary.LittleEndian.Uint32(area), binary.LittleEndian.Uint32(area[4:])
				area = area[8:]
			} else {
				if len(area) < 16 {
					break
				}
				length, block, part = binary.LittleEndian.Uint32(area), binary.LittleEndian.Uint32(area[4:]), binary.LittleEndian.Uint16(area[8:])
				area = area[16:]
			}
			kind, l := length>>30, int64(length&0x3fffffff)
			if l == 0 {
				break
			}
			e := extent{logical: pos, length: l}
			switch kind {
			case 0: // recorded
				off, err := img.byteOffset(part, block)
				if err != nil {
					return nil, err
				}
				e.start = off
			case 1, 2: // allocated, not recorded: zeros
				e.zero = true
			case 3:
				return nil, errors.New("udf: files with a continuation of descriptors are not supported")
			}
			n.extents = append(n.extents, e)
			pos += l
		}
	default:
		return nil, fmt.Errorf("udf: allocation type %d is not supported", flags)
	}
	return n, nil
}

// load reads a folder's entries.
func (n *node) load() error {
	if n.loaded || !n.dir {
		return nil
	}
	data := make([]byte, n.size)
	if _, err := n.readAt(data, 0); err != nil && err != io.EOF {
		return err
	}
	for i := 0; i+38 <= len(data); {
		if tagOf(data[i:]) != tagFID {
			break
		}
		chars := data[i+18]
		lenFI, lenIU := int(data[i+19]), int(binary.LittleEndian.Uint16(data[i+36:]))
		icb := readLongAD(data[i+20:])
		idStart := i + 38 + lenIU
		total := (38 + lenIU + lenFI + 3) &^ 3
		if idStart+lenFI > len(data) {
			break
		}
		name := decodeName(data[idStart : idStart+lenFI])
		i += total
		if chars&0x08 != 0 || chars&0x04 != 0 || name == "" { // parent folder, or deleted
			continue
		}
		kid, err := n.img.readICB(icb)
		if err != nil {
			continue
		}
		kid.name, kid.dir = name, chars&0x02 != 0 || kid.dir
		n.kids = append(n.kids, kid)
	}
	sort.Slice(n.kids, func(a, b int) bool { return n.kids[a].name < n.kids[b].name })
	n.loaded = true
	return nil
}

// decodeName reads a name stored as OSTA compressed unicode: one byte that says 8 or 16 bits per character, then the characters.
func decodeName(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	switch b[0] {
	case 8:
		return string(runesLatin1(b[1:]))
	case 16:
		u := make([]uint16, 0, len(b)/2)
		for i := 1; i+1 < len(b); i += 2 {
			u = append(u, binary.BigEndian.Uint16(b[i:]))
		}
		return string(utf16.Decode(u))
	}
	return ""
}

func runesLatin1(b []byte) []rune {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return r
}

// readAt reads from the data of a file or folder, across its pieces.
func (n *node) readAt(p []byte, off int64) (int, error) {
	if n.embed != nil {
		if off >= int64(len(n.embed)) {
			return 0, io.EOF
		}
		c := copy(p, n.embed[off:])
		if c < len(p) {
			return c, io.EOF
		}
		return c, nil
	}
	if off >= n.size {
		return 0, io.EOF
	}
	read := 0
	for _, e := range n.extents {
		if off >= e.logical+e.length || read == len(p) {
			continue
		}
		in := off - e.logical
		want := int64(len(p) - read)
		if want > e.length-in {
			want = e.length - in
		}
		if want > n.size-off {
			want = n.size - off
		}
		if e.zero {
			for i := int64(0); i < want; i++ {
				p[read+int(i)] = 0
			}
		} else if c, err := n.img.r.ReadAt(p[read:read+int(want)], e.start+in); err != nil && err != io.EOF {
			return read + c, err
		}
		read += int(want)
		off += want
	}
	if read < len(p) {
		return read, io.EOF
	}
	return read, nil
}

func (img *Image) find(name string) (*node, error) {
	cur := img.root
	for _, part := range strings.Split(path.Clean("/"+name), "/") {
		if part == "" {
			continue
		}
		if err := cur.load(); err != nil {
			return nil, err
		}
		var hit *node
		for _, k := range cur.kids {
			if k.name == part {
				hit = k
				break
			}
		}
		if hit == nil {
			for _, k := range cur.kids {
				if strings.EqualFold(k.name, part) {
					hit = k
					break
				}
			}
		}
		if hit == nil {
			return nil, fs.ErrNotExist
		}
		cur = hit
	}
	return cur, nil
}

// File is an open file of the image.
type File struct{ n *node }

// Open returns the file at name (forward slashes).
func (img *Image) Open(name string) (*File, error) {
	n, err := img.find(name)
	if err != nil {
		return nil, err
	}
	if n.dir {
		return nil, errors.New("udf: that is a folder")
	}
	return &File{n: n}, nil
}

// Exists reports whether a file or folder is in the image.
func (img *Image) Exists(name string) bool {
	_, err := img.find(name)
	return err == nil
}

// List returns the names in a folder, sorted; folders end with a slash.
func (img *Image) List(name string) ([]string, error) {
	n, err := img.find(name)
	if err != nil {
		return nil, err
	}
	if !n.dir {
		return nil, errors.New("udf: that is a file")
	}
	if err := n.load(); err != nil {
		return nil, err
	}
	names := make([]string, len(n.kids))
	for i, k := range n.kids {
		names[i] = k.name
		if k.dir {
			names[i] += "/"
		}
	}
	return names, nil
}

// Size is the length of the file.
func (f *File) Size() int64 { return f.n.size }

// ReadAt reads from the file at an offset.
func (f *File) ReadAt(p []byte, off int64) (int, error) { return f.n.readAt(p, off) }

// Reader makes the file an io.ReadSeeker for http.ServeContent.
func (f *File) Reader() io.ReadSeeker { return io.NewSectionReader(f, 0, f.n.size) }
