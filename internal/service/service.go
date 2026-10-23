// Package service runs the parts of the server that PCs talk to (DHCP, TFTP, the boot menu, the network disk, iSCSI, the Windows share) as one group that the web
// interface can stop and start again. A service that cannot start (its port is taken) is reported, it does not end the program. The group also stops by itself
// while the library is not available, and starts again when it is back.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package service

import (
	"sort"
	"sync"
	"time"
)

// Instance is a running service.
type Instance struct {
	Stop     func()          // closes what Start opened, connections included, and returns when that is done
	Sessions func() []string // the PCs (addresses) that are using the service right now; nil when it cannot tell
}

// Def describes one service.
type Def struct {
	ID     string
	Title  string
	Detail string // for the person: the ports, for example "TCP 10809"
	Start  func() (Instance, error)
}

// State of a service.
const (
	Stopped = "stopped"
	Running = "running"
	Failed  = "failed"
)

// Status is what the web interface shows for one service.
type Status struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	State    string   `json:"state"`
	Error    string   `json:"error,omitempty"`
	Sessions []string `json:"sessions"`
}

// Report is the state of the whole group.
type Report struct {
	Wanted   bool     `json:"wanted"`            // the server is meant to run (started by the person or at the start of the program)
	Blocked  string   `json:"blocked,omitempty"` // why it cannot run now (the library is not available)
	Services []Status `json:"services"`
}

// Supervisor owns the services.
type Supervisor struct {
	// Gate says whether the services may run now (nil: always); the error is shown to the person. It is asked at every Start and Reconcile.
	Gate func() error
	// Grace is how long the gate has to keep failing before running services are stopped by Reconcile.
	Grace time.Duration
	Logf  func(format string, args ...any)

	mu           sync.Mutex
	defs         []Def
	running      map[string]Instance
	errs         map[string]string
	wanted       bool
	blocked      string
	failingSince time.Time
}

// Add registers a service. It is not started.
func (s *Supervisor) Add(d Def) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defs = append(s.defs, d)
}

func (s *Supervisor) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Start makes the server run: it starts every service that is not running. It returns the gate's error when the library is not available; the server is then
// still wanted, and Reconcile starts it when the library is back.
func (s *Supervisor) Start() error {
	gateErr := s.gate() // (asked before the lock is taken: a library that is slow to answer must not hold up the status page)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted = true
	return s.startLocked(gateErr)
}

// gate asks the gate.
func (s *Supervisor) gate() error {
	if s.Gate == nil {
		return nil
	}
	return s.Gate()
}

func (s *Supervisor) startLocked(gateErr error) error {
	if gateErr != nil {
		s.blocked = gateErr.Error()
		return gateErr
	}
	s.blocked = ""
	if s.running == nil {
		s.running, s.errs = map[string]Instance{}, map[string]string{}
	}
	for _, d := range s.defs {
		if _, ok := s.running[d.ID]; ok {
			continue
		}
		inst, err := d.Start()
		if err != nil {
			s.errs[d.ID] = err.Error()
			s.logf("%s: cannot start: %v", d.Title, err)
			continue
		}
		delete(s.errs, d.ID)
		s.running[d.ID] = inst
	}
	return nil
}

// Stop makes the server stop: every service is closed, and it stays stopped until Start.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted = false
	s.blocked = ""
	s.stopLocked()
}

func (s *Supervisor) stopLocked() {
	for i := len(s.defs) - 1; i >= 0; i-- { // the last to start is the first to stop
		d := s.defs[i]
		if inst, ok := s.running[d.ID]; ok {
			inst.Stop()
			delete(s.running, d.ID)
		}
	}
	s.errs = map[string]string{}
}

// Reconcile follows the gate: when the library goes away the services stop (the server stays wanted), and when it is back they start again. Call it now and then.
func (s *Supervisor) Reconcile() {
	if s.Gate == nil {
		return
	}
	gateErr := s.gate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.wanted {
		return
	}
	if err := gateErr; err != nil {
		now := time.Now()
		if s.failingSince.IsZero() {
			s.failingSince = now
		}
		// a short failure (a network mount that stalls for a moment) must not end the installs that are running: the PCs retry for a long time
		if len(s.running) > 0 && now.Sub(s.failingSince) >= s.Grace {
			s.logf("stopping the server: %v", err)
			s.stopLocked()
		}
		if len(s.running) == 0 {
			s.blocked = err.Error()
		}
		return
	}
	s.failingSince = time.Time{}
	if s.blocked != "" {
		s.logf("the library is back: starting the server")
		_ = s.startLocked(nil)
	}
}

// Active says whether PCs are being served: the server is wanted and nothing blocks it. The web server answers PCs only then.
func (s *Supervisor) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wanted && s.blocked == ""
}

// Report is the state of every service.
func (s *Supervisor) Report() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Report{Wanted: s.wanted, Blocked: s.blocked, Services: []Status{}}
	for _, d := range s.defs {
		st := Status{ID: d.ID, Title: d.Title, Detail: d.Detail, State: Stopped, Sessions: []string{}}
		if inst, ok := s.running[d.ID]; ok {
			st.State = Running
			if inst.Sessions != nil {
				st.Sessions = append(st.Sessions, inst.Sessions()...)
			}
		} else if e, ok := s.errs[d.ID]; ok {
			st.State, st.Error = Failed, e
		}
		r.Services = append(r.Services, st)
	}
	return r
}

// Sessions are the PCs using any service right now, once each.
func (s *Supervisor) Sessions() []string {
	seen := map[string]bool{}
	for _, st := range s.Report().Services {
		for _, a := range st.Sessions {
			seen[a] = true
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}
