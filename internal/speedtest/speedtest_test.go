package speedtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureWritesReadsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	r, err := Measure(context.Background(), dir, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	if r.Bytes != 16<<20 || r.WriteMBps <= 0 || r.ReadMBps <= 0 {
		t.Fatalf("%+v", r)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".speedtest-*")); len(left) != 0 {
		t.Fatalf("the test file was left behind: %v", left)
	}
	if r.Installs != int(r.ReadMBps/InstallMBps) || r.Summary() == "" {
		t.Fatalf("%+v", r)
	}
}

func TestSizeAndRefusals(t *testing.T) {
	if Size(100<<30) != MaxSize || Size(1<<30)%chunk != 0 || Size(1<<30) != (1<<30)/10/chunk*chunk {
		t.Errorf("%d %d", Size(100<<30), Size(1<<30))
	}
	if _, err := Measure(context.Background(), t.TempDir(), 1<<20); err == nil {
		t.Error("a test file under 16 MB must be refused")
	}
	if _, err := Folder(context.Background(), filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Error("a folder that is not there")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	if _, err := Measure(ctx, dir, 16<<20); err == nil {
		t.Error("a cancelled test must say so")
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
