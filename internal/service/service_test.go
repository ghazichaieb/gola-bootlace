// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package service

import (
	"errors"
	"net"
	"testing"
	"time"
)

// counter is a service that only counts its starts and stops.
type counter struct{ starts, stops int }

func (c *counter) def(id string, fail *error) Def {
	return Def{ID: id, Title: id, Start: func() (Instance, error) {
		if fail != nil && *fail != nil {
			return Instance{}, *fail
		}
		c.starts++
		return Instance{Stop: func() { c.stops++ }}, nil
	}}
}

func states(s *Supervisor) map[string]string {
	m := map[string]string{}
	for _, st := range s.Report().Services {
		m[st.ID] = st.State
	}
	return m
}

func TestStartStop(t *testing.T) {
	var a, b counter
	s := &Supervisor{}
	s.Add(a.def("a", nil))
	s.Add(b.def("b", nil))
	if s.Active() {
		t.Fatal("active before Start")
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !s.Active() || states(s)["a"] != Running || states(s)["b"] != Running {
		t.Fatalf("not running after Start: %v", states(s))
	}
	if err := s.Start(); err != nil || a.starts != 1 {
		t.Fatalf("a second Start must not start a running service again (starts=%d, err=%v)", a.starts, err)
	}
	s.Stop()
	if s.Active() || a.stops != 1 || b.stops != 1 || states(s)["a"] != Stopped {
		t.Fatalf("not stopped: %v stops=%d,%d", states(s), a.stops, b.stops)
	}
}

// A service that cannot start is reported and the others run; starting again retries only the one that failed.
func TestFailedServiceDoesNotStopTheOthers(t *testing.T) {
	var a, b counter
	problem := errors.New("port 445 is taken")
	s := &Supervisor{}
	s.Add(a.def("a", nil))
	s.Add(b.def("b", &problem))
	_ = s.Start()
	st := s.Report().Services
	if st[0].State != Running || st[1].State != Failed || st[1].Error != "port 445 is taken" {
		t.Fatalf("report: %+v", st)
	}
	problem = nil
	_ = s.Start()
	if a.starts != 1 || b.starts != 1 || states(s)["b"] != Running {
		t.Fatalf("retry: a=%d b=%d %v", a.starts, b.starts, states(s))
	}
}

func TestGateStopsAfterTheGraceAndStartsAgain(t *testing.T) {
	var a counter
	down := error(nil)
	s := &Supervisor{Gate: func() error { return down }, Grace: 50 * time.Millisecond}
	s.Add(a.def("a", nil))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	down = errors.New("the library is not available")
	s.Reconcile()
	if a.stops != 0 || !s.Active() {
		t.Fatal("stopped at once, without the grace")
	}
	time.Sleep(70 * time.Millisecond)
	s.Reconcile()
	if a.stops != 1 || s.Active() || s.Report().Blocked == "" {
		t.Fatalf("not stopped after the grace: stops=%d blocked=%q", a.stops, s.Report().Blocked)
	}
	down = nil
	s.Reconcile()
	if a.starts != 2 || !s.Active() || s.Report().Blocked != "" {
		t.Fatalf("not started again: starts=%d", a.starts)
	}
}

// A failure that ends before the grace is over changes nothing, and the next failure counts from its own start.
func TestShortGateFailureIsForgotten(t *testing.T) {
	var a counter
	down := error(nil)
	s := &Supervisor{Gate: func() error { return down }, Grace: 60 * time.Millisecond}
	s.Add(a.def("a", nil))
	_ = s.Start()
	down = errors.New("gone")
	s.Reconcile()
	time.Sleep(40 * time.Millisecond)
	down = nil
	s.Reconcile()
	down = errors.New("gone again")
	time.Sleep(40 * time.Millisecond)
	s.Reconcile()
	if a.stops != 0 {
		t.Fatal("two short failures were counted as one long one")
	}
}

// Wanted at the start of the program while the library is not there yet: it starts when the library appears.
func TestStartWhileBlockedWaits(t *testing.T) {
	var a counter
	down := errors.New("no library yet")
	s := &Supervisor{Gate: func() error { return down }}
	s.Add(a.def("a", nil))
	if err := s.Start(); err == nil {
		t.Fatal("Start did not report the gate")
	}
	if s.Active() || a.starts != 0 {
		t.Fatal("started although the gate was shut")
	}
	down = nil
	s.Reconcile()
	if a.starts != 1 || !s.Active() {
		t.Fatal("did not start when the library appeared")
	}
}

func TestTrackedListener(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := Track(raw)
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if got := l.Sessions(); len(got) != 1 || got[0] != "127.0.0.1" {
		t.Fatalf("sessions: %v", got)
	}
	l.Shutdown()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("the connection stayed open after Shutdown")
	}
	if got := l.Sessions(); len(got) != 0 {
		t.Fatalf("a closed connection is still listed: %v", got)
	}
	_ = server.Close()
	if _, err := net.Dial("tcp", raw.Addr().String()); err == nil {
		t.Fatal("still listening after Shutdown")
	}
}
