package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
)

func TestTheHistoryOfEveryPcCanBeDownloadedAsCsvWithoutFormulas(t *testing.T) {
	r := newRig(t)
	r.login()
	now := time.Now()
	r.backend.clients = []httpd.Client{{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.5", LastSeen: now, History: []httpd.HistoryEvent{
		{Time: now.Add(-time.Hour), What: "asked for the menu"}, {Time: now, What: "=HYPERLINK(\"http://evil\")"}}}}
	code, body := r.raw("GET", "/api/pcs/history.csv", nil)
	text := string(body)
	if code != 200 || !strings.HasPrefix(text, "time,mac,name,address,what\n") {
		t.Fatalf("%d %q", code, text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "'=HYPERLINK") || !strings.Contains(lines[2], "asked for the menu") {
		t.Fatalf("newest first, and a cell that starts like a formula is quoted:\n%s", text)
	}
}
