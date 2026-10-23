package httpd

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPCsHistoryNotesWhatHappenedAndMergesRepeats(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/menu?mac=aa:bb:cc:dd:ee:01&ip=10.0.0.9", nil)
	req.RemoteAddr = "10.0.0.9:5555"
	s.noteClient(req)
	s.setAction("aa:bb:cc:dd:ee:01", "rule: pfSense")
	for i := 0; i < 5; i++ { // an ISO is fetched in many requests: one event
		s.noteFetch("10.0.0.9:6000", "pfSense")
	}
	c := s.Clients()[0]
	var what []string
	for _, e := range c.History {
		what = append(what, e.What)
	}
	if got := strings.Join(what, " | "); got != "asked for the menu | rule: pfSense | booting pfSense" {
		t.Fatalf("%s", got)
	}
	// asking for the menu again after that is a new event; asking twice in a row within two minutes is one
	s.noteClient(req)
	s.noteClient(req)
	if n := len(s.Clients()[0].History); n != 4 {
		t.Fatalf("%d events", n)
	}
}

func TestHistoryKeepsTheLastFortyAndCleansControlCharacters(t *testing.T) {
	var c Client
	now := time.Now()
	for i := 0; i < 60; i++ {
		c.note(fmt.Sprintf("event %d\r\n", i), now.Add(time.Duration(i)*time.Hour))
	}
	if len(c.History) != maxHistory || c.History[0].What != "event 20" || c.History[len(c.History)-1].What != "event 59" {
		t.Fatalf("%d: first %q last %q", len(c.History), c.History[0].What, c.History[len(c.History)-1].What)
	}
	if strings.ContainsAny(c.History[0].What, "\r\n") {
		t.Fatal("control characters must not reach the history")
	}
}
