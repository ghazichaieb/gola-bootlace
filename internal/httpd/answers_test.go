package httpd

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

const (
	ansID  = "0123456789ab"
	ansISO = "abc1234567"
	ansMAC = "aa:bb:cc:dd:ee:01"
)

func TestTheKernelLineIsPointedAtTheAnswerFileOfEachKind(t *testing.T) {
	base := "http://s/answers/" + ansID + "/aa-bb-cc-dd-ee-01/"
	for kind, want := range map[string]string{
		"preseed":     "auto=true priority=critical cdrom-detect/retry=false url=" + base + "preseed.cfg",
		"autoinstall": "autoinstall ds=nocloud-net;s=" + base,
		"kickstart":   "inst.ks=" + base + "ks.cfg",
		"autoyast":    "autoyast=" + base + "autoinst.xml",
		"unknown":     "",
	} {
		if got := answerArgs(kind, base); got != want {
			t.Errorf("%s: %q, want %q", kind, got, want)
		}
	}
}

func TestAnswerArgumentsGoBeforeTheSeparatorOfDebianAndUbuntuAndNeverIntoALineWithoutAKernel(t *testing.T) {
	cmds := []string{"echo hi", "kernel http://s/vmlinuz vga=788 --- quiet", "initrd http://s/initrd", "boot"}
	got := insertArgs(cmds, "auto=true url=http://s/p")
	if got[1] != "kernel http://s/vmlinuz vga=788 auto=true url=http://s/p --- quiet" || got[0] != cmds[0] || got[2] != cmds[2] {
		t.Fatalf("%q", got[1])
	}
	if cmds[1] != "kernel http://s/vmlinuz vga=788 --- quiet" {
		t.Fatal("the shared commands must not change")
	}
	if got := insertArgs([]string{"kernel http://s/linux ip=dhcp"}, "inst.ks=x"); got[0] != "kernel http://s/linux ip=dhcp inst.ks=x" {
		t.Fatalf("%q", got[0])
	}
	disk := []string{"sanboot --drive 0x80 x"}
	if got := insertArgs(disk, "inst.ks=x"); got[0] != disk[0] {
		t.Fatalf("%q", got)
	}
}

