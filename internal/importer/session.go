// Resumable uploads: a big file is sent in pieces to an upload session, so that a connection that breaks halfway (or a page that is reloaded) continues from where the
// server has the file instead of from the start. The part file is the same kind of file as the one a single-request upload writes, in <library>/.bootlace/incoming; a small
// record next to it (<id>.session.json) says what is being received. The checks that a single-request upload makes (name, content, size, checksum, room, nothing replaced)
// all still run, the content check as soon as the start of the file is there and the rest when the file is complete.
// There is one administrator, so a session is not tied to one login (a login lasts 12 hours, a big upload to a slow link can last longer).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package importer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// Session is an upload that has been started.
type Session struct {
	ID       string        `json:"id"`
	Request  UploadRequest `json:"request"`
	Part     string        `json:"part"`     // the name of the part file in the incoming folder
	Received int64         `json:"received"` // how many bytes of the file the server has, in order from the start
	Created  time.Time     `json:"created"`
	Updated  time.Time     `json:"updated"`
}

// SessionTTL is how long a session that has not been used is kept.
const SessionTTL = 7 * 24 * time.Hour

// MaxSessions is how many uploads may be open at once.
const MaxSessions = 20

// MaxChunk is the largest piece one request may carry.
const MaxChunk = 64 << 20

// OffsetError says where the server has the file when a piece arrives at the wrong place.
type OffsetError struct{ Received int64 }

func (e *OffsetError) Error() string {
	return fmt.Sprintf("the server has %d bytes of this file: continue from there", e.Received)
}

// ErrNoSession is the answer for a session that is not there (finished, cancelled, expired).
var ErrNoSession = errors.New("there is no such upload (it may have ended or expired): start it again")

var sessionLocks sync.Map // id -> *sync.Mutex: one piece at a time per session

func lockOf(id string) *sync.Mutex {
	m, _ := sessionLocks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (m *Manager) sessionFile(id string) (string, error) {
	if len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
		return "", ErrNoSession
	}
	dir, err := m.Lib.IncomingDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".session.json"), nil
}

func (m *Manager) loadSession(id string) (*Session, string, error) {
	path, err := m.sessionFile(id)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrNoSession
	}
	var s Session
	if json.Unmarshal(data, &s) != nil || s.ID != id {
		return nil, "", ErrNoSession
	}
	return &s, path, nil
}

func (s *Session) save(path string) error {
	s.Updated = time.Now()
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *Manager) partPath(s *Session) (string, error) {
	dir, err := m.Lib.IncomingDir()
	if err != nil {
		return "", err
	}
	if s.Part == "" || s.Part != filepath.Base(s.Part) || !strings.HasSuffix(s.Part, ".part") {
		return "", errors.New("the upload record is damaged")
	}
	return filepath.Join(dir, s.Part), nil
}

