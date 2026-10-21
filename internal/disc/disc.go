// Package disc opens the file system of an ISO whichever kind it is: UDF (Windows media and many others) or plain ISO 9660 with Rock Ridge (most Linux).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package disc

import (
	"io"

	"github.com/ghazichaieb/gola-bootlace/internal/iso9660"
	"github.com/ghazichaieb/gola-bootlace/internal/udf"
)

// File is an open file of a disc.
type File interface {
	Size() int64
	ReadAt(p []byte, off int64) (int, error)
	Reader() io.ReadSeeker
}

// Entry is what a folder lists.
type Entry struct {
	Name string
	Size int64
	Dir  bool
}

// FS is the contents of a disc.
type FS interface {
	Exists(name string) bool
	List(name string) ([]string, error)
	Open(name string) (File, error)
	Stat(name string) (Entry, error)
	ReadDir(name string) ([]Entry, error)
	Kind() string
}

type udfFS struct{ *udf.Image }

func (u udfFS) Open(name string) (File, error) { return u.Image.Open(name) }
func (u udfFS) Kind() string                   { return "UDF" }
func (u udfFS) Stat(name string) (Entry, error) {
	e, err := u.Image.Stat(name)
	return Entry(e), err
}
func (u udfFS) ReadDir(name string) ([]Entry, error) {
	list, err := u.Image.ReadDir(name)
	out := make([]Entry, len(list))
	for i, e := range list {
		out[i] = Entry(e)
	}
	return out, err
}

type isoFS struct{ *iso9660.Image }

func (i isoFS) Open(name string) (File, error) { return i.Image.Open(name) }
func (i isoFS) Kind() string                   { return "ISO 9660" }
func (i isoFS) Stat(name string) (Entry, error) {
	e, err := i.Image.Stat(name)
	return Entry(e), err
}
func (i isoFS) ReadDir(name string) ([]Entry, error) {
	list, err := i.Image.ReadDir(name)
	out := make([]Entry, len(list))
	for i, e := range list {
		out[i] = Entry(e)
	}
	return out, err
}

// Open reads the file system of the image: UDF when there is one with files in it, otherwise ISO 9660.
func Open(r io.ReaderAt, size int64) (FS, error) {
	if u, err := udf.Open(r, size); err == nil {
		if names, err := u.List("/"); err == nil && len(names) > 0 {
			return udfFS{u}, nil
		}
	}
	i, err := iso9660.Open(r, size)
	if err != nil {
		return nil, err
	}
	return isoFS{i}, nil
}
