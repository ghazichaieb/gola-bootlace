package archisoesp

import (
	"os"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// BOOTLACE_TEST_DISK=path to the ChimeraOS ISO runs the whole thing against it.
func TestARealIso(t *testing.T) {
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
	iso, err := disc.Open(f, st.Size())
	if err != nil || !iso.Exists("/arch/x86_64/airootfs.sfs") {
		t.Skip("not an archiso with the system at /arch/x86_64")
	}
	parts, err := disc.OpenDisk(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if !Is(part.FS) {
			continue
		}
		data, err := Extra(part.FS, iso, initrd.Settings{Host: "192.168.1.201", Port: 10809, Name: "abc"}, []byte("helper"))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("archive: %d bytes", len(data))
		s := string(data)
		if !strings.Contains(s, "hooks/bootlace") || !strings.Contains(s, "bootlace archiso") || !strings.Contains(s, "e1000e.ko") {
			t.Fatal("the archive lacks the hook, the changed config or the e1000e driver")
		}
		return
	}
	t.Skip("no EFI image with an archiso kernel")
}
