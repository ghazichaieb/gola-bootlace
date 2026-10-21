// Package smbshare shows the ISOs of the folder to Windows' set-up environment as a read-only network share: one folder per ISO (named by the ISO's
// short id) holding what the ISO holds, so that `net use Z: \\server\iso\<id>` followed by Z:\setup.exe installs Windows straight from the ISO file.
// The SMB protocol itself is the work of github.com/sonroyaalmerol/go-smb-server (MIT).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package smbshare

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net"
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
	Debug    bool   // log every message and every file opening (a lot: for finding faults); without it only each new connection is logged
	Forward  string // for finding faults: pass every connection on to another SMB server (host:port) and log what is said, instead of serving the ISOs
}

// Serve runs the share until ctx ends. Windows opens a connection with an SMB1 "multi-protocol negotiate" that offers SMB 2 among its dialects, and
// expects the server to answer in SMB 2 and the client to start again; the library speaks SMB 2 only. A small front end on the public address
// answers that first message and hands every connection on to the library's server, which listens on this computer's loopback address.
func Serve(ctx context.Context, o Options) error {
	trace := o.Logf
	level := slog.LevelDebug
	if !o.Debug {
		trace, level = nil, slog.LevelWarn
	}
	backend := &Backend{dir: o.ISODir, open: map[string]*opened{}, logf: trace}
	inner, err := freeLoopbackAddr()
	if err != nil {
		return err
	}
	if o.Forward != "" {
		inner = o.Forward
	}
	opts := []server.Option{server.WithAddr(inner),
		server.WithShares(vfs.NewDiskShare(ShareName, backend)),
		server.WithAuth(ntlmssp.NewServer(oneUser{user: o.User, password: o.Password}, "BOOTLACE")),
		server.WithLogger(slog.New(slog.NewTextHandler(logWriter(o.Logf), &slog.HandlerOptions{Level: level})))}
	srv, err := server.New(opts...)
	if err != nil {
		return err
	}
	front, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		front.Close()
	}()
	go func() {
		for {
			c, err := front.Accept()
			if err != nil {
				return
			}
			if o.Logf != nil {
				o.Logf("smb: %s connected", c.RemoteAddr())
			}
			go handOn(c, inner, o.Forward != "", trace)
		}
	}()
	if o.Forward != "" {
		<-ctx.Done()
		return nil
	}
	return srv.ListenAndServe(ctx)
}

func freeLoopbackAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

// handOn connects a client to the library's server. A first message that is an SMB1 negotiate gets the SMB 2 "wildcard" answer here and is not passed on.
func handOn(client net.Conn, inner string, passThrough bool, logf func(format string, args ...any)) {
	defer client.Close()
	first, err := readFrame(client)
	if err != nil {
		return
	}
	backend, err := dialInner(inner)
	if err != nil {
		return
	}
	defer backend.Close()
	if !passThrough && len(first) > 4 && first[0] == 0xFF && string(first[1:4]) == "SMB" {
		if logf != nil {
			logf("smb: %s opened with an SMB1 negotiate (%d bytes: % x): answering for SMB 2", client.RemoteAddr(), len(first), first[:min(len(first), 120)])
		}
		if _, err := client.Write(frame(wildcardNegotiateResponse())); err != nil {
			return
		}
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		next, err := readFrame(client)
		client.SetReadDeadline(time.Time{})
		if err != nil {
			if logf != nil {
				logf("smb: after our answer the client sent nothing usable: %v", err)
			}
			return
		}
		if logf != nil {
			logf("smb: the client's next message (%d bytes: % x)", len(next), next[:min(len(next), 100)])
		}
		if _, err := backend.Write(frame(next)); err != nil {
			return
		}
	} else if _, err := backend.Write(frame(first)); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { relay(backend, client, "client", logf); done <- struct{}{} }()
	go func() { relay(client, backend, "server", logf); done <- struct{}{} }()
	<-done
}

func dialInner(addr string) (net.Conn, error) {
	var last error
	for i := 0; i < 20; i++ { // the library's server may still be starting
		c, err := net.Dial("tcp", addr)
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(100 * time.Millisecond)
	}
	return nil, last
}

// readFrame reads one direct-TCP SMB message: a zero byte, a 3-byte length, the message.
func readFrame(c net.Conn) ([]byte, error) {
	var h [4]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		return nil, err
	}
	n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
	if n == 0 || n > 1<<24 { // a full read answer is the 1 MiB the server offers plus its headers
		return nil, errors.New("bad frame")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(c, b)
	return b, err
}

func frame(b []byte) []byte {
	return append([]byte{0, byte(len(b) >> 16), byte(len(b) >> 8), byte(len(b))}, b...)
}

