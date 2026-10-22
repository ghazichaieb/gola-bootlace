package pve

import (
	"os"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
)

const sample = `#!/bin/sh
echo hi
# we have no iscsi daemon, so we need to scan iscsi device manually.
for i in /sys/block/hd* /sys/block/sr* /sys/block/scd* /sys/block/sd* /sys/block/nvme*; do
    echo $i
done
`

func TestThePatchAddsTheNetworkAndLooksOnTheNetworkDisk(t *testing.T) {
	out, ok := Patch(sample, Settings{Host: "10.0.0.1", Port: 10809, Name: "abc"})
	if !ok {
		t.Fatal("not patched")
	}
	for _, want := range []string{"/bootlace/bootlace-nbd -host '10.0.0.1' -port 10809 -name 'abc'", "/sys/block/nvme* /sys/block/nbd0; do", "modprobe -q nbd"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Index(out, "bootlace-nbd") > strings.Index(out, "# we have no iscsi daemon") {
		t.Error("the network must come up before the search for the ISO")
	}
	if _, ok := Patch("#!/bin/sh\necho unrelated\n", Settings{}); ok {
		t.Error("a script without the anchors must be left alone")
	}
}

// BOOTLACE_TEST_PVE_ISO=path to a Proxmox VE ISO runs the whole thing against it.
func TestARealProxmoxIso(t *testing.T) {
	p := os.Getenv("BOOTLACE_TEST_PVE_ISO")
	if p == "" {
		t.Skip("BOOTLACE_TEST_PVE_ISO not set")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	img, err := disc.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	if !Is(img) {
		t.Fatal("not recognised as a Proxmox ISO")
	}
	data, err := Extra(img, Settings{Host: "192.168.1.201", Port: 10809, Name: "abc"}, []byte("helper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("archive: %d bytes", len(data))
	if !strings.Contains(string(data), "e1000e") || !strings.Contains(string(data), "bootlace-nbd -host") {
		t.Fatal("the archive lacks the e1000e driver or the patched script")
	}
}
