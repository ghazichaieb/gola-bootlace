// Uploading a file from the browser: the body of the request is written straight to a part file in the library's incoming folder, never held in memory, checked
// for what it is while it arrives, and put in place when it is complete.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package importer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// UploadRequest is what the browser says about a file before and while it sends it.
type UploadRequest struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Size   int64  `json:"size"`   // the size of the file: required, it is how room is made and how a cut upload is noticed
	SHA256 string `json:"sha256"` // optional: what the person says the file's SHA-256 is
}

// CheckUpload looks at an upload before it is sent: the name, the place, the room, and what it would repeat.
func (m *Manager) CheckUpload(u UploadRequest) Check {
	return m.checkUpload(u, 0)
}

// received bytes already occupy space in the incoming folder.
func (m *Manager) checkUpload(u UploadRequest, received int64) Check {
	c := Check{Size: u.Size, Problems: []string{}, Matches: []library.Match{}}
	problem := func(s string) Check { c.Problems = append(c.Problems, s); return c }
	if err := m.Lib.Check(); err != nil {
		return problem(err.Error())
	}
	name := strings.TrimSpace(u.Name)
	if err := library.ValidName(name); err != nil {
		return problem("choose a file name: " + err.Error())
	}
	if !library.Managed(name) {
		return problem("the file name has to end in .iso, .img, .raw, .xz, .gz, .zst, .bz2 or .zip")
	}
	folder, err := library.CleanRel(u.Folder)
	if err != nil {
		return problem(err.Error())
	}
	if _, err := m.Lib.ListDir(folder); err != nil {
		return problem(err.Error())
	}
	if u.Size <= 0 {
		return problem("the size of the file is needed")
	}
	if err := m.Lib.EnoughSpace(max(u.Size-received, 0)); err != nil {
		return problem(err.Error())
	}
	c.Name, c.Dest = name, strings.TrimPrefix(folder+"/"+name, "/")
	sha := ""
	if s := strings.TrimSpace(u.SHA256); s != "" {
		if sha, err = normalizeSum(s); err != nil || len(sha) != 64 {
			return problem("the checksum has to be a SHA-256 (64 characters) in hexadecimal")
		}
		c.Expected = sha
	}
	if idx := m.Index(); idx != nil {
		c.Matches = idx.Duplicates(c.Dest, "", u.Size, sha)
	}
	return c
}

// Upload receives the file and puts it in the library; it returns the path it has there and what it noticed (the same content as another file).
func (m *Manager) Upload(u UploadRequest, body io.Reader) (dest string, warnings []string, err error) {
	c := m.CheckUpload(u)
	if len(c.Problems) > 0 {
		return "", nil, errors.New(strings.Join(c.Problems, "; "))
	}
	f, err := m.Lib.NewPart(c.Name)
	if err != nil {
		return "", nil, err
	}
	defer func() {
		f.Close()
		if err != nil {
			_ = os.Remove(f.Name()) // whatever went wrong, no part of the file stays behind
		}
	}()
	sums := newSums("")
	var head []byte
	hw := &headWriter{buf: &head}
	buf := make([]byte, 256<<10)
	var n, sinceCheck int64
	for {
		k, rerr := body.Read(buf)
		if k > 0 {
			if n+int64(k) > u.Size {
				return "", nil, errors.New("more data arrived than the size that was announced")
			}
			if _, werr := f.Write(buf[:k]); werr != nil {
				return "", nil, fmt.Errorf("cannot write to the library: %w", werr)
			}
			sums.Write(buf[:k])
			if len(head) < library.NeedBytes(c.Name) {
				hw.Write(buf[:k])
				if len(head) >= library.NeedBytes(c.Name) {
					if serr := library.Sniff(c.Name, head); serr != nil && !errors.Is(serr, library.ErrTooShort) {
						return "", nil, fmt.Errorf("this file is not what its name says: %w", serr)
					}
				}
			}
			n += int64(k)
			if sinceCheck += int64(k); sinceCheck >= 256<<20 {
				sinceCheck = 0
				if e := m.Lib.EnoughSpace(0); e != nil {
					return "", nil, e
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", nil, fmt.Errorf("the upload was cut: %w", rerr)
		}
	}
	if n != u.Size {
		return "", nil, fmt.Errorf("the upload is incomplete: %d of %d bytes arrived", n, u.Size)
	}
	if serr := library.Sniff(c.Name, head); serr != nil {
		return "", nil, fmt.Errorf("this file is not what its name says: %w", serr)
	}
	sha := sums.sha256Hex()
	if c.Expected != "" && sha != c.Expected {
		return "", nil, fmt.Errorf("the checksum does not match: the file has %s, expected %s (the upload is damaged or it is another file)", short(sha), short(c.Expected))
	}
	if err = f.Sync(); err != nil {
		return "", nil, err
	}
	if err = f.Close(); err != nil {
		return "", nil, err
	}
	if err = m.Lib.Place(f.Name(), c.Dest); err != nil {
		return "", nil, fmt.Errorf("the file cannot be put in the library: %w", err)
	}
	if idx := m.Index(); idx != nil {
		_ = idx.Set(c.Dest, library.Record{SHA256: sha, Added: time.Now()})
		for _, d := range idx.Duplicates("", "", 0, sha) {
			if d.Path != c.Dest {
				warnings = append(warnings, "the content is the same as "+d.Path)
			}
		}
	}
	m.logf("upload: %s is in the library", c.Dest)
	return c.Dest, warnings, nil
}
