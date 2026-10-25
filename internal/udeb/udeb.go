// Package udeb reads what the Debian installer's own packages are made of: the Packages index of an ISO (which udebs there are, where they are, what they need) and the udeb
// files themselves (an ar archive holding a compressed tar of the files to install). The installer of a netinst ISO starts from a small start-up disk and installs the rest of
// its parts from the ISO's pool; Bootlace takes parts out of that pool for the start-up disk (network card drivers, the iSCSI client) that the installer would only load once
// it has found its CD.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package udeb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// maxFile caps the size of one file taken out of a package (a kernel module is far smaller: this only stops a damaged file from taking the server's memory).
const maxFile = 64 << 20

// Package is one stanza of a Packages index.
type Package struct {
	Name     string
	Version  string
	Filename string   // the path of the file in the ISO, without a leading slash
	Depends  []string // package names, versions and alternatives left out (the first alternative stands for the group)
}

// ParsePackages reads a Packages index (uncompressed text).
func ParsePackages(r io.Reader) ([]Package, error) {
	var out []Package
	var cur Package
	var field string
	flush := func() {
		if cur.Name != "" {
			out = append(out, cur)
		}
		cur, field = Package{}, ""
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case line[0] == ' ' || line[0] == '\t': // a continuation line: only the dependency list can go on over several lines
			if field == "Depends" {
				cur.Depends = append(cur.Depends, splitDepends(line)...)
			}
		default:
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			field = key
			value = strings.TrimSpace(value)
			switch key {
			case "Package":
				cur.Name = value
			case "Version":
				cur.Version = value
			case "Filename":
				cur.Filename = strings.TrimPrefix(value, "/")
			case "Depends":
				cur.Depends = append(cur.Depends, splitDepends(value)...)
			}
		}
	}
	flush()
	return out, sc.Err()
}

// splitDepends turns "a (>= 1), b | c" into [a b]: the names only, the first of each group of alternatives.
func splitDepends(s string) []string {
	var out []string
	for _, group := range strings.Split(s, ",") {
		first, _, _ := strings.Cut(group, "|")
		name := strings.TrimSpace(first)
		if i := strings.IndexAny(name, " ("); i >= 0 {
			name = name[:i]
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// Index finds packages by name.
type Index map[string]Package

// NewIndex makes an index of the packages; the last stanza of a name wins.
func NewIndex(list []Package) Index {
	idx := Index{}
	for _, p := range list {
		idx[p.Name] = p
	}
	return idx
}

// Closure is the package and what it needs, each name once, the package first, following Depends through packages the index knows (a name the index does not know is a
// virtual package or one the start-up disk already has: it is left out). skip names packages that are not wanted, and what only they need.
func (idx Index) Closure(name string, skip func(string) bool) []Package {
	var out []Package
	seen := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		if seen[n] || (skip != nil && skip(n)) {
			return
		}
		seen[n] = true
		p, ok := idx[n]
		if !ok {
			return
		}
		out = append(out, p)
		for _, d := range p.Depends {
			walk(d)
		}
	}
	walk(name)
	return out
}

// Entry is one file of a package.
type Entry struct {
	Name string      // path without a leading "./" or slash
	Mode fs.FileMode // permission bits and the type (directory, link)
	Link string      // the target of a symbolic link
	Data []byte      // the contents of a regular file
}

// Files reads a udeb (an ar archive holding data.tar.xz, .gz, .zst or plain) and calls fn for every regular file, link and directory in it. fn may keep the entry.
func Files(r io.Reader, fn func(Entry) error) error {
	var found bool
	err := ar(r, func(name string, size int64, body io.Reader) error {
		var tr io.Reader
		switch {
		case name == "data.tar":
			tr = body
		case name == "data.tar.gz":
			zr, err := gzip.NewReader(body)
			if err != nil {
				return err
			}
			tr = zr
		case name == "data.tar.xz":
			zr, err := xz.NewReader(body)
			if err != nil {
				return err
			}
			tr = zr
		case name == "data.tar.zst":
			zr, err := zstd.NewReader(body)
			if err != nil {
				return err
			}
			defer zr.Close()
			tr = zr
		default:
			return nil
		}
		found = true
		t := tar.NewReader(tr)
		for {
			h, err := t.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			e := Entry{Name: strings.TrimPrefix(path.Clean("/"+h.Name), "/"), Mode: h.FileInfo().Mode()}
			if e.Name == "" || e.Name == "." {
				continue
			}
			switch h.Typeflag {
			case tar.TypeReg:
				if h.Size > maxFile {
					return fmt.Errorf("%s: a file of %d bytes is far too big for a package part", e.Name, h.Size)
				}
				if e.Data, err = io.ReadAll(t); err != nil {
					return err
				}
			case tar.TypeSymlink:
				e.Link = h.Linkname
			case tar.TypeDir:
			default:
				continue
			}
			if err := fn(e); err != nil {
				return err
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return errors.New("not a udeb: no data archive in it")
	}
	return nil
}

// ar walks the members of an ar archive ("!<arch>\n", then for each member a 60 byte header: name 16, date 12, owner 6, group 6, mode 8, size 10, end "`\n"). A member is
// read while fn runs; the padding byte after an odd size is skipped.
func ar(r io.Reader, fn func(name string, size int64, body io.Reader) error) error {
	br := bufio.NewReader(r)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != "!<arch>\n" {
		return errors.New("not an ar archive")
	}
	for {
		hdr := make([]byte, 60)
		if _, err := io.ReadFull(br, hdr); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if string(hdr[58:60]) != "`\n" {
			return errors.New("ar: damaged member header")
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		if err != nil || size < 0 {
			return errors.New("ar: bad member size")
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(hdr[:16])), "/")
		lr := &io.LimitedReader{R: br, N: size}
		if err := fn(name, size, lr); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, lr); err != nil {
			return err
		}
		if size%2 == 1 {
			if _, err := br.ReadByte(); err != nil && !errors.Is(err, io.EOF) {
				return err
			}
		}
	}
}

// Gunzip reads a compressed Packages index (Packages.gz, or plain text as it is).
func Gunzip(b []byte) (io.Reader, error) {
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		return gzip.NewReader(bytes.NewReader(b))
	}
	if len(b) > 6 && b[0] == 0xfd && string(b[1:6]) == "7zXZ\x00" {
		return xz.NewReader(bytes.NewReader(b))
	}
	return bytes.NewReader(b), nil
}
