package httpd

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func fetchAnswer(s *Server, from, method string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/answers/"+ansID+"/aa-bb-cc-dd-ee-01/user-data", nil)
	req.RemoteAddr = from + ":5555"
	s.answers(rec, req)
	return rec.Code
}

func menuAsk(s *Server, from, mac string) {
	req := httptest.NewRequest("GET", "/menu?mac="+mac+"&plat=efi", nil)
	req.RemoteAddr = from + ":4444"
	s.noteClient(req)
}

func TestAnAnswerFileIsOnlyGivenToAPCThatAskedForTheMenu(t *testing.T) {
	s, log := answerServer(t, nil)
	s.clients = map[string]*Client{} // nobody asked yet
	if fetchAnswer(s, "10.0.0.9", "GET") != 404 {
		t.Fatal("a PC that never asked for the menu was given the file")
	}
	if !strings.Contains(strings.Join(*log, "\n"), "never asked for the menu") {
		t.Fatalf("the refusal has a reason in the log: %v", *log)
	}
	menuAsk(s, "10.0.0.9", "aa:bb:cc:dd:ee:01")
	if fetchAnswer(s, "10.0.0.9", "GET") != 200 {
		t.Fatal("the PC that asked for the menu was refused")
	}
	// by default the address does not have to be the same: iPXE and the installer get different addresses from a router that leases by client identifier (seen with Ubuntu Server)
	if fetchAnswer(s, "10.0.0.77", "GET") != 200 {
		t.Fatal("a PC at another address than its menu was refused although the strict setting is off")
	}
	if got := strings.Join(*log, "\n"); !strings.Contains(got, "the PC asked for the menu from 10.0.0.9") {
		t.Fatalf("a file given at another address says so in the log: %s", got)
	}
	if err := s.Config.Update(func(c *config.Config) error { c.AnswersSameAddress = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if fetchAnswer(s, "10.0.0.77", "GET") != 404 {
		t.Fatal("the strict setting did not apply")
	}
	if got := strings.Join(*log, "\n"); !strings.Contains(got, "from 10.0.0.9, not from 10.0.0.77") {
		t.Fatalf("%s", got)
	}
	if fetchAnswer(s, "10.0.0.9", "GET") != 200 {
		t.Fatal("the strict setting refused the address that asked for the menu")
	}
	for _, line := range *log {
		if strings.Contains(line, "hunter2") {
			t.Fatalf("the text of the file is in the log: %q", line)
		}
	}
}

func TestAnAnswerFileStopsWhenTheWindowRunsOutAndTheMenuOpensItAgain(t *testing.T) {
	s, _ := answerServer(t, nil)
	s.clients["aa:bb:cc:dd:ee:01"].MenuAt = time.Now().Add(-answerWindow - time.Minute)
	if fetchAnswer(s, "10.0.0.9", "GET") != 404 {
		t.Fatal("a file was given after the window")
	}
	// fetching the files of an ISO from that address does not revive a window that ran out
	s.noteFetch("10.0.0.9:1", "Ubuntu")
	if fetchAnswer(s, "10.0.0.9", "GET") != 404 {
		t.Fatal("an ISO download opened a window that had run out")
	}
	menuAsk(s, "10.0.0.9", "aa:bb:cc:dd:ee:01")
	if fetchAnswer(s, "10.0.0.9", "GET") != 200 {
		t.Fatal("asking for the menu did not open the window")
	}
	// while the PC is fetching ISO files the window is kept open
	s.clients["aa:bb:cc:dd:ee:01"].MenuAt = time.Now().Add(-answerWindow + time.Minute)
	s.noteFetch("10.0.0.9:1", "Ubuntu")
	if got := time.Since(s.clients["aa:bb:cc:dd:ee:01"].MenuAt); got > time.Minute {
		t.Fatalf("the window was not kept open: %v", got)
	}
}

func TestAPCIsGivenAFewFilesPerMenuRequestAndThenTheAddressClosesUntilItAsksAgain(t *testing.T) {
	s, log := answerServer(t, nil)
	for i := 0; i < answerFetches; i++ {
		if got := fetchAnswer(s, "10.0.0.9", "GET"); got != 200 {
			t.Fatalf("fetch %d: %d", i+1, got)
		}
		if i < answerFetches-1 && fetchAnswer(s, "10.0.0.9", "HEAD") != 200 { // (a HEAD request is not counted)
			t.Fatalf("HEAD after fetch %d", i+1)
		}
	}
	// the 13th fetch is refused, and so is a HEAD
	if fetchAnswer(s, "10.0.0.9", "GET") != 404 || fetchAnswer(s, "10.0.0.9", "HEAD") != 404 {
		t.Fatal("the PC went on being given the file")
	}
	if got := strings.Join(*log, "\n"); !strings.Contains(got, "has to ask for the menu again") {
		t.Fatalf("%s", got)
	}
	menuAsk(s, "10.0.0.9", "aa:bb:cc:dd:ee:01")
	if fetchAnswer(s, "10.0.0.9", "GET") != 200 {
		t.Fatal("asking for the menu again did not open the address")
	}
}

func TestAMenuRequestFromAnotherAddressForTheSameMACIsLoggedAndTheLastAddressWinsWhenStrict(t *testing.T) {
	s, log := answerServer(t, func(c *config.Config) { c.AnswersSameAddress = true })
	menuAsk(s, "10.0.0.50", "aa:bb:cc:dd:ee:01")
	if !strings.Contains(strings.Join(*log, "\n"), "another host poses as it") {
		t.Fatalf("no warning: %v", *log)
	}
	if fetchAnswer(s, "10.0.0.9", "GET") != 404 || fetchAnswer(s, "10.0.0.50", "GET") != 200 {
		t.Fatal("the last address to ask for the menu is the one that is served")
	}
}

func TestTheGrantSurvivesTheSavedListOfPCs(t *testing.T) {
	s, _ := answerServer(t, nil)
	s.PCFile = t.TempDir() + "/pcs.json"
	s.saveClients()
	again := &Server{PCFile: s.PCFile}
	if err := again.LoadClients(); err != nil {
		t.Fatal(err)
	}
	c := again.clients["aa:bb:cc:dd:ee:01"]
	if c == nil || c.MenuIP != "10.0.0.9" || c.AnswersLeft != answerFetches || c.MenuAt.IsZero() {
		t.Fatalf("%+v", c)
	}
}
