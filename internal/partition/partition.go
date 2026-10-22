// Package partition reads the partition table of a disk image (MBR with extended partitions, or GPT) without mounting it, so that the file systems inside can be read
// (see packages fat and disc). A disk image without a table that starts with a file system straight away (a "superfloppy") is reported as one partition.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package partition

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

const sector = 512

// Partition is one partition of a disk image.
type Partition struct {
	Number int    // 1-based, as Linux numbers them (logical partitions of an MBR start at 5)
	Start  int64  // byte offset in the image
	Size   int64  // in bytes
	MBR    byte   // the MBR partition type (0xEE for a GPT disk's protective entry is never listed); 0 for GPT partitions
	GUID   string // the GPT partition type (lower case), empty for MBR partitions
	Name   string // the GPT partition name
}

// GPT partition types that matter here.
const (
	GUIDEFISystem = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
	GUIDBasicData = "ebd0a0a2-b9e5-4433-87c0-68b6b72699c7"
)

// Section returns the partition as a reader of its own.
func (p Partition) Section(r io.ReaderAt) *io.SectionReader {
	return io.NewSectionReader(r, p.Start, p.Size)
}

// Read lists the partitions of the image. An image with neither a table nor a file system at its start gives an error.
func Read(r io.ReaderAt, size int64) ([]Partition, error) {
	head := make([]byte, 2*sector)
	if _, err := r.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	mbr := head[:sector]
	if mbr[510] == 0x55 && mbr[511] == 0xAA && !looksLikeFAT(mbr) {
		if mbr[446+4] == 0xEE || bytes.Equal(head[sector:sector+8], []byte("EFI PART")) {
			if list, err := readGPT(r, size, head[sector:]); err == nil && len(list) > 0 {
				return list, nil
			}
		}
		if list := readMBR(r, size, mbr); len(list) > 0 {
			return list, nil
		}
	}
	if looksLikeFAT(mbr) {
		return []Partition{{Number: 1, Start: 0, Size: size}}, nil
	}
	return nil, errors.New("no partition table")
}

// looksLikeFAT says whether a boot sector is a FAT file system's rather than a master boot record (both end with 0x55 0xAA).
func looksLikeFAT(b []byte) bool {
	if b[0] != 0xEB && b[0] != 0xE9 {
		return false
	}
	if binary.LittleEndian.Uint16(b[11:]) == 0 || b[13] == 0 || b[16] == 0 {
		return false
	}
	return bytes.Equal(b[54:57], []byte("FAT")) || bytes.Equal(b[82:85], []byte("FAT"))
}

func readMBR(r io.ReaderAt, size int64, mbr []byte) []Partition {
	var out []Partition
	var extended int64
	for i := 0; i < 4; i++ {
		e := mbr[446+16*i : 446+16*(i+1)]
		typ, start, count := e[4], int64(binary.LittleEndian.Uint32(e[8:])), int64(binary.LittleEndian.Uint32(e[12:]))
		if typ == 0 || count == 0 {
			continue
		}
		if typ == 0x05 || typ == 0x0F || typ == 0x85 {
			if extended == 0 {
				extended = start
			}
			continue
		}
		if p := (Partition{Number: i + 1, Start: start * sector, Size: count * sector, MBR: typ}); p.Start+p.Size <= size {
			out = append(out, p)
		}
	}
	if extended != 0 { // logical partitions: a chain of boot records, each with one partition and a link to the next
		next := int64(0)
		for n := 5; n < 5+60; n++ {
			b := make([]byte, sector)
			if _, err := r.ReadAt(b, (extended+next)*sector); err != nil || b[510] != 0x55 || b[511] != 0xAA {
				break
			}
			e := b[446 : 446+16]
			typ, start, count := e[4], int64(binary.LittleEndian.Uint32(e[8:])), int64(binary.LittleEndian.Uint32(e[12:]))
			if typ != 0 && count != 0 {
				if p := (Partition{Number: n, Start: (extended + next + start) * sector, Size: count * sector, MBR: typ}); p.Start+p.Size <= size {
					out = append(out, p)
				}
			}
			link := b[446+16 : 446+32]
			if link[4] == 0 {
				break
			}
			next = int64(binary.LittleEndian.Uint32(link[8:]))
		}
	}
	return out
}

func readGPT(r io.ReaderAt, size int64, hdr []byte) ([]Partition, error) {
	if !bytes.Equal(hdr[:8], []byte("EFI PART")) {
		return nil, errors.New("no GPT header")
	}
	entriesLBA := int64(binary.LittleEndian.Uint64(hdr[72:]))
	count := int(binary.LittleEndian.Uint32(hdr[80:]))
	entrySize := int(binary.LittleEndian.Uint32(hdr[84:]))
	if entrySize < 128 || entrySize > 4096 || count < 1 || count > 4096 {
		return nil, fmt.Errorf("odd GPT header (%d entries of %d bytes)", count, entrySize)
	}
	table := make([]byte, count*entrySize)
	if _, err := r.ReadAt(table, entriesLBA*sector); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var out []Partition
	for i := 0; i < count; i++ {
		e := table[i*entrySize : (i+1)*entrySize]
		if allZero(e[:16]) {
			continue
		}
		first, last := int64(binary.LittleEndian.Uint64(e[32:])), int64(binary.LittleEndian.Uint64(e[40:]))
		if last < first {
			continue
		}
		p := Partition{Number: i + 1, Start: first * sector, Size: (last - first + 1) * sector, GUID: guid(e[:16]), Name: name(e[56:128])}
		if p.Start+p.Size > size {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// guid writes a GPT GUID (mixed endian: the first three groups are little endian) the usual way.
func guid(b []byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x", binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint16(b[4:]), binary.LittleEndian.Uint16(b[6:]), b[8:10], b[10:16])
}

func name(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}
