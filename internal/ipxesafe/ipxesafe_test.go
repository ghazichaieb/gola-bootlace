package ipxesafe

import (
	"strings"
	"testing"
)

// chainWords counts the words iPXE would take as the end of a command, splitting the way iPXE does (any of space, tab, line feed, vertical tab, form feed, carriage return).
func chainWords(s string) int {
	n := 0
	for _, w := range strings.FieldsFunc(s, space) {
		if Terminator(w) {
			n++
		}
	}
	return n
}

func TestTextTurnsEveryChainWordAndEveryControlCharacterIntoPlainText(t *testing.T) {
	for _, in := range []string{
		"evil && chain http://elsewhere/boot.ipxe || shell ; echo pwned",
		"a\v&&\fb", "a\t||\rb", "x\n;\ny", "${net0/mac} and ${next-server}", "&& && ||", ";", "a&&b ;; ||| &&&",
	} {
		out := Text(in)
		if chainWords(out) != 0 {
			t.Errorf("%q became %q: it still chains commands", in, out)
		}
		if strings.ContainsAny(out, "\r\n\t\v\f\x00") || strings.Contains(out, "${") {
			t.Errorf("%q became %q: a control character or ${ is left", in, out)
		}
	}
	if got := Text("Ubuntu 24.04 [Ubuntu (casper), 5.8 GB] a&b c;d"); got != "Ubuntu 24.04 [Ubuntu (casper), 5.8 GB] a&b c;d" {
		t.Errorf("an ordinary title is changed: %q", got)
	}
}

func TestArgsDropsTheChainWordsAndKeepsTheOthers(t *testing.T) {
	if got := Args("quiet splash ds=nocloud-net;s=http://x/ && chain http://y ; boot=casper\t||  foo"); got != "quiet splash ds=nocloud-net;s=http://x/ chain http://y boot=casper foo" {
		t.Errorf("got %q", got)
	}
	if got := Args("a${b}"); strings.Contains(got, "${") {
		t.Errorf("expansion left in %q", got)
	}
}

func TestUnsafeArgsRefusesWhatAPersonShouldNotType(t *testing.T) {
	for _, bad := range []string{"nomodeset && reboot", "a || b", "a ; b", "a\tb", "a\vb", "a\x7fb", "a\x00b"} {
		if UnsafeArgs(bad) == "" {
			t.Errorf("%q is accepted", bad)
		}
	}
	for _, ok := range []string{"", "nomodeset", "console=ttyS0,115200n8 loglevel=7", "ds=nocloud-net;s=http://x/", "root=live:LABEL=A&&B", "a ;b c; d"} {
		if why := UnsafeArgs(ok); why != "" {
			t.Errorf("%q is refused: %s", ok, why)
		}
	}
}
