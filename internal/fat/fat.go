// Package fat reads the files of a FAT12, FAT16 or FAT32 file system (with long file names) without mounting it. Disk images of live systems keep their kernel and
// start-up files on one, and the EFI system partition inside an ISO is another. Only reading is supported; the image itself is never changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package fat

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// Entry is what a folder lists.
type Entry struct {
	Name string
	Size int64
	Dir  bool
}

// Volume is an opened FAT file system.
type Volume struct {
	r             io.ReaderAt
	bytesPerSec   int64
	secPerCluster int64
	clusterBytes  int64
	fatStart      int64 // byte offset of the first FAT
	rootStart     int64 // FAT12/16: byte offset of the fixed root folder
	rootBytes     int64
	dataStart     int64 // byte offset of cluster 2
	clusters      uint32
	kind          int // 12, 16 or 32
	rootCluster   uint32
	fat           []byte
	Label         string
	mu            sync.Mutex
	dirs          map[uint32][]node // folder cache by first cluster (0: the fixed root)
}

type node struct {
	name    string
	dir     bool
	size    int64
	cluster uint32
}

// maxFAT is the largest allocation table that is read into memory (a FAT32 of 2 TB with 32 KB clusters has 16 MB of it).
const maxFAT = 64 << 20

// maxFolder is the largest folder that is read.
const maxFolder = 64 << 20

// Open reads the boot sector and the allocation table of a FAT volume.
func Open(r io.ReaderAt) (*Volume, error) {
	b := make([]byte, 512)
	if _, err := r.ReadAt(b, 0); err != nil {
		return nil, err
	}
	if b[510] != 0x55 || b[511] != 0xAA {
		return nil, errors.New("fat: no boot sector signature")
	}
	v := &Volume{r: r, bytesPerSec: int64(binary.LittleEndian.Uint16(b[11:])), secPerCluster: int64(b[13])}
	if v.bytesPerSec < 512 || v.bytesPerSec > 4096 || v.bytesPerSec&(v.bytesPerSec-1) != 0 || v.secPerCluster == 0 || v.secPerCluster&(v.secPerCluster-1) != 0 {
		return nil, errors.New("fat: not a FAT boot sector")
	}
	reserved, nfats := int64(binary.LittleEndian.Uint16(b[14:])), int64(b[16])
	rootEntries := int64(binary.LittleEndian.Uint16(b[17:]))
	total := int64(binary.LittleEndian.Uint16(b[19:]))
	if total == 0 {
		total = int64(binary.LittleEndian.Uint32(b[32:]))
	}
	fatSize := int64(binary.LittleEndian.Uint16(b[22:]))
	if fatSize == 0 {
		fatSize = int64(binary.LittleEndian.Uint32(b[36:]))
	}
	if reserved == 0 || nfats == 0 || fatSize == 0 || total == 0 {
		return nil, errors.New("fat: not a FAT boot sector")
	}
	if bytes.Equal(b[3:11], []byte("EXFAT   ")) {
		return nil, errors.New("fat: exFAT is not supported")
	}
	v.clusterBytes = v.bytesPerSec * v.secPerCluster
	v.fatStart = reserved * v.bytesPerSec
	v.rootBytes = rootEntries * 32
	v.rootStart = v.fatStart + nfats*fatSize*v.bytesPerSec
	rootSectors := (v.rootBytes + v.bytesPerSec - 1) / v.bytesPerSec
	v.dataStart = v.rootStart + rootSectors*v.bytesPerSec
	dataSectors := total - reserved - nfats*fatSize - rootSectors
	if dataSectors <= 0 {
		return nil, errors.New("fat: not a FAT boot sector")
	}
	v.clusters = uint32(dataSectors / v.secPerCluster)
	switch {
	case rootEntries == 0:
		v.kind = 32
		v.rootCluster = binary.LittleEndian.Uint32(b[44:])
		v.Label = strings.TrimSpace(string(b[71:82]))
	case v.clusters < 4085:
		v.kind = 12
		v.Label = strings.TrimSpace(string(b[43:54]))
	default:
		v.kind = 16
		v.Label = strings.TrimSpace(string(b[43:54]))
	}
	if fatSize*v.bytesPerSec > maxFAT {
		return nil, errors.New("fat: the allocation table is too large")
	}
	v.fat = make([]byte, fatSize*v.bytesPerSec)
	if _, err := r.ReadAt(v.fat, v.fatStart); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	v.dirs = map[uint32][]node{}
	return v, nil
}