func answerServer(t *testing.T, mutate func(*config.Config)) (*Server, *[]string) {
	t.Helper()
	store, _, err := config.Open(filepath.Join(t.TempDir(), "c.json"), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error {
		c.Answers = map[string]config.AnswerFile{ansID: {Name: "Lab", Kind: "autoinstall", Content: "#cloud-config\nautoinstall:\n  identity:\n    hostname: {{hostname}}\n    password: hunter2\n  # {{mac}} {{ip}} {{serial}} {{unknown}}\n"}}
		c.ISOAnswers = map[string]string{ansISO: ansID}
		c.PCNames = map[string]string{ansMAC: "Front Desk #3"}
		if mutate != nil {
			mutate(c)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var log []string
	s := &Server{Base: "http://10.0.0.1:8080", Config: store, Logf: func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }}
	s.clients = map[string]*Client{ansMAC: {MAC: ansMAC, Serial: "SN 42/x"}}
	return s, &log
}

func TestAnAnswerFileIsMadeForTheFetchingPCAndItsTextIsNeverLogged(t *testing.T) {
	s, log := answerServer(t, nil)
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "10.0.0.9:5555"
		s.answers(rec, req)
		return rec.Code, rec.Body.String()
	}
	code, body := get("/answers/" + ansID + "/aa-bb-cc-dd-ee-01/user-data")
	if code != 200 || !strings.Contains(body, "hostname: front-desk-3") || !strings.Contains(body, "aa:bb:cc:dd:ee:01 10.0.0.9 SN42x {{unknown}}") || !strings.Contains(body, "password: hunter2") {
		t.Fatalf("%d\n%s", code, body)
	}
	if code, body := get("/answers/" + ansID + "/aa-bb-cc-dd-ee-01/meta-data"); code != 200 || body != "instance-id: aa-bb-cc-dd-ee-01\nlocal-hostname: front-desk-3\n" {
		t.Fatalf("meta-data: %d %q", code, body)
	}
	if code, body := get("/answers/" + ansID + "/aa-bb-cc-dd-ee-01/vendor-data"); code != 200 || !strings.HasPrefix(body, "#cloud-config") {
		t.Fatalf("vendor-data: %d %q", code, body)
	}
	// a PC with no name gets pc- and the end of its address
	if _, body := get("/answers/" + ansID + "/aa-bb-cc-dd-ee-99/user-data"); !strings.Contains(body, "hostname: pc-ddee99") {
		t.Fatalf("%s", body)
	}
	for _, line := range *log {
		if strings.Contains(line, "hunter2") || strings.Contains(line, "autoinstall:") {
			t.Fatalf("the text of the file is in the log: %q", line)
		}
	}
	if len(*log) == 0 || !strings.Contains((*log)[0], "served to") {
		t.Fatalf("the log says who was given a file: %v", *log)
	}
}

func TestAnAnswerFileIsOnlyGivenWhenItIsAttachedAndTheAddressIsRight(t *testing.T) {
	s, _ := answerServer(t, nil)
	status := func(path string) int {
		rec := httptest.NewRecorder()
		s.answers(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code
	}
	good := "/answers/" + ansID + "/aa-bb-cc-dd-ee-01/user-data"
	if status(good) != 200 {
		t.Fatal("setup")
	}
	for name, path := range map[string]string{
		"another id":     "/answers/ffffffffffff/aa-bb-cc-dd-ee-01/user-data",
		"not a MAC":      "/answers/" + ansID + "/zz/user-data",
		"not its file":   "/answers/" + ansID + "/aa-bb-cc-dd-ee-01/ks.cfg",
		"missing part":   "/answers/" + ansID + "/user-data",
		"path trickery":  "/answers/" + ansID + "/aa-bb-cc-dd-ee-01/../../x",
		"too many parts": "/answers/" + ansID + "/aa-bb-cc-dd-ee-01/user-data/x",
	} {
		if status(path) != 404 {
			t.Errorf("%s was served", name)
		}
	}
	// detached: nobody is given it any more
	if err := s.Config.Update(func(c *config.Config) error { c.ISOAnswers = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if status(good) != 404 {
		t.Error("a file that is attached to nothing was served")
	}
	// attached to one PC only
	if err := s.Config.Update(func(c *config.Config) error {
		c.PCOptions = map[string]config.PCOptions{ansMAC: {Answer: ansID}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if status(good) != 200 {
		t.Error("a file attached to a PC is given")
	}
}

func TestAnEntryGetsTheFileOfItsPCOrItsIsoAndNoneKeepsItAway(t *testing.T) {
	s, _ := answerServer(t, func(c *config.Config) {
		c.Answers["ba9876543210"] = config.AnswerFile{Name: "Other", Kind: "preseed", Content: "d-i x string y"}
		c.PCOptions = map[string]config.PCOptions{"aa:bb:cc:dd:ee:02": {Answer: "ba9876543210"}, "aa:bb:cc:dd:ee:03": {Answer: config.NoAnswer}}
	})
	cfg := s.settings()
	entry := menu.Entry{Name: "X", Kind: "Debian installer", Commands: []string{"kernel http://s/vmlinuz vga=788 --- quiet", "boot"}}
	line := func(mac string) string {
		return s.entryForPC(cfg, ansISO, entry, cfg.PCOptions[mac], mac).Commands[0]
	}
	if got := line("aa:bb:cc:dd:ee:09"); !strings.Contains(got, "autoinstall ds=nocloud-net;s=http://10.0.0.1:8080/answers/"+ansID+"/aa-bb-cc-dd-ee-09/ ---") {
		t.Errorf("the ISO's file for any PC: %q", got)
	}
	if got := line("aa:bb:cc:dd:ee:02"); !strings.Contains(got, "auto=true priority=critical cdrom-detect/retry=false url=http://10.0.0.1:8080/answers/ba9876543210/aa-bb-cc-dd-ee-02/preseed.cfg hostname=pc-ddee02 netcfg/hostname=pc-ddee02 ---") || strings.Contains(got, "autoinstall") {
		t.Errorf("the PC's own file wins: %q", got)
	}
	if got := line("aa:bb:cc:dd:ee:03"); got != entry.Commands[0] {
		t.Errorf("none keeps the file away: %q", got)
	}
	if other := s.entryForPC(cfg, "ffffffffff", entry, config.PCOptions{}, "aa:bb:cc:dd:ee:09").Commands[0]; other != entry.Commands[0] {
		t.Errorf("an ISO with no file gets none: %q", other)
	}
	if entry.Commands[0] != "kernel http://s/vmlinuz vga=788 --- quiet" {
		t.Fatal("the shared entry must not change")
	}
}