// StartUpload checks the request like a single-request upload does and opens a session for it. An upload of the same name, folder and size that is still open is
// continued instead of started again, so a page that was reloaded finds its upload.
func (m *Manager) StartUpload(u UploadRequest) (*Session, Check, error) {
	c := m.CheckUpload(u)
	if len(c.Problems) > 0 {
		return nil, c, errors.New(strings.Join(c.Problems, "; "))
	}
	for _, s := range m.Uploads() {
		if s.Request.Name == u.Name && s.Request.Folder == u.Folder && s.Request.Size == u.Size {
			return &s, c, nil
		}
	}
	if len(m.Uploads()) >= MaxSessions {
		return nil, c, fmt.Errorf("%d uploads are open already: finish or cancel one first", MaxSessions)
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, c, err
	}
	f, err := m.Lib.NewPart(c.Name)
	if err != nil {
		return nil, c, err
	}
	part := filepath.Base(f.Name())
	f.Close()
	s := &Session{ID: hex.EncodeToString(b), Request: u, Part: part, Created: time.Now()}
	path, err := m.sessionFile(s.ID)
	if err == nil {
		err = s.save(path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return nil, c, err
	}
	return s, c, nil
}

// UploadState is the state of one open upload.
func (m *Manager) UploadState(id string) (*Session, error) {
	s, _, err := m.loadSession(id)
	return s, err
}

// UploadChunk adds a piece of the file at the given place. The place must be exactly where the server's copy ends: otherwise it says where that is (an OffsetError) and
// nothing is written. A piece that breaks off still counts for the bytes that arrived.
func (m *Manager) UploadChunk(id string, offset int64, body io.Reader) (*Session, error) {
	lock := lockOf(id)
	if !lock.TryLock() {
		return nil, errors.New("another piece of this upload is being received")
	}
	defer lock.Unlock()
	s, path, err := m.loadSession(id)
	if err != nil {
		return nil, err
	}
	part, err := m.partPath(s)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(part)
	if err != nil {
		return nil, ErrNoSession
	}
	if st.Size() < s.Received {
		return nil, errors.New("the part file is shorter than the upload record says: cancel this upload and start it again")
	}
	if st.Size() > s.Received { // a piece that was cut off after the record was written: the file is cut back to what the record says
		if err := os.Truncate(part, s.Received); err != nil {
			return nil, err
		}
	}
	if offset != s.Received {
		return s, &OffsetError{s.Received}
	}
	if err := m.Lib.EnoughSpace(s.Request.Size - s.Received); err != nil {
		return s, err
	}
	f, err := os.OpenFile(part, os.O_WRONLY, 0o644) // (not O_APPEND: on Windows a file opened for appending cannot be cut back)
	if err != nil {
		return nil, ErrNoSession
	}
	if _, err := f.Seek(s.Received, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	remaining := s.Request.Size - s.Received
	n, copyErr := io.Copy(f, io.LimitReader(body, min(remaining, MaxChunk)+1))
	if n > remaining {
		n = remaining
		copyErr = errors.New("more data arrived than the size that was announced")
		_ = f.Truncate(s.Received + n)
	} else if n > MaxChunk {
		copyErr = fmt.Errorf("a piece is at most %d MB", MaxChunk>>20)
		n = MaxChunk
		_ = f.Truncate(s.Received + n)
	}
	if err := f.Sync(); err != nil && copyErr == nil {
		copyErr = err
	}
	f.Close()
	s.Received += n
	if serr := s.save(path); serr != nil && copyErr == nil {
		copyErr = serr
	}
	if copyErr == nil {
		// the start of the file says what it is: a web page saved as ubuntu.iso is refused as soon as its first bytes are here, not after 5 GB
		if need := int64(library.NeedBytes(s.Request.Name)); s.Received-n < need && s.Received >= need {
			if serr := m.sniffPart(s, part); serr != nil {
				return s, serr
			}
		}
	}
	return s, copyErr
}

func (m *Manager) sniffPart(s *Session, part string) error {
	f, err := os.Open(part)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, library.NeedBytes(s.Request.Name))
	k, _ := io.ReadFull(f, head)
	if serr := library.Sniff(s.Request.Name, head[:k]); serr != nil && !errors.Is(serr, library.ErrTooShort) {
		return fmt.Errorf("this file is not what its name says: %w", serr)
	}
	return nil
}

// FinishUpload checks the whole file and puts it in the library; the session ends (also when the file is refused: it cannot be repaired by sending more).
func (m *Manager) FinishUpload(id string) (dest string, warnings []string, err error) {
	lock := lockOf(id)
	if !lock.TryLock() {
		return "", nil, errors.New("a piece of this upload is being received")
	}
	defer lock.Unlock()
	s, path, err := m.loadSession(id)
	if err != nil {
		return "", nil, err
	}
	part, err := m.partPath(s)
	if err != nil {
		return "", nil, err
	}
	end := func() {
		_ = os.Remove(path)
		sessionLocks.Delete(id)
	}
	if s.Received != s.Request.Size {
		return "", nil, fmt.Errorf("the upload is incomplete: %d of %d bytes are here", s.Received, s.Request.Size)
	}
	c := m.CheckUpload(s.Request)
	if len(c.Problems) > 0 { // (a file of that name has appeared since the upload was started, or the library has no room left)
		return "", nil, errors.New(strings.Join(c.Problems, "; "))
	}
	fail := func(e error) (string, []string, error) {
		_ = os.Remove(part)
		end()
		return "", nil, e
	}
	if st, serr := os.Stat(part); serr != nil || st.Size() != s.Request.Size {
		return fail(errors.New("the part file does not have the size it should: start the upload again"))
	}
	if serr := m.sniffPart(s, part); serr != nil {
		return fail(serr)
	}
	sha, herr := library.HashFile(part, nil)
	if herr != nil {
		return "", nil, herr
	}
	if c.Expected != "" && sha != c.Expected {
		return fail(fmt.Errorf("the checksum does not match: the file has %s, expected %s (the upload is damaged or it is another file)", short(sha), short(c.Expected)))
	}
	if perr := m.Lib.Place(part, c.Dest); perr != nil {
		return "", nil, fmt.Errorf("the file cannot be put in the library: %w", perr) // (the upload stays: the name can be changed by starting another)
	}
	end()
	if idx := m.Index(); idx != nil {
		_ = idx.Set(c.Dest, library.Record{SHA256: sha, Added: time.Now()})
		for _, d := range idx.Duplicates("", "", 0, sha) {
			if d.Path != c.Dest {
				warnings = append(warnings, "the content is the same as "+d.Path)
			}
		}
	}
	m.logf("upload: %s is in the library (received in pieces)", c.Dest)
	return c.Dest, warnings, nil
}

// AbortUpload ends an upload and removes what was received.
func (m *Manager) AbortUpload(id string) error {
	lock := lockOf(id)
	lock.Lock()
	defer lock.Unlock()
	s, path, err := m.loadSession(id)
	if err != nil {
		return err
	}
	if part, perr := m.partPath(s); perr == nil {
		_ = os.Remove(part)
	}
	_ = os.Remove(path)
	sessionLocks.Delete(id)
	return nil
}

// Uploads lists the open uploads, the one touched last first.
func (m *Manager) Uploads() []Session {
	dir, err := m.Lib.IncomingDir()
	if err != nil {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.session.json"))
	var out []Session
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".session.json")
		if s, _, err := m.loadSession(id); err == nil {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// UploadParts are the names of the part files that open uploads own (the clean-up leaves them alone).
func (m *Manager) UploadParts() map[string]bool {
	out := map[string]bool{}
	for _, s := range m.Uploads() {
		out[s.Part] = true
	}
	return out
}

// ExpireUploads ends the uploads that were not touched for SessionTTL and removes what they received. It says how many.
func (m *Manager) ExpireUploads() int {
	n := 0
	for _, s := range m.Uploads() {
		if time.Since(s.Updated) > SessionTTL && m.AbortUpload(s.ID) == nil {
			m.logf("upload: %s was not touched for %d days: removed", s.Request.Name, int(SessionTTL.Hours()/24))
			n++
		}
	}
	return n
}
