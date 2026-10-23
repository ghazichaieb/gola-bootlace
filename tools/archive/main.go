// Command archive packs a folder into a release archive the same way on every computer: tools/archive <folder> <out.tar.gz|out.zip> [-time unix-seconds].
// Every file is owned by user 0 (an archive that names the builder's own user cannot be unpacked by root in an unprivileged container), the program is executable
// and the rest is not (Windows has no execute bit, so the builder's tar could not say it), and every file has the same time, so that the same sources give
// the same archive. The folder itself becomes the one top-level folder of the archive.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	stamp := flag.Int64("time", 0, "the time of every file, in Unix seconds (default: SOURCE_DATE_EPOCH, or now)")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: archive [-time N] <folder> <out.tar.gz|out.zip>")
		os.Exit(2)
	}
	t := time.Now()
	switch {
	case *stamp != 0:
		t = time.Unix(*stamp, 0)
	case os.Getenv("SOURCE_DATE_EPOCH") != "":
		var n int64
		if _, err := fmt.Sscan(os.Getenv("SOURCE_DATE_EPOCH"), &n); err == nil {
			t = time.Unix(n, 0)
		}
	}
	if err := pack(flag.Arg(0), flag.Arg(1), t.UTC()); err != nil {
		fmt.Fprintln(os.Stderr, "archive:", err)
		os.Exit(1)
	}
}

// executable says whether a file of a release is a program.
func executable(name string) bool {
	base := filepath.Base(name)
	return base == "bootlace" || base == "bootlace.exe"
}

type entry struct {
	rel  string // under the top-level folder, with "/"
	path string
	dir  bool
	size int64
}

func list(root string) ([]entry, error) {
	var out []entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a plain file", p)
		}
		out = append(out, entry{rel: filepath.ToSlash(rel), path: p, dir: d.IsDir(), size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, err
}

func pack(folder, out string, t time.Time) error {
	folder = filepath.Clean(folder)
	top := filepath.Base(folder)
	entries, err := list(folder)
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	switch {
	case strings.HasSuffix(out, ".tar.gz"):
		gz, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: top + "/", Mode: 0o755, ModTime: t, Format: tar.FormatPAX}); err != nil {
			return err
		}
		for _, e := range entries {
			h := &tar.Header{Name: top + "/" + e.rel, ModTime: t, Uid: 0, Gid: 0, Format: tar.FormatPAX}
			if e.dir {
				h.Typeflag, h.Name, h.Mode = tar.TypeDir, h.Name+"/", 0o755
			} else {
				h.Typeflag, h.Size, h.Mode = tar.TypeReg, e.size, 0o644
				if executable(e.rel) {
					h.Mode = 0o755
				}
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if !e.dir {
				if err := copyFile(tw, e.path); err != nil {
					return err
				}
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	case strings.HasSuffix(out, ".zip"):
		zw := zip.NewWriter(f)
		for _, e := range entries {
			if e.dir {
				continue
			}
			h := &zip.FileHeader{Name: top + "/" + e.rel, Method: zip.Deflate, Modified: t}
			h.SetMode(0o644)
			if executable(e.rel) {
				h.SetMode(0o755)
			}
			w, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if err := copyFile(w, e.path); err != nil {
				return err
			}
		}
		return zw.Close()
	}
	return errors.New("the output must end in .tar.gz or .zip")
}

func copyFile(w io.Writer, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}
