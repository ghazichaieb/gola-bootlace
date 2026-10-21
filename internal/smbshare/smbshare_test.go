package smbshare

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestSearchPatternsAsWindowsSendsThem(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*", "boot.wim", true}, {"", "boot.wim", true}, {"*.*", "readme", true}, {"boot.wim", "BOOT.WIM", true}, {"boot.wim", "boot.sdi", false},
		{"*.wim", "install.wim", true}, {"*.wim", "install.esd", false}, {"s?tup.exe", "setup.exe", true}, {"set*", "setup.exe", true}, {"set*", "boot", false},
	}
	for _, c := range cases {
		if got := matches(c.pattern, c.name); got != c.want {
			t.Errorf("matches(%q, %q) = %v", c.pattern, c.name, got)
		}
	}
}

func TestTheWildcardAnswerIsAnSMB2NegotiateResponseWithTheWildcardDialect(t *testing.T) {
	b := wildcardNegotiateResponse()
	if string(b[:4]) != "\xfeSMB" || binary.LittleEndian.Uint16(b[4:]) != 64 || binary.LittleEndian.Uint16(b[12:]) != 0 || binary.LittleEndian.Uint32(b[16:])&1 == 0 {
		t.Fatalf("header %x", b[:64])
	}
	body := b[64:]
	if binary.LittleEndian.Uint16(body) != 65 || binary.LittleEndian.Uint16(body[4:]) != 0x02FF {
		t.Fatalf("body %x", body[:16])
	}
	off, n := int(binary.LittleEndian.Uint16(body[56:])), int(binary.LittleEndian.Uint16(body[58:]))
	if off+n != len(b) || n != len(spnegoInit) {
		t.Fatalf("security buffer %d+%d in %d bytes", off, n, len(b))
	}
}

func TestFramesCarryTheirLength(t *testing.T) {
	f := frame([]byte{1, 2, 3})
	if len(f) != 7 || f[0] != 0 || f[3] != 3 {
		t.Fatalf("%x", f)
	}
}

// A full read answer is the 1 MiB the server offers plus its headers: the relay must pass it on.
func TestReadFrameAcceptsAFullReadAnswer(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	msg := make([]byte, 1<<20+80)
	go func() { a.Write(frame(msg)) }()
	got, err := readFrame(b)
	if err != nil || len(got) != len(msg) {
		t.Fatalf("got %d bytes, err %v", len(got), err)
	}
}
