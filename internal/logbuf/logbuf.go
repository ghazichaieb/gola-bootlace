// Package logbuf keeps the last lines of the server's log in memory, so that the web interface can show what is happening without anyone opening
// a terminal. Every line has a number that only grows; a reader asks for "everything after number N".
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package logbuf

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Line is one line of the log.
type Line struct {
	N    int       `json:"n"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

// Buffer is safe for use from several goroutines.
type Buffer struct {
	max int

	mu    sync.Mutex
	lines []Line
	next  int

	file     *os.File // the optional log file (see SetFile)
	path     string
	size     int64
	fileSize int64 // the size at which the file is rotated
}

// New keeps the last keep lines (at least 100).
func New(keep int) *Buffer {
	return &Buffer{max: max(keep, 100), next: 1}
}

// Add stores one line in memory and, when there is one, in the log file.
func (b *Buffer) Add(text string) { b.add(text, true) }

// AddPrivate stores one line in memory only: for a line that holds a secret (the password made up at the first start), which must never reach a file.
func (b *Buffer) AddPrivate(text string) { b.add(text, false) }

func (b *Buffer) add(text string, toFile bool) {
	text = strings.TrimRight(text, "\r\n")
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.lines = append(b.lines, Line{N: b.next, Time: now, Text: text})
	b.next++
	if len(b.lines) > b.max {
		b.lines = append(b.lines[:0], b.lines[len(b.lines)-b.max:]...)
	}
	if toFile && b.file != nil {
		b.write(now, text)
	}
}

// SetFile also writes every line to the file at path, which is kept at about limit bytes: when the file is full it becomes path+".1" (the one older
// file that is kept) and a new one is started. A limit under 64 KB is raised to it.
func (b *Buffer) SetFile(path string, limit int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if b.file != nil {
		b.file.Close()
	}
	b.file, b.path, b.size, b.fileSize = f, path, info.Size(), max(limit, 64<<10)
	return nil
}

// write adds one line to the file; the caller holds the lock. A file that cannot be written is given up: the log in memory goes on.
func (b *Buffer) write(t time.Time, text string) {
	line := t.Format("2006-01-02 15:04:05") + "  " + text + "\n"
	if b.size+int64(len(line)) > b.fileSize && b.size > 0 {
		b.file.Close()
		_ = os.Remove(b.path + ".1")
		_ = os.Rename(b.path, b.path+".1")
		f, err := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o600)
		if err != nil {
			b.file = nil
			return
		}
		b.file, b.size = f, 0
	}
	n, err := b.file.WriteString(line)
	b.size += int64(n)
	if err != nil {
		b.file.Close()
		b.file = nil
	}
}

// Dump writes the whole log to w, oldest first: the file and the older file when there is a log file, else the lines kept in memory.
func (b *Buffer) Dump(w io.Writer) error {
	b.mu.Lock()
	path, has := b.path, b.file != nil
	b.mu.Unlock()
	if has {
		for _, name := range []string{path + ".1", path} {
			f, err := os.Open(name)
			if err != nil {
				continue
			}
			_, err = io.Copy(w, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	lines, _ := b.After(0, 0)
	for _, l := range lines {
		if _, err := fmt.Fprintf(w, "%s  %s\n", l.Time.Format("2006-01-02 15:04:05"), l.Text); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the log file, if there is one.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file != nil {
		b.file.Close()
		b.file = nil
	}
}

// Logf is Add with a format, in the shape the rest of the program uses for logging.
func (b *Buffer) Logf(format string, args ...any) { b.Add(fmt.Sprintf(format, args...)) }

// After returns the lines numbered higher than n (all that are still kept when n is 0), at most limit of them (the newest), and the number of
// the last line stored so far.
func (b *Buffer) After(n, limit int) ([]Line, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	last := b.next - 1
	var out []Line
	for _, l := range b.lines {
		if l.N > n {
			out = append(out, l)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return append([]Line{}, out...), last
}
