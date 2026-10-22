package initrd

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheArchiveIsInTheFormatTheKernelReads(t *testing.T) {
	a := Archive([]File{{Name: "usr", Mode: 0o755, Dir: true}, {Name: "usr/bin/x", Mode: 0o755, Data: []byte("hello")}})
	if !bytes.HasPrefix(a, []byte("070701")) || !bytes.Contains(a, []byte("TRAILER!!!")) || !bytes.Contains(a, []byte("usr/bin/x\x00")) || !bytes.Contains(a, []byte("hello")) {
		t.Fatalf("%q", a)
	}
	if len(a)%4 != 0 {
		t.Fatalf("length %d is not a multiple of 4", len(a))
	}
	// every header is 110 bytes, and names and data are padded to 4
	if string(a[110:114]) != "usr\x00" {
		t.Fatalf("name at 110: %q", a[110:118])
	}
}

func TestTheScriptNamesTheIsoAndHasAWayOut(t *testing.T) {
	a := string(Extra(Settings{Host: "10.0.0.1", Port: 10809, Name: "abc1234567", URL: "http://10.0.0.1:8080/iso/u.iso"}, []byte("ELF")))
	for _, want := range []string{"-host '10.0.0.1' -port 10809 -name 'abc1234567'", "wget 'http://10.0.0.1:8080/iso/u.iso'", "usr/bin/bootlace-nbd", "mount -t iso9660 -o ro /dev/nbd0"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q", want)
		}
	}
}
