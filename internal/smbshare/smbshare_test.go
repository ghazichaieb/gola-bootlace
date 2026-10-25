package smbshare

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
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

func TestTheAnswerFileOfAPCIsAFileOfTheIsoFolderMadeWhenOpenedAndCountedWhenRead(t *testing.T) {
	var asked []string
	b := &Backend{answer: func(id, mac, name string, commit bool) ([]byte, bool) {
		asked = append(asked, fmt.Sprintf("%s/%s/%s/%v", id, mac, name, commit))
		if mac != "00-15-5D-01-02-03" {
			return nil, false
		}
		return []byte("<unattend/>"), true
	}}
	if h := b.answerHandle("ab12", "_bootlace/AA-BB-CC-DD-EE-FF/autounattend.xml"); h != nil {
		t.Fatal("a PC without a file got one")
	}
	for _, rest := range []string{"sources/boot.wim", "_bootlace/AA-BB-CC-DD-EE-FF", "_bootlace/00-15-5D-01-02-03/autounattend.xml/x", "other/00-15-5D-01-02-03/autounattend.xml"} {
		if b.answerHandle("ab12", rest) != nil {
			t.Errorf("%q is not an answer path", rest)
		}
	}
	h := b.answerHandle("ab12", "_BootLace/00-15-5D-01-02-03/AutoUnattend.XML")
	if h == nil {
		t.Fatal("no handle")
	}
	st, _ := h.Stat(context.Background())
	if st.IsDir || st.Size != int64(len("<unattend/>")) || st.Name != "autounattend.xml" {
		t.Fatalf("%+v", st)
	}
	if got := asked[len(asked)-1]; !strings.HasSuffix(got, "/false") {
		t.Fatalf("opening must only look: %v", asked)
	}
	buf := make([]byte, 100)
	n, err := h.Read(context.Background(), 0, buf)
	if err != nil || string(buf[:n]) != "<unattend/>" {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := h.Read(context.Background(), int64(n), buf); err != io.EOF {
		t.Fatalf("%v", err)
	}
	h.Read(context.Background(), 0, buf)
	commits := 0
	for _, a := range asked {
		if strings.HasSuffix(a, "/true") {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("the file is counted once per handle: %v", asked)
	}
	if _, err := h.Write(context.Background(), 0, []byte("x")); err == nil {
		t.Fatal("writable")
	}
}

// Windows opens the folders on the way before it copies a file (FindFirstFile on the parent): they must be there, and list the file for a PC that has one.
func TestTheFoldersOnTheWayToTheAnswerAreThereAndListTheFileOnlyForAPCThatHasOne(t *testing.T) {
	b := &Backend{answer: func(id, mac, name string, commit bool) ([]byte, bool) {
		return []byte("<unattend/>"), mac == "00-15-5D-01-02-03"
	}}
	top := b.answerHandle("ab12", "_bootlace")
	if top == nil {
		t.Fatal("no folder")
	}
	if st, _ := top.Stat(context.Background()); !st.IsDir || st.Name != "_bootlace" {
		t.Fatalf("%+v", st)
	}
	n := 0
	for range top.Enumerate(context.Background(), "*") {
		n++
	}
	if n != 0 {
		t.Fatal("the folder of all PCs must list nothing")
	}
	if b.answerHandle("ab12", "_bootlace/AA-BB-CC-DD-EE-FF") != nil {
		t.Fatal("a folder for a PC without a file")
	}
	mine := b.answerHandle("ab12", "_bootlace/00-15-5D-01-02-03")
	if mine == nil {
		t.Fatal("no folder for the PC")
	}
	var names []string
	for fi, err := range mine.Enumerate(context.Background(), "autounattend.xml") {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, fi.Name)
	}
	if strings.Join(names, ",") != "autounattend.xml" {
		t.Fatalf("%v", names)
	}
	for range mine.Enumerate(context.Background(), "*.wim") {
		t.Fatal("a pattern that does not match listed the file")
	}
}