// next is the cluster after c in its chain; end is true at the end of the chain (or a damaged one).
func (v *Volume) next(c uint32) (n uint32, end bool) {
	switch v.kind {
	case 12:
		off := int(c) + int(c)/2
		if off+1 >= len(v.fat) {
			return 0, true
		}
		w := uint32(v.fat[off]) | uint32(v.fat[off+1])<<8
		if c&1 == 1 {
			w >>= 4
		} else {
			w &= 0xFFF
		}
		return w, w >= 0xFF8 || w < 2
	case 16:
		if int(c)*2+1 >= len(v.fat) {
			return 0, true
		}
		w := uint32(binary.LittleEndian.Uint16(v.fat[c*2:]))
		return w, w >= 0xFFF8 || w < 2
	default:
		if int(c)*4+3 >= len(v.fat) {
			return 0, true
		}
		w := binary.LittleEndian.Uint32(v.fat[c*4:]) & 0x0FFFFFFF
		return w, w >= 0x0FFFFFF8 || w < 2
	}
}

// run is a stretch of clusters that follow each other on the disk.
type run struct {
	logical int64 // offset in the file
	disk    int64 // offset in the volume
	length  int64
}

// chain follows the clusters of a file or folder and joins neighbours into runs. limit is how many bytes are needed at most (0: until the end of the chain).
func (v *Volume) chain(first uint32, limit int64) ([]run, error) {
	var runs []run
	if first < 2 {
		return nil, nil
	}
	c, pos := first, int64(0)
	seen := 0
	for {
		if c < 2 || c-2 >= v.clusters {
			return nil, errors.New("fat: a cluster is outside the volume")
		}
		disk := v.dataStart + int64(c-2)*v.clusterBytes
		if n := len(runs); n > 0 && runs[n-1].disk+runs[n-1].length == disk {
			runs[n-1].length += v.clusterBytes
		} else {
			runs = append(runs, run{logical: pos, disk: disk, length: v.clusterBytes})
		}
		pos += v.clusterBytes
		if limit > 0 && pos >= limit {
			return runs, nil
		}
		nx, end := v.next(c)
		if end {
			return runs, nil
		}
		if seen++; uint32(seen) > v.clusters {
			return nil, errors.New("fat: the cluster chain loops")
		}
		c = nx
	}
}

// readChain reads a whole folder.
func (v *Volume) readChain(first uint32) ([]byte, error) {
	runs, err := v.chain(first, 0)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, r := range runs {
		if r.length < 0 || r.length > maxFolder || int64(len(out))+r.length > maxFolder {
			return nil, errors.New("fat: a folder that is too big")
		}
		buf := make([]byte, r.length)
		if _, err := v.r.ReadAt(buf, r.disk); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		out = append(out, buf...)
	}
	return out, nil
}

// folder lists a folder by its first cluster (0 for the fixed root of FAT12/16).
func (v *Volume) folder(cluster uint32) ([]node, error) {
	v.mu.Lock()
	if l, ok := v.dirs[cluster]; ok {
		v.mu.Unlock()
		return l, nil
	}
	v.mu.Unlock()
	var raw []byte
	if cluster == 0 && v.kind != 32 {
		raw = make([]byte, v.rootBytes)
		if _, err := v.r.ReadAt(raw, v.rootStart); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	} else {
		var err error
		if raw, err = v.readChain(cluster); err != nil {
			return nil, err
		}
	}
	list := parseFolder(raw)
	v.mu.Lock()
	v.dirs[cluster] = list
	v.mu.Unlock()
	return list, nil
}

func parseFolder(raw []byte) []node {
	var out []node
	var long []uint16
	var seq, sum byte
	for i := 0; i+32 <= len(raw); i += 32 {
		e := raw[i : i+32]
		if e[0] == 0 {
			break
		}
		if e[0] == 0xE5 {
			long = nil
			continue
		}
		attr := e[11]
		if attr&0x3F == 0x0F { // a piece of a long name; the last piece comes first
			n := e[0]
			if n&0x40 != 0 {
				long = make([]uint16, 0, 26)
				seq, sum = n&0x3F, e[13]
				long = append(long, make([]uint16, int(seq)*13)...)
			}
			idx := int(n&0x3F) - 1
			if long == nil || idx < 0 || idx*13+13 > len(long) {
				long = nil
				continue
			}
			put := func(off, count, to int) {
				for k := 0; k < count; k++ {
					long[idx*13+to+k] = binary.LittleEndian.Uint16(e[off+2*k:])
				}
			}
			put(1, 5, 0)
			put(14, 6, 5)
			put(28, 2, 11)
			continue
		}
		if attr&0x08 != 0 { // the volume label
			long = nil
			continue
		}
		short := shortName(e)
		name := short
		if long != nil && checksum(e[:11]) == sum {
			u := long
			for k, c := range u {
				if c == 0 || c == 0xFFFF {
					u = u[:k]
					break
				}
			}
			name = string(utf16.Decode(u))
		}
		long = nil
		if name == "." || name == ".." {
			continue
		}
		cl := uint32(binary.LittleEndian.Uint16(e[20:]))<<16 | uint32(binary.LittleEndian.Uint16(e[26:]))
		out = append(out, node{name: name, dir: attr&0x10 != 0, size: int64(binary.LittleEndian.Uint32(e[28:])), cluster: cl})
	}
	return out
}

