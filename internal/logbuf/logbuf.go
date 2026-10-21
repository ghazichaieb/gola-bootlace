// Package logbuf keeps the last lines of the server's log in memory, so that the web interface can show what is happening without anyone opening
// a terminal. Every line has a number that only grows; a reader asks for "everything after number N".
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package logbuf

import (
	"fmt"
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
}

// New keeps the last keep lines (at least 100).
func New(keep int) *Buffer {
	return &Buffer{max: max(keep, 100), next: 1}
}

// Add stores one line.
func (b *Buffer) Add(text string) {
	text = strings.TrimRight(text, "\r\n")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, Line{N: b.next, Time: time.Now(), Text: text})
	b.next++
	if len(b.lines) > b.max {
		b.lines = append(b.lines[:0], b.lines[len(b.lines)-b.max:]...)
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
