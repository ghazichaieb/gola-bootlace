package tails

import (
	"os"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

type tree map[string]bool

func (t tree) Exists(p string) bool { return t[p] }

func TestIs(t *testing.T) {
	if !Is(tree{"/live/Tails.module": true, "/live/initrd.img": true, "/live/filesystem.squashfs": true}) {
		t.Error("Tails is not recognised")
	}
	if Is(tree{"/live/initrd.img": true, "/live/filesystem.squashfs": true}) {
		t.Error("a plain Debian live is taken for Tails")
	}
}

// BOOTLACE_TEST_DISK=path to the Tails disk image runs the whole thing against it.
func TestARealTailsImage(t *testing.T) {
	p := os.Getenv("BOOTLACE_TEST_DISK")
	if p == "" {
		t.Skip("BOOTLACE_TEST_DISK not set")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	parts, err := disc.OpenDisk(f, st.Size())
	if err != nil || len(parts) == 0 || !Is(parts[0].FS) {
		t.Skip("not a Tails image")
	}
	data, err := Extra(parts[0].FS, initrd.Settings{Host: "192.168.1.201", Port: 10809, Name: "abc@1"}, []byte("helper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("archive: %d bytes", len(data))
	s := string(data)
	if !strings.Contains(s, "scripts/init-top/bootlace-nic") || !strings.Contains(s, "-e1000e.ko") && !strings.Contains(s, "e1000e.ko") {
		t.Fatal("the archive lacks the driver loader or the e1000e driver")
	}
	if !strings.Contains(s, "/scripts/init-top/all_generic_ide") {
		t.Fatal("the original ORDER file is not kept")
	}
}