func checksum(name []byte) byte {
	var s byte
	for _, c := range name {
		s = (s>>1 | s<<7) + c
	}
	return s
}

// shortName is the 8.3 name of a folder entry, in the case the entry says (Linux's and Windows's lower case flags).
func shortName(e []byte) string {
	base, ext := strings.TrimRight(string(e[:8]), " "), strings.TrimRight(string(e[8:11]), " ")
	if e[0] == 0x05 {
		base = "\xE5" + base[1:]
	}
	if e[12]&0x08 != 0 {
		base = strings.ToLower(base)
	}
	if e[12]&0x10 != 0 {
		ext = strings.ToLower(ext)
	}
	if ext == "" {
		return base
	}
	return base + "." + ext
}

// lookup finds a path (case does not matter) and returns its entry. The root has the cluster 0 on FAT12/16 and the root cluster on FAT32.
func (v *Volume) lookup(name string) (node, error) {
	root := node{dir: true}
	if v.kind == 32 {
		root.cluster = v.rootCluster
	}
	cur := root
	for _, part := range strings.Split(strings.Trim(name, "/"), "/") {
		if part == "" {
			continue
		}
		if !cur.dir {
			return node{}, fs.ErrNotExist
		}
		list, err := v.folder(cur.cluster)
		if err != nil {
			return node{}, err
		}
		found := false
		for _, n := range list {
			if strings.EqualFold(n.name, part) {
				cur, found = n, true
				break
			}
		}
		if !found {
			return node{}, fs.ErrNotExist
		}
		if cur.dir && cur.cluster == 0 && v.kind == 32 { // ".." style entries that point at the root
			cur.cluster = v.rootCluster
		}
	}
	return cur, nil
}

// Exists says whether the path is there.
func (v *Volume) Exists(name string) bool {
	_, err := v.lookup(name)
	return err == nil
}

// Stat describes one path.
func (v *Volume) Stat(name string) (Entry, error) {
	n, err := v.lookup(name)
	if err != nil {
		return Entry{}, err
	}
	base := name[strings.LastIndex(name, "/")+1:]
	if n.name != "" {
		base = n.name
	}
	return Entry{Name: base, Size: n.size, Dir: n.dir}, nil
}

// ReadDir lists a folder, folders first and then files, each in name order.
func (v *Volume) ReadDir(name string) ([]Entry, error) {
	n, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	if !n.dir {
		return nil, errors.New("fat: not a folder")
	}
	list, err := v.folder(n.cluster)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(list))
	for _, e := range list {
		out = append(out, Entry{Name: e.name, Size: e.size, Dir: e.dir})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// List names the contents of a folder, with a slash after each folder.
func (v *Volume) List(name string) ([]string, error) {
	es, err := v.ReadDir(name)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
		if e.Dir {
			out[i] += "/"
		}
	}
	return out, nil
}

// File is an open file of the volume.
type File struct {
	v    *Volume
	size int64
	runs []run
}

// Open opens a file for reading.
func (v *Volume) Open(name string) (*File, error) {
	n, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	if n.dir {
		return nil, fmt.Errorf("fat: %s is a folder", name)
	}
	f := &File{v: v, size: n.size}
	if n.size > 0 {
		if f.runs, err = v.chain(n.cluster, n.size); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// Size is the length of the file.
func (f *File) Size() int64 { return f.size }

// ReadAt reads from a position of the file.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("fat: negative offset")
	}
	if off >= f.size {
		return 0, io.EOF
	}
	want := int64(len(p))
	eof := false
	if off+want > f.size {
		want, eof = f.size-off, true
	}
	done := int64(0)
	// the first run that holds the position
	i := sort.Search(len(f.runs), func(k int) bool { return f.runs[k].logical+f.runs[k].length > off })
	for done < want && i < len(f.runs) {
		r := f.runs[i]
		within := off + done - r.logical
		n := r.length - within
		if n > want-done {
			n = want - done
		}
		got, err := f.v.r.ReadAt(p[done:done+n], r.disk+within)
		done += int64(got)
		if err != nil && !errors.Is(err, io.EOF) {
			return int(done), err
		}
		if int64(got) < n {
			return int(done), io.ErrUnexpectedEOF
		}
		i++
	}
	if done < want {
		return int(done), io.ErrUnexpectedEOF
	}
	if eof {
		return int(done), io.EOF
	}
	return int(done), nil
}

// Reader is the file as a seekable reader.
func (f *File) Reader() io.ReadSeeker { return io.NewSectionReader(f, 0, f.size) }
