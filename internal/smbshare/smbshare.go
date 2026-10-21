// Package smbshare shows the ISOs of the folder to Windows' set-up environment as a read-only network share: one folder per ISO (named by the ISO's
// short id) holding what the ISO holds, so that `net use Z: \\server\iso\<id>` followed by Z:\setup.exe installs Windows straight from the ISO file.
// The SMB protocol itself is the work of github.com/sonroyaalmerol/go-smb-server (MIT).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package smbshare

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sonroyaalmerol/go-smb-server/smb/ntlmssp"
	"github.com/sonroyaalmerol/go-smb-server/smb/server"
	"github.com/sonroyaalmerol/go-smb-server/smb/vfs"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// ShareName is the name of the one share.
const ShareName = "iso"

const (
	attrReadOnly  = 0x01
	attrDirectory = 0x10
)

// Options of the share.
type Options struct {
	Addr     string // where to listen, for example 192.168.88.1:445
	ISODir   string
	User     string
	Password string
	Logf     func(format string, args ...any)
}

// Serve runs the share until ctx ends.
func Serve(ctx context.Context, o Options) error {
	backend := &Backend{dir: o.ISODir, open: map[string]*opened{}}
	srv, err := server.New(
		server.WithAddr(o.Addr),
		server.WithShares(vfs.NewDiskShare(ShareName, backend)),
		server.WithAuth(ntlmssp.NewServer(oneUser{user: o.User, password: o.Password}, "BOOTLACE")),
		server.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	if err != nil {
		return err
	}
	return srv.ListenAndServe(ctx)
}

// oneUser accepts the one account the set-up environment logs in with, whatever domain name the client sends.
type oneUser struct{ user, password string }

func (u oneUser) LookupNTOWFv2(_ context.Context, domain, user string) ([]byte, error) {
	if !strings.EqualFold(user, u.user) {
		return nil, errors.New("unknown user")
	}
	return ntlmssp.NTOWFv2(u.password, user, domain), nil
}

// Backend is the read-only file system of the share.
type Backend struct {
	dir  string
	mu   sync.Mutex
	open map[string]*opened // by ISO id: the file and its contents, kept open while the server runs
}

type opened struct {
	file *os.File
	fs   disc.FS
	iso  isos.ISO
}

func (b *Backend) disc(id string) (*opened, error) {
	list, err := isos.Scan(b.dir)
	if err != nil {
		return nil, err
	}
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if o, ok := b.open[id]; ok && o.iso.ModTime.Equal(iso.ModTime) && o.iso.Size == iso.Size {
			return o, nil
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			return nil, err
		}
		fsys, err := disc.Open(f, iso.Size)
		if err != nil {
			f.Close()
			return nil, err
		}
		o := &opened{file: f, fs: fsys, iso: iso}
		b.open[id] = o
		return o, nil
	}
	return nil, os.ErrNotExist
}

// Open implements vfs.Backend. Anything that would change a file is refused.
func (b *Backend) Open(_ context.Context, opts vfs.OpenOptions) (vfs.Handle, error) {
	if opts.Disposition != vfs.DispositionOpen && opts.Disposition != vfs.DispositionOpenIf || opts.CreateDir {
		return nil, os.ErrPermission
	}
	p := strings.Trim(strings.ReplaceAll(opts.Path, "\\", "/"), "/")
	if p == "" {
		return &rootHandle{b: b}, nil
	}
	id, rest, _ := strings.Cut(p, "/")
	o, err := b.disc(id)
	if err != nil {
		return nil, os.ErrNotExist
	}
	e, err := o.fs.Stat("/" + rest)
	if err != nil {
		return nil, os.ErrNotExist
	}
	h := &handle{o: o, path: "/" + rest, name: e.Name, entry: e}
	if !e.Dir {
		if h.file, err = o.fs.Open("/" + rest); err != nil {
			return nil, err
		}
	}
	if rest == "" {
		h.name = id
	}
	return h, nil
}

func info(name string, size int64, dir bool, mod time.Time) vfs.FileInfo {
	attr := uint32(attrReadOnly)
	if dir {
		attr = attrDirectory
		size = 0
	}
	return vfs.FileInfo{Name: name, Size: size, IsDir: dir, Attributes: attr, CreationTime: mod, LastAccess: mod, LastWrite: mod, ChangeTime: mod}
}

// handle is a file or a folder of an ISO.
type handle struct {
	o     *opened
	path  string
	name  string
	entry disc.Entry
	file  disc.File
}

func (h *handle) Read(_ context.Context, offset int64, p []byte) (int, error) {
	if h.file == nil {
		return 0, errors.New("a folder cannot be read as a file")
	}
	return h.file.ReadAt(p, offset)
}

func (h *handle) Write(context.Context, int64, []byte) (int, error) { return 0, os.ErrPermission }
func (h *handle) Close(context.Context) error                       { return nil }

func (h *handle) Stat(context.Context) (vfs.FileInfo, error) {
	return info(h.name, h.entry.Size, h.entry.Dir, h.o.iso.ModTime), nil
}

func (h *handle) Enumerate(_ context.Context, pattern string) iter.Seq2[vfs.FileInfo, error] {
	return func(yield func(vfs.FileInfo, error) bool) {
		list, err := h.o.fs.ReadDir(h.path)
		if err != nil {
			yield(vfs.FileInfo{}, err)
			return
		}
		for _, e := range list {
			if !matches(pattern, e.Name) {
				continue
			}
			if !yield(info(e.Name, e.Size, e.Dir, h.o.iso.ModTime), nil) {
				return
			}
		}
	}
}

// rootHandle is the top of the share: a folder per ISO.
type rootHandle struct{ b *Backend }

func (r *rootHandle) Read(context.Context, int64, []byte) (int, error) {
	return 0, errors.New("a folder")
}
func (r *rootHandle) Write(context.Context, int64, []byte) (int, error) {
	return 0, os.ErrPermission
}
func (r *rootHandle) Close(context.Context) error { return nil }
func (r *rootHandle) Stat(context.Context) (vfs.FileInfo, error) {
	return info("", 0, true, time.Now()), nil
}
func (r *rootHandle) Enumerate(_ context.Context, pattern string) iter.Seq2[vfs.FileInfo, error] {
	return func(yield func(vfs.FileInfo, error) bool) {
		list, err := isos.Scan(r.b.dir)
		if err != nil {
			yield(vfs.FileInfo{}, err)
			return
		}
		for _, iso := range list {
			if matches(pattern, iso.ID) && !yield(info(iso.ID, 0, true, iso.ModTime), nil) {
				return
			}
		}
	}
}

// matches is the directory search pattern of SMB: "*" and "" list everything, "?" and "*" work as usual, case does not matter.
func matches(pattern, name string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	return wildcard(strings.ToLower(pattern), strings.ToLower(name))
}

func wildcard(p, s string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for i := 0; i <= len(s); i++ {
				if wildcard(p[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
		}
		p, s = p[1:], s[1:]
	}
	return len(s) == 0
}
