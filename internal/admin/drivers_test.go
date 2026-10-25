package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheDriverFoldersOfTheLibraryAreListed(t *testing.T) {
	lib := t.TempDir()
	if got := driverFolders(lib); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	if err := os.MkdirAll(filepath.Join(lib, "drivers", "all", "vio"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(lib, "drivers", "all", "vio", "a.inf"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(lib, "drivers", "all", "vio", "a.sys"), []byte("yy"), 0o644)
	os.MkdirAll(filepath.Join(lib, "drivers", "My ISO"), 0o755)
	os.WriteFile(filepath.Join(lib, "drivers", "stray.txt"), []byte("no"), 0o644) // (a file, not a folder)
	got := driverFolders(lib)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	var all driverFolder
	for _, f := range got {
		if f.Name == "all" {
			all = f
		}
	}
	if !all.All || all.Packages != 1 || all.Files != 2 || all.Bytes != 3 {
		t.Fatalf("%+v", all)
	}
}

func TestTheTuningPageSaysWhatTheDriversFolderHolds(t *testing.T) {
	r := newRig(t)
	r.login()
	_, out := r.do("GET", "/api/tuning", nil, nil)
	if _, ok := out["driverFolders"].([]any); !ok || out["driverLimitMB"].(float64) != 1024 || out["driversFolder"] != "drivers" {
		t.Fatalf("%v", out)
	}
	if code, _ := r.do("PUT", "/api/tuning", map[string]any{"hidePatterns": []string{}, "setupChecks": map[string]any{"drivers": true, "localAccount": true, "tpm": true}}, nil); code != 200 {
		t.Fatal("save")
	}
	c := r.store.Get()
	if !c.SetupChecks.Drivers || !c.SetupChecks.LocalAccount || !c.SetupChecks.TPM {
		t.Fatalf("%+v", c.SetupChecks)
	}
}
