package httpd

import (
	"encoding/xml"
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

const winID = "00ffeeddcc99"

func winServer(t *testing.T) *Server {
	t.Helper()
	s, _ := answerServer(t, func(c *config.Config) {
		c.Answers[winID] = config.AnswerFile{Name: "Shop PCs", Kind: "unattend", Content: "<ComputerName>{{computername}}</ComputerName><!-- {{name}} -->\n<Password>\n<Value>c2VjcmV0</Value>\n</Password>\n"}
		c.ISOAnswers = map[string]string{"a1b2c3d4e5": winID}
	})
	return s
}

func TestAWindowsAnswerIsMadeForThePCThatReadsItAndIsCountedOnlyWhenItIsRead(t *testing.T) {
	s := winServer(t)
	data, ok := s.WindowsAnswer("a1b2c3d4e5", "AA-BB-CC-DD-EE-01", "autounattend.xml", false)
	if !ok || !strings.Contains(string(data), "<ComputerName>front-desk-3</ComputerName>") || !strings.Contains(string(data), "<!-- Front Desk #3 -->") {
		t.Fatalf("%v %q", ok, data)
	}
	if got := s.clients["aa:bb:cc:dd:ee:01"].AnswersLeft; got != answerFetches {
		t.Fatalf("looking is not counted: %d left", got)
	}
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", true); !ok || s.clients["aa:bb:cc:dd:ee:01"].AnswersLeft != answerFetches-1 {
		t.Fatal("reading is counted once")
	}
	if got := s.clients["aa:bb:cc:dd:ee:01"].LastAction; !strings.Contains(got, "unattended install: Shop PCs") {
		t.Fatalf("the PC's history says nothing: %q", got)
	}
	for name, args := range map[string][3]string{
		"another file name":     {"a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "ks.cfg"},
		"an ISO without file":   {"ffffffffff", "aa:bb:cc:dd:ee:01", "autounattend.xml"},
		"not a MAC":             {"a1b2c3d4e5", "zz", "autounattend.xml"},
		"a PC that never asked": {"a1b2c3d4e5", "aa:bb:cc:dd:ee:77", "autounattend.xml"},
	} {
		if _, ok := s.WindowsAnswer(args[0], args[1], args[2], false); ok {
			t.Errorf("%s was served", name)
		}
	}
	// a PC's own choice wins, and "none" keeps Bootlace's file away from it
	if err := s.Config.Update(func(c *config.Config) error {
		c.PCOptions = map[string]config.PCOptions{"aa:bb:cc:dd:ee:01": {Answer: config.NoAnswer}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); ok {
		t.Error("none did not keep the file away")
	}
	// an answer file of another kind named for the PC is not a Windows answer
	if err := s.Config.Update(func(c *config.Config) error {
		c.PCOptions = map[string]config.PCOptions{"aa:bb:cc:dd:ee:01": {Answer: ansID}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); ok {
		t.Error("a cloud-init file was given to Windows Setup")
	}
}

func TestAWindowsAnswerFollowsTheSameLimitsAsTheWebAddress(t *testing.T) {
	s := winServer(t)
	for i := 0; i < answerFetches; i++ {
		if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", true); !ok {
			t.Fatalf("read %d refused", i+1)
		}
	}
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); ok {
		t.Fatal("the limit does not apply to the share")
	}
	// the menu (or a native boot) opens it again, also with the strict address setting (the share cannot see the address)
	if err := s.Config.Update(func(c *config.Config) error { c.AnswersSameAddress = true; return nil }); err != nil {
		t.Fatal(err)
	}
	s.grantAnswers("aa:bb:cc:dd:ee:01")
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", true); !ok {
		t.Fatal("a native start did not open the share's answer file")
	}
}

func TestWordsPutIntoAnXmlAnswerAreEscapedAndComputerNamesFitWindows(t *testing.T) {
	s := winServer(t)
	if err := s.Config.Update(func(c *config.Config) error { c.PCNames["aa:bb:cc:dd:ee:01"] = `Tom & "Jerry" <x>`; return nil }); err != nil {
		t.Fatal(err)
	}
	data, _ := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false)
	if !strings.Contains(string(data), "<!-- Tom &amp; &#34;Jerry&#34; &lt;x&gt; -->") {
		t.Fatalf("%s", data)
	}
	for in, want := range map[string]string{"front-desk-3": "front-desk-3", "a-very-long-host-name-here": "a-very-long-hos", "pc-0123456789abcdef": "pc-0123456789ab", "12345": "PC-12345", "abcdefghijklmn-": "abcdefghijklmn", "---": "PC-"} {
		if got := computerNameFor(in); got != want || len(got) > 15 {
			t.Errorf("computerNameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTheLocalAccountSettingGivesASmallFileOnlyWhenNoWindowsFileIsAttachedAndThePCDidNotRefuseOne(t *testing.T) {
	s, _ := answerServer(t, nil) // (no Windows answer file attached anywhere)
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); ok {
		t.Fatal("a file was given although the setting is off")
	}
	if err := s.Config.Update(func(c *config.Config) error { c.SetupChecks.LocalAccount = true; return nil }); err != nil {
		t.Fatal(err)
	}
	data, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", true)
	if !ok {
		t.Fatal("the setting gave nothing")
	}
	for _, want := range []string{"<HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>", "<HideOnlineAccountScreens>true</HideOnlineAccountScreens>", "<WillShowUI>Always</WillShowUI>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, not := range []string{"DiskConfiguration", "WillWipeDisk", "<Password>", "BypassNRO"} {
		if strings.Contains(string(data), not) {
			t.Errorf("the small file must not hold %s", not)
		}
	}
	// a Windows answer file attached to the ISO wins
	if err := s.Config.Update(func(c *config.Config) error {
		c.Answers[winID] = config.AnswerFile{Name: "Mine", Kind: "unattend", Content: "<unattend>mine</unattend>"}
		c.ISOAnswers["a1b2c3d4e5"] = winID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if data, _ := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); string(data) != "<unattend>mine</unattend>" {
		t.Fatalf("%q", data)
	}
	// a PC told to have none gets nothing, not even the small file
	if err := s.Config.Update(func(c *config.Config) error {
		delete(c.ISOAnswers, "a1b2c3d4e5")
		c.PCOptions = map[string]config.PCOptions{"aa:bb:cc:dd:ee:01": {Answer: config.NoAnswer}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.WindowsAnswer("a1b2c3d4e5", "aa:bb:cc:dd:ee:01", "autounattend.xml", false); ok {
		t.Fatal("none did not keep the small file away")
	}
}

func TestTheSmallWindowsFileIsWellFormedXml(t *testing.T) {
	d := xml.NewDecoder(strings.NewReader(localAccountXML))
	for {
		if _, err := d.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("not well formed: %v", err)
			}
			break
		}
	}
}
