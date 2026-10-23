// A listener that remembers its connections, so that stopping a service also ends the PCs' connections to it and the web interface can say who is connected.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package service

import (
	"net"
	"sort"
	"sync"
)

// Listener wraps a listener and keeps the connections it accepted until they are closed.
type Listener struct {
	net.Listener

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// Track wraps l.
func Track(l net.Listener) *Listener {
	return &Listener{Listener: l, conns: map[net.Conn]struct{}{}}
}

// Accept returns the next connection, which is forgotten again when it is closed.
func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	t := &conn{Conn: c, owner: l}
	l.mu.Lock()
	l.conns[t] = struct{}{}
	l.mu.Unlock()
	return t, nil
}

// Sessions are the addresses connected right now, once each.
func (l *Listener) Sessions() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	for c := range l.conns {
		if host, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
			seen[host] = true
		}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Shutdown stops listening and closes every connection that is open.
func (l *Listener) Shutdown() {
	_ = l.Listener.Close()
	l.mu.Lock()
	open := make([]net.Conn, 0, len(l.conns))
	for c := range l.conns {
		open = append(open, c)
	}
	l.mu.Unlock()
	for _, c := range open {
		_ = c.Close()
	}
}

type conn struct {
	net.Conn
	owner *Listener
}

func (c *conn) Close() error {
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return c.Conn.Close()
}
