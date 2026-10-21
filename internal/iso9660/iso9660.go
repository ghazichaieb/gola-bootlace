// Package iso9660 reads the files inside an ISO image without mounting it: the server hands a PC the kernel, the start-up disk and the packages
// that a Linux ISO carries, straight out of the ISO file. Rock Ridge names (the long, lower case names Linux ISOs use) are understood; the
// image itself is never changed.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package iso9660

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

const sector = 2048

// Image is an opened ISO.
type Image struct {
	r    io.ReaderAt
	root *node
}

type extent struct {
	start  int64 // byte offset in the image
	length int64
}

type node struct {
	img     *Image
	name    string
	dir     bool
	size    int64
	extents []extent // a file is one or more pieces (files over 4 GiB are stored in several)
	loaded  bool
	kids    []*node
}

// Open reads the volume descriptors of an ISO and returns it. size is the length of the file.
func Open(r io.ReaderAt, size int64) (*Image, error) {
	var pvd [sector]byte
	for block := int64(16); block < 32; block++ { // the primary volume descriptor is one of the first few
		if _, err := r.ReadAt(pvd[:], block*sector); err != nil {
			return nil, fmt.Errorf("iso9660: cannot read the volume descriptors: %w", err)
		}
		if string(pvd[1:6]) != "CD001" {
			return nil, errors.New("iso9660: this is not an ISO 9660 image")
		}
		if pvd[0] == 255 {
			break
		}
		if pvd[0] != 1 {
			continue
		}
		img := &Image{r: r}
		root, err := img.record(pvd[156:190], true)
		if err != nil || root == nil {
			return nil, errors.New("iso9660: the root folder is unreadable")
		}
		root.dir, root.name = true, "."
		img.root = root
		return img, nil
	}
	return nil, errors.New("iso9660: no primary volume descriptor")
}

// record decodes one directory record. skipDot makes it ignore the "." and ".." entries (returns nil for them).
func (img *Image) record(b []byte, isRoot bool) (*node, error) {
	if len(b) < 34 || int(b[0]) > len(b) {
		return nil, errors.New("short directory record")
	}
	n := &node{img: img}
	start := int64(binary.LittleEndian.Uint32(b[2:])) * sector
	length := int64(binary.LittleEndian.Uint32(b[10:]))
	n.dir = b[25]&2 != 0
	n.size = length
	n.extents = []extent{{start, length}}
	idLen := int(b[32])
	if 33+idLen > len(b) {
		return nil, errors.New("bad directory record name")
	}
	id := b[33 : 33+idLen]
	if !isRoot && idLen == 1 && (id[0] == 0 || id[0] == 1) {
		return nil, nil // "." and ".."
	}
	n.name = isoName(id)
	suspStart := 33 + idLen
	if idLen%2 == 0 {
		suspStart++
	}
	if suspStart < int(b[0]) {
		if rr := img.rockRidgeName(b[suspStart:int(b[0])]); rr != "" {
			n.name = rr
		}
	}
	return n, nil
}

// isoName turns the plain ISO 9660 name (UPPER.CASE;1) into something readable when there is no Rock Ridge name.
func isoName(id []byte) string {
	s := string(id)
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(s, ".")
}

// rockRidgeName finds the NM (alternate name) entries in a system use area, following continuation areas (CE).
func (img *Image) rockRidgeName(area []byte) string {
	var name strings.Builder
	for depth := 0; depth < 8; depth++ {
		var next []byte
		for i := 0; i+4 <= len(area); {
			l := int(area[i+2])
			if l < 4 || i+l > len(area) {
				break
			}
			switch string(area[i : i+2]) {
			case "NM":
				if l > 5 && area[i+4]&6 == 0 { // flags: bit 1 "current folder", bit 2 "parent": not names
					name.Write(area[i+5 : i+l])
				}
			case "CE":
				if l >= 28 {
					block := int64(binary.LittleEndian.Uint32(area[i+4:]))
					off := int64(binary.LittleEndian.Uint32(area[i+12:]))
					n := int64(binary.LittleEndian.Uint32(area[i+20:]))
					buf := make([]byte, n)
					if _, err := img.r.ReadAt(buf, block*sector+off); err == nil || err == io.EOF {
						next = buf
					}
				}
			}
			i += l
		}
		if next == nil {
			break
		}
		area = next
	}
	return name.String()
}

