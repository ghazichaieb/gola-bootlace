package libreelec

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

type tree map[string]bool

func (t tree) Exists(p string) bool { return t[p] }

func TestIs(t *testing.T) {
	if !Is(tree{"/KERNEL": true, "/SYSTEM": true}) {
		t.Error("a LibreELEC boot partition is not recognised")
	}
	if Is(tree{"/KERNEL": true, "/SYSTEM": true, "/live": true}) || Is(tree{"/KERNEL": true}) {
		t.Error("something else is taken for LibreELEC")
	}
}

func TestExtraNamesTheExportOfTheBootDevice(t *testing.T) {
	data := string(Extra(initrd.Settings{Host: "10.0.0.1", Port: 10809, Name: "abc@1", Block: 512}, []byte("helper")))
	for _, want := range []string{"nbd0 abc@1 512\n", "usr/bin/nbd-client", "bootlace-init", "sed -i 's#nbd-client $NBD_SERVER#/usr/bin/nbd-client $NBD_SERVER#' /init", "exec /init"} {
		if !strings.Contains(data, want) {
			t.Errorf("the archive lacks %q", want)
		}
	}
}
