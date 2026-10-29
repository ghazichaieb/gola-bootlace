package httpd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

func TestFindingsDiskCacheIsBoundedVersionedAndInvalidated(t *testing.T) {
	iso := isos.ISO{Path: filepath.Join(t.TempDir(), "sample.iso"), Size: 123, ModTime: time.Unix(123, 456)}
	s := &Server{CacheDir: t.TempDir(), Base: "http://server:8080"}
	payload, _ := json.Marshal(map[string]any{"format": 1, "stamp": stamp(iso), "facts": map[string]any{stamp(iso) + "|" + s.Base + "/fs/id/initrd": map[string]any{"flavor": "casper", "kernelVersion": "6.1", "NIC": true, "NBD": true}}})
	if err := s.findings.RestoreStamp(stamp(iso), payload); err != nil {
		t.Fatal(err)
	}
	s.saveFindings(iso, s.findingsGeneration())
	path := s.findingsPath(iso)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	restart := func(base string) *Server { return &Server{CacheDir: s.CacheDir, Base: base} }
	restored := restart(s.Base)
	if !restored.loadFindings(iso) {
		t.Fatal("matching restart cache missed")
	}
	got, _ := restored.findings.MarshalStamp(stamp(iso))
	var parsed map[string]any
	if json.Unmarshal(got, &parsed) != nil || len(parsed["facts"].(map[string]any)) != 1 {
		t.Fatal("facts not restored")
	}
	changed := iso
	changed.ModTime = changed.ModTime.Add(time.Second)
	if restart(s.Base).loadFindings(changed) {
		t.Fatal("changed ISO reused facts")
	}
	if restart("http://other:8080").loadFindings(iso) {
		t.Fatal("address mismatch accepted")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved findingsFile
	if err := json.Unmarshal(original, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Program = version.Version + "-other"
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if restart(s.Base).loadFindings(iso) {
		t.Fatal("different program accepted")
	}
	for _, bad := range [][]byte{[]byte("{invalid"), make([]byte, maxFindingsFile+1)} {
		if err := os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if restart(s.Base).loadFindings(iso) {
			t.Fatal("corrupt/oversize cache accepted")
		}
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s.Rescan()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("routine refresh removed cache", err)
	}
	after, _ := s.findings.MarshalStamp(stamp(iso))
	if string(after) != string(got) {
		t.Fatal("routine refresh removed inspected facts")
	}
	if !s.loadFindings(iso) {
		t.Fatal("routine refresh blocked loading disk facts")
	}
	epoch := s.findingsGeneration()
	s.ForgetFindings()
	s.Rescan()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rescan retained disk cache", err)
	}
	s.saveFindings(iso, epoch)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("old background generation repopulated disk cache", err)
	}
}

func TestFindingsCacheFailureDoesNotPreventServing(t *testing.T) {
	iso := isos.ISO{Path: "missing.iso"}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: blocked, Base: "http://server"}
	if s.loadFindings(iso) {
		t.Fatal("unavailable cache accepted")
	}
	s.saveFindings(iso, s.findingsGeneration()) // best-effort; no panic or effect on boot state
	if (&Server{}).loadFindings(iso) {
		t.Fatal("memory-only server loaded disk cache")
	}
}

func TestOlderRecognitionCannotPublishAfterRescan(t *testing.T) {
	s := &Server{}
	epoch := s.findingsGeneration()
	s.Rescan()
	if s.publishRecognition(epoch, func() { t.Error("stale look published") }) {
		t.Fatal("stale generation accepted")
	}
	called := false
	if !s.publishRecognition(s.findingsGeneration(), func() { called = true }) || !called {
		t.Fatal("current look rejected")
	}
}
