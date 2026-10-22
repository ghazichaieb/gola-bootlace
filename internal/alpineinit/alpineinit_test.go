package alpineinit

import (
	"os"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
)

type tree map[string]bool

func (t tree) Exists(p string) bool { return t[p] }

func TestIs(t *testing.T) {
	if !Is(tree{"/.alpine-release": true, "/boot/initramfs-lts": true, "/boot/modloop-lts": true}, "lts") {
		t.Error("Alpine is not recognised")
	}
	if Is(tree{"/.alpine-release": true, "/boot/initramfs-lts": true}, "lts") {
		t.Error("an Alpine without a modloop is taken as one that can be given drivers")
	}
}

// BOOTLACE_TEST_DISK=path to an Alpine ISO runs the whole thing against it.
func TestARealAlpineIso(t *testing.T) {
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
	if err != nil || !Is(iso, "lts") {
		t.Skip("not an Alpine ISO with the lts kernel")
	}
	data, err := Extra(iso, "lts", []byte("helper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("archive: %d bytes", len(data))
	s := string(data)
	for _, want := range []string{"-e1000e.ko", "bootlace-init", "exec /init"} {
		if !strings.Contains(s, want) {
			t.Errorf("the archive lacks %q", want)
		}
	}
}
