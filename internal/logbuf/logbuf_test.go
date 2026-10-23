package logbuf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinesAreNumberedAndOnlyTheLastOnesAreKept(t *testing.T) {
	b := New(100)
	for i := 1; i <= 130; i++ {
		b.Logf("line %d\n", i)
	}
	all, last := b.After(0, 0)
	if last != 130 || len(all) != 100 || all[0].N != 31 || all[0].Text != "line 31" {
		t.Fatalf("last %d, kept %d, first %+v", last, len(all), all[0])
	}
	news, _ := b.After(125, 0)
	if len(news) != 5 || news[0].Text != "line 126" {
		t.Fatalf("after 125: %+v", news)
	}
	few, _ := b.After(0, 3)
	if len(few) != 3 || few[2].N != 130 {
		t.Fatalf("limit: %+v", few)
	}
	if none, last := b.After(130, 0); len(none) != 0 || last != 130 {
		t.Fatalf("nothing new: %+v %d", none, last)
	}
}

func TestFileIsRotatedAndSecretsStayOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootlace.log")
	b := New(100)
	if err := b.SetFile(path, 1); err != nil { // raised to the 64 KB minimum
		t.Fatal(err)
	}
	defer b.Close()
	b.AddPrivate("the password was made up: hunter22")
	if mem, _ := b.After(0, 0); len(mem) != 1 || mem[0].Text != "the password was made up: hunter22" {
		t.Fatalf("the private line is not in memory: %+v", mem)
	}
	filler := strings.Repeat("x", 1000)
	for i := 0; i < 200; i++ {
		b.Logf("line %d %s", i, filler)
	}
	cur, err := os.Stat(path)
	if err != nil || cur.Size() > 64<<10 {
		t.Fatalf("current file: %v %v", cur, err)
	}
	old, err := os.Stat(path + ".1")
	if err != nil || old.Size() == 0 {
		t.Fatalf("older file: %v %v", old, err)
	}
	var all bytes.Buffer
	if err := b.Dump(&all); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(all.String(), "hunter22") {
		t.Fatal("a private line reached the file")
	}
	if !strings.Contains(all.String(), "line 199 ") || strings.Index(all.String(), "line 199 ") < strings.Index(all.String(), "line 150 ") {
		t.Fatal("the dump is not in order or lacks the newest line")
	}
}

func TestDumpWithoutFileUsesMemory(t *testing.T) {
	b := New(100)
	b.Logf("one")
	b.Logf("two")
	var out bytes.Buffer
	if err := b.Dump(&out); err != nil || !strings.Contains(out.String(), "  one\n") || !strings.HasSuffix(out.String(), "  two\n") {
		t.Fatalf("%q %v", out.String(), err)
	}
}