// spnegoInit is the security blob a server puts in its first negotiate answer: a SPNEGO token that offers NTLMSSP (and the "please ignore" mechanism
// that Microsoft's own servers list), the same 42 bytes that Windows and Samba send.
var spnegoInit = []byte{0x60, 0x28, 0x06, 0x06, 0x2b, 0x06, 0x01, 0x05, 0x05, 0x02, 0xa0, 0x1e, 0x30, 0x1c, 0xa0, 0x1a, 0x30, 0x18, 0x06, 0x0a, 0x2b, 0x06, 0x01,
	0x04, 0x01, 0x82, 0x37, 0x02, 0x02, 0x1e, 0x06, 0x0a, 0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0x37, 0x02, 0x02, 0x0a}

// wildcardNegotiateResponse is the SMB 2 NEGOTIATE response that answers an SMB1 multi-protocol negotiate: dialect 0x02FF tells the client to
// start again with an SMB 2 negotiate of its own ([MS-SMB2] 3.3.5.3.1).
func wildcardNegotiateResponse() []byte {
	b := make([]byte, 64+64+len(spnegoInit))
	copy(b, []byte{0xFE, 'S', 'M', 'B'})
	binary.LittleEndian.PutUint16(b[4:], 64) // header size
	binary.LittleEndian.PutUint16(b[14:], 1) // credits granted
	binary.LittleEndian.PutUint32(b[16:], 1) // flags: this is a response
	body := b[64:]
	binary.LittleEndian.PutUint16(body[0:], 65)                                                                            // structure size
	binary.LittleEndian.PutUint16(body[2:], 1)                                                                             // security mode: signing enabled
	binary.LittleEndian.PutUint16(body[4:], 0x02FF)                                                                        // dialect: wildcard
	copy(body[8:], []byte{0x42, 0x6f, 0x6f, 0x74, 0x6c, 0x61, 0x63, 0x65, 0x47, 0x6f, 0x6c, 0x61, 0x53, 0x76, 0x63, 0x01}) // server guid
	binary.LittleEndian.PutUint32(body[28:], 0x100000)
	binary.LittleEndian.PutUint32(body[32:], 0x100000)
	binary.LittleEndian.PutUint32(body[36:], 0x100000)
	now := uint64(time.Now().UnixNano()/100) + 116444736000000000 // Windows file time
	binary.LittleEndian.PutUint64(body[40:], now)
	binary.LittleEndian.PutUint64(body[48:], now)
	binary.LittleEndian.PutUint16(body[56:], 128) // security buffer offset, from the start of the message
	binary.LittleEndian.PutUint16(body[58:], uint16(len(spnegoInit)))
	copy(body[64:], spnegoInit)
	return b
}

// logWriter turns the server library's log lines into lines of our own log.
type logWriter func(format string, args ...any)

func (w logWriter) Write(p []byte) (int, error) {
	if w != nil {
		w("smb: %s", strings.TrimSpace(string(p)))
	}
	return len(p), nil
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
	logf func(format string, args ...any)
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
func (b *Backend) Open(ctx context.Context, opts vfs.OpenOptions) (vfs.Handle, error) {
	h, err := b.open_(ctx, opts)
	if b.logf != nil {
		b.logf("smb: open %q disposition %d dir=%v: %v", opts.Path, opts.Disposition, opts.CreateDir, err)
	}
	return h, err
}

func (b *Backend) open_(_ context.Context, opts vfs.OpenOptions) (vfs.Handle, error) {
	if opts.Disposition != vfs.DispositionOpen && opts.Disposition != vfs.DispositionOpenIf { // making, replacing or emptying a file: refused (CreateDir is only Windows saying "this must be a folder")
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

// started is the time the root folder of the share shows: a folder whose dates change at every look makes Windows think it was replaced.
var started = time.Now()

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
	return info("", 0, true, started), nil
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

// relay passes SMB messages from one side to the other and notes what they are (the command, and the status of an answer) when the log is on.
func relay(to, from net.Conn, who string, logf func(format string, args ...any)) {
	for {
		msg, err := readFrame(from)
		if err != nil {
			if logf != nil {
				logf("smb: %s side ended: %v", who, err)
			}
			return
		}
		if logf != nil && !(len(msg) >= 16 && msg[0] == 0xFE) {
			logf("smb: %s: a frame that is not plain SMB2 (%d bytes: % x)", who, len(msg), msg[:min(len(msg), 64)])
		}
		if logf != nil && len(msg) >= 16 && msg[0] == 0xFE {
			logf("smb: %s: command %d status %#x flags %#x msgid %d (%d bytes)", who, binary.LittleEndian.Uint16(msg[12:]), binary.LittleEndian.Uint32(msg[8:]), binary.LittleEndian.Uint32(msg[16:]), binary.LittleEndian.Uint64(msg[24:]), len(msg))
			if c := binary.LittleEndian.Uint16(msg[12:]); c <= 1 || c == 3 || c == 5 || c == 16 || c == 14 || c == 11 {
				logf("smb: %s bytes: % x", who, msg[:min(len(msg), 400)])
			}
		}
		if _, err := to.Write(frame(msg)); err != nil {
			return
		}
	}
}