func (n *node) load() error {
	if n.loaded || !n.dir {
		return nil
	}
	data := make([]byte, n.size)
	if _, err := n.img.r.ReadAt(data, n.extents[0].start); err != nil && err != io.EOF {
		return err
	}
	var last *node
	for i := 0; i < len(data); {
		l := int(data[i])
		if l == 0 { // records do not cross a sector: the rest of this sector is empty
			i = (i/sector + 1) * sector
			continue
		}
		if i+l > len(data) {
			break
		}
		rec, err := n.img.record(data[i:i+l], false)
		i += l
		if err != nil || rec == nil {
			continue
		}
		if last != nil && last.name == rec.name && !last.dir { // one more piece of a file stored in several
			last.extents = append(last.extents, rec.extents...)
			last.size += rec.size
			continue
		}
		n.kids = append(n.kids, rec)
		last = rec
	}
	sort.Slice(n.kids, func(a, b int) bool { return n.kids[a].name < n.kids[b].name })
	n.loaded = true
	return nil
}

// find walks a path (forward slashes, no leading slash needed). Names are compared exactly first and without regard to case if that finds nothing.
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

// File is an open file of the image: it can be read at any position, so a web server can answer range requests.
type File struct {
	n *node
}

// Open returns the file at name.
func (img *Image) Open(name string) (*File, error) {
	n, err := img.find(name)
	if err != nil {
		return nil, err
	}
	if n.dir {
		return nil, errors.New("iso9660: that is a folder")
	}
	return &File{n: n}, nil
}

// Exists reports whether a file or folder is in the image.
func (img *Image) Exists(name string) bool {
	_, err := img.find(name)
	return err == nil
}

// List returns the names in a folder, sorted.
func (img *Image) List(name string) ([]string, error) {
	n, err := img.find(name)
	if err != nil {
		return nil, err
	}
	if !n.dir {
		return nil, errors.New("iso9660: that is a file")
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

// ReadAt reads from the file at an offset, across its pieces.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.n.size {
		return 0, io.EOF
	}
	read := 0
	pos := int64(0)
	for _, e := range f.n.extents {
		if off >= pos+e.length {
			pos += e.length
			continue
		}
		inPiece := off - pos
		if inPiece < 0 {
			inPiece = 0
		}
		want := int64(len(p) - read)
		if want > e.length-inPiece {
			want = e.length - inPiece
		}
		n, err := f.n.img.r.ReadAt(p[read:read+int(want)], e.start+inPiece)
		read += n
		off += int64(n)
		pos += e.length
		if err != nil && err != io.EOF {
			return read, err
		}
		if read == len(p) {
			return read, nil
		}
	}
	if read < len(p) {
		return read, io.EOF
	}
	return read, nil
}

// Reader makes the file an io.ReadSeeker for http.ServeContent.
func (f *File) Reader() io.ReadSeeker { return io.NewSectionReader(f, 0, f.n.size) }

// ModTime is not stored per file here: the web server uses a fixed time, which is fine for files that never change.
func (f *File) ModTime() time.Time { return time.Unix(0, 0) }

// Entry is what a folder lists.
type Entry struct {
	Name string
	Size int64
	Dir  bool
}

// Stat says how big the file or folder at name is, and which of the two it is.
func (img *Image) Stat(name string) (Entry, error) {
	n, err := img.find(name)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Name: n.name, Size: n.size, Dir: n.dir}, nil
}

// ReadDir lists a folder with the size of each entry.
func (img *Image) ReadDir(name string) ([]Entry, error) {
	n, err := img.find(name)
	if err != nil {
		return nil, err
	}
	if !n.dir {
		return nil, errors.New("not a folder")
	}
	if err := n.load(); err != nil {
		return nil, err
	}
	out := make([]Entry, len(n.kids))
	for i, k := range n.kids {
		out[i] = Entry{Name: k.name, Size: k.size, Dir: k.dir}
	}
	return out, nil
}
