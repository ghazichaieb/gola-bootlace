// Receiving files into the library: a file is written under <library>/.bootlace/incoming with a name that ends in .part, checked, and renamed into its place only
// when it is complete. The menu, the Windows share and the file list never see a partial file, and a failure leaves nothing in the library itself.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IncomingDir is the folder where files are received. It is made when needed, and only in a library that is available.
func (l *Library) IncomingDir() (string, error) {
	if err := l.Check(); err != nil {
		return "", err
	}
	dir := filepath.Join(l.Path(), MetaDir, "incoming")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// NewPart makes the empty file that a download or an upload is written to. The name is only for the person reading a folder listing; it is not trusted.
func (l *Library) NewPart(name string) (*os.File, error) {
	dir, err := l.IncomingDir()
	if err != nil {
		return nil, err
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, filepath.Base(name))
	if len(safe) > 80 {
		safe = safe[:80]
	}
	return os.OpenFile(filepath.Join(dir, hex.EncodeToString(b)+"-"+safe+".part"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
}

// Place moves a finished part file into the library as destRel (a "/" path whose name is one the library offers). Nothing is replaced: a name that is taken is an
// error, and the part file stays where it is for the caller to remove.
func (l *Library) Place(part, destRel string) error {
	incoming, err := l.IncomingDir()
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(incoming, part); err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("that is not a file that was received")
	}
	_, dst, err := l.abs(destRel)
	if err != nil {
		return err
	}
	destRel, _ = CleanRel(destRel)
	name := filepath.Base(dst)
	if destRel == "" {
		return errors.New("choose a name for the file")
	}
	if err := ValidName(name); err != nil {
		return err
	}
	if !Managed(name) {
		return fmt.Errorf("a file keeps a name that ends in %s", managedList())
	}
	if st, err := os.Stat(filepath.Dir(dst)); err != nil || !st.IsDir() {
		return ErrNotFound
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrExists
	}
	return os.Rename(part, dst)
}

// ---- free space

// Reserve is the space that is never used up: a library with a full disk stops everything that writes to it. It is 1 % of the disk, at least 512 MiB and at most
// 5 GiB.
func Reserve(total uint64) uint64 {
	r := total / 100
	if r < 512<<20 {
		r = 512 << 20
	}
	if r > 5<<30 {
		r = 5 << 30
	}
	return r
}

// EnoughSpace says whether need more bytes fit in the library and leave the reserve alone. need 0 only asks whether the reserve is still there.
func (l *Library) EnoughSpace(need int64) error {
	free, total, err := DiskSpace(l.Path())
	if err != nil {
		return nil // the disk cannot say: the write itself will fail if there is no room
	}
	r := Reserve(total)
	if free < r || free-r < uint64(max(need, 0)) {
		return fmt.Errorf("not enough room in the library: %s free, %s of it is kept in reserve", Size(free), Size(r))
	}
	return nil
}

// Size is a number of bytes as a person reads it.
func Size(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

// ---- what a file is

// SniffBytes is how much of the start of a file Sniff wants to see.
const SniffBytes = 64 << 10

// ErrTooShort is the answer of Sniff when the start it was given is shorter than it needs, so the caller has to read more first (or, when that is the whole file,
// refuse it).
var ErrTooShort = errors.New("the file is too short to be what its name says")

var magics = map[string][]string{
	".gz":  {"\x1f\x8b"},
	".xz":  {"\xfd7zXZ\x00"},
	".zst": {"\x28\xb5\x2f\xfd"},
	".bz2": {"BZh"},
	".zip": {"PK\x03\x04", "PK\x05\x06"},
}

// Sniff checks the start of a file against what its name says it is, so that a web page that was saved as "ubuntu.iso" is refused before it is kept.
func Sniff(name string, head []byte) error {
	ext := strings.ToLower(filepath.Ext(name))
	if m, ok := magics[ext]; ok {
		for _, magic := range m {
			if bytes.HasPrefix(head, []byte(magic)) {
				return nil
			}
		}
		if len(head) < 6 {
			return ErrTooShort
		}
		return fmt.Errorf("this is not a %s file: it does not start the way one does (a web page that says the download failed is the usual cause)", ext)
	}
	switch ext {
	case ".iso":
		// an ISO has "CD001" (ISO 9660) or "BEA01" (UDF) at the start of its 17th sector
		if len(head) < 0x8006 {
			return ErrTooShort
		}
		if tag := head[0x8001:0x8006]; bytes.Equal(tag, []byte("CD001")) || bytes.Equal(tag, []byte("BEA01")) {
			return nil
		}
		return errors.New("this is not an ISO file: it has no ISO 9660 or UDF header (a web page that says the download failed is the usual cause)")
	case ".img", ".raw":
		if len(head) < 512 {
			return ErrTooShort
		}
		if looksLikeText(head[:512]) {
			return errors.New("this looks like text, not a disk image (a web page that says the download failed is the usual cause)")
		}
		return nil
	}
	return fmt.Errorf("a file keeps a name that ends in %s", managedList())
}

// NeedBytes is how much of the start of a file Sniff needs to give a definite answer for this name.
func NeedBytes(name string) int {
	if strings.EqualFold(filepath.Ext(name), ".iso") {
		return 0x8006
	}
	return 512
}

func looksLikeText(b []byte) bool {
	trim := strings.ToLower(strings.TrimSpace(string(b[:min(len(b), 64)])))
	for _, p := range []string{"<!doctype", "<html", "<?xml", "<head", "<body", "{", "["} {
		if strings.HasPrefix(trim, p) {
			return true
		}
	}
	printable := 0
	for _, c := range b {
		if c == 0 {
			return false
		}
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f) {
			printable++
		}
	}
	return printable*100 >= len(b)*95
}
