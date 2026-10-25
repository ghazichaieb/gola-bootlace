package httpd

import (
	"regexp"
	"strings"
	"testing"
)

var validHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// The name of a PC goes into an installer's file as a host name: whatever a person typed as the PC's name, it must come out as a valid host name of at most 63 characters.
func FuzzHostnameFor(f *testing.F) {
	f.Add("Lab PC 4", "aa:bb:cc:dd:ee:ff")
	f.Add("--x--", "")
	f.Add("éè !!", "zz")
	f.Add(strings.Repeat("a", 200), "aa:bb:cc:dd:ee:ff")
	f.Fuzz(func(t *testing.T, name, mac string) {
		h := hostnameFor(name, mac)
		if len(h) == 0 || len(h) > 63 || !validHost.MatchString(h) {
			t.Fatalf("%q, %q gave the host name %q", name, mac, h)
		}
	})
}

// Words in a value must not be replaced a second time (a PC called "{{serial}}" must not pull the serial number into the file), and text without the words is left as it is.
func FuzzRenderAnswerReplacesOnce(f *testing.F) {
	f.Add("hostname {{hostname}} name {{name}}", "pc")
	f.Add("{{{{mac}}}}", "{{mac}}")
	f.Add("{{ip}}", "{{serial}}")
	f.Fuzz(func(t *testing.T, text, name string) {
		const token = "SERIAL-TOKEN-0451"
		vars := map[string]string{"hostname": "h", "name": name, "mac": "aa:bb:cc:dd:ee:ff", "mac-dash": "aa-bb-cc-dd-ee-ff", "ip": "10.0.0.1", "serial": token}
		out := renderAnswer(text, vars)
		if !strings.Contains(text, "{{") && out != text {
			t.Fatalf("text without words was changed: %q -> %q", text, out)
		}
		if !strings.Contains(text, "{{serial}}") && !strings.Contains(text, token) && !strings.Contains(name, token) && strings.Contains(out, token) {
			t.Fatalf("the serial was pulled in through another value: %q rendered from %q (name %q)", out, text, name)
		}
	})
}
