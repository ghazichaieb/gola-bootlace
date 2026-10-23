// A password in front of an ISO in the menu: the menu script asks for it and the server checks it before the script goes on. This keeps a menu that anyone at the PC can use
// from starting a disk wipe by accident; it is not a lock against someone with a network cable: the password crosses the local network in the clear, and the ISO file itself
// can still be downloaded by anyone who knows its address.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	authFailsAllowed = 5
	authLockFor      = time.Minute
)

type authFailure struct {
	count int
	until time.Time
}

// guardLines are the iPXE lines that come before the commands of a protected ISO in the menu (a rule that sends a PC straight to it does not use them: that is a
// deliberate start). The password is typed with iPXE's own login prompt, which hides it; the user name is not used.
func (s *Server) guardLines(id string) []string {
	check := s.Base + "/auth?iso=" + url.QueryEscape(id) + "&mac=${mac}&pw=${password:uristring}"
	return []string{
		"echo This entry asks for a password. Press Enter at the user name, then type the password.",
		"login || goto denied",
		"imgfetch --name auth " + check + " || goto denied",
		"clear password",
		"clear username",
	}
}

// authBoot is /auth?iso=<id>&pw=<password>: 200 when the password is the one set for that ISO, 403 when it is not. A wrong guess five times in a row from one address locks
// that address out for a minute.
func (s *Server) authBoot(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	s.authMu.Lock()
	if s.authFails == nil {
		s.authFails = map[string]*authFailure{}
	}
	f := s.authFails[host]
	if f != nil && now.Before(f.until) {
		s.authMu.Unlock()
		http.Error(w, "too many wrong passwords: wait a minute", http.StatusTooManyRequests)
		return
	}
	s.authMu.Unlock()
	q := r.URL.Query()
	id, pw := q.Get("iso"), q.Get("pw")
	hash := s.settings().BootPasswords[id]
	ok := hash != "" && s.CheckPassword != nil && pw != "" && s.CheckPassword(hash, pw)
	s.authMu.Lock()
	if ok {
		delete(s.authFails, host)
	} else {
		if f == nil {
			f = &authFailure{}
			s.authFails[host] = f
		}
		if f.count++; f.count >= authFailsAllowed {
			f.count, f.until = 0, now.Add(authLockFor)
		}
	}
	s.authMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if !ok {
		s.logf("menu: a wrong password for an ISO from %s", host)
		http.Error(w, "wrong password", http.StatusForbidden)
		return
	}
	s.logf("menu: the password for an ISO was right (%s)", host)
	_, _ = w.Write([]byte("ok\n"))
}
