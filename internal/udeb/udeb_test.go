// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package udeb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

const packagesText = `Package: open-iscsi-udeb
Version: 2.1.11-1
Depends: libc6-udeb (>= 2.41), libkmod2-udeb,
 libopeniscsiusr0-udeb | libother
Filename: pool/main/o/open-iscsi/open-iscsi-udeb_2.1.11-1_amd64.udeb
Size: 100

Package: libkmod2-udeb
Version: 34
Depends: libc6-udeb
Filename: /pool/main/k/kmod/libkmod2-udeb_34_amd64.udeb

Package: libc6-udeb
Version: 2.41
Filename: pool/main/g/glibc/libc6-udeb_2.41_amd64.udeb

Package: standalone
Version: 1
`

func TestParsePackages(t *testing.T) {
	list, err := ParsePackages(strings.NewReader(packagesText))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("got %d packages, want 4", len(list))
	}
	o := list[0]
	if o.Name != "open-iscsi-udeb" || o.Version != "2.1.11-1" || o.Filename != "pool/main/o/open-iscsi/open-iscsi-udeb_2.1.11-1_amd64.udeb" {
		t.Errorf("first package: %+v", o)
	}
	if want := []string{"libc6-udeb", "libkmod2-udeb", "libopeniscsiusr0-udeb"}; !reflect.DeepEqual(o.Depends, want) {
		t.Errorf("depends over two lines: got %v, want %v", o.Depends, want)
	}
	if list[1].Filename != "pool/main/k/kmod/libkmod2-udeb_34_amd64.udeb" {
		t.Errorf("a leading slash is not dropped: %q", list[1].Filename)
	}
	if list[3].Name != "standalone" || list[3].Filename != "" {
		t.Errorf("last stanza without a blank line after it: %+v", list[3])
	}
}

func TestSplitDepends(t *testing.T) {
	for in, want := range map[string][]string{
		"":                              nil,
		"a":                             {"a"},
		"a (>= 1), b | c, d(<< 2)":      {"a", "b", "d"},
		"  a  ,\tb (= 1.0) | c (>= 2) ": {"a", "b"},
		", ,":                           nil,
	} {
		if got := splitDepends(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitDepends(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClosure(t *testing.T) {
	list, _ := ParsePackages(strings.NewReader(packagesText))
	idx := NewIndex(list)
	names := func(ps []Package) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	// libopeniscsiusr0-udeb is not in the index (a virtual or already present package): left out, not an error
	if got, want := names(idx.Closure("open-iscsi-udeb", nil)), []string{"open-iscsi-udeb", "libc6-udeb", "libkmod2-udeb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("closure: got %v, want %v", got, want)
	}
	skip := func(n string) bool { return strings.HasPrefix(n, "libc6") }
	if got, want := names(idx.Closure("open-iscsi-udeb", skip)), []string{"open-iscsi-udeb", "libkmod2-udeb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("closure with skip: got %v, want %v", got, want)
	}
	if got := idx.Closure("nothing", nil); len(got) != 0 {
		t.Errorf("an unknown package has a closure: %v", got)
	}
	// a dependency cycle ends
	cyc := NewIndex([]Package{{Name: "a", Depends: []string{"b"}}, {Name: "b", Depends: []string{"a"}}})
	if got := names(cyc.Closure("a", nil)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("cycle: %v", got)
	}
}

type tarFile struct {
	name, link string
	typ        byte
	data       string
	mode       int64
}

func makeTar(t *testing.T, files []tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Typeflag: f.typ, Mode: f.mode, Linkname: f.link}
		if f.typ == tar.TypeReg {
			h.Size = int64(len(f.data))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if f.typ == tar.TypeReg {
			if _, err := io.WriteString(tw, f.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeAr(members map[string][]byte, order []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	for _, name := range order {
		body := members[name]
		fmt.Fprintf(&buf, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", name+"/", 0, 0, 0, "100644", len(body))
		buf.Write(body)
		if len(body)%2 == 1 {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

func compress(t *testing.T, kind string, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	switch kind {
	case "":
		return raw
	case ".gz":
		w := gzip.NewWriter(&buf)
		w.Write(raw)
		w.Close()
	case ".xz":
		w, err := xz.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(raw)
		w.Close()
	default:
		t.Fatal("unknown kind", kind)
	}
	return buf.Bytes()
}

func TestFiles(t *testing.T) {
	files := []tarFile{
		{name: "./", typ: tar.TypeDir, mode: 0o755},
		{name: "./usr/", typ: tar.TypeDir, mode: 0o755},
		{name: "./usr/sbin/iscsistart", typ: tar.TypeReg, data: "ELF-odd", mode: 0o755}, // an odd size: the ar member is padded
		{name: "./usr/lib/libx.so.0", typ: tar.TypeSymlink, link: "libx.so.0.2.0"},
		{name: "../escape/up", typ: tar.TypeReg, data: "x", mode: 0o644},
		{name: "./dev/null", typ: tar.TypeChar, mode: 0o666},
	}
	raw := makeTar(t, files)
	for _, kind := range []string{"", ".gz", ".xz"} {
		t.Run("data.tar"+kind, func(t *testing.T) {
			ar := makeAr(map[string][]byte{
				"debian-binary":   []byte("2.0\n"),
				"control.tar.xz":  compress(t, ".xz", []byte("not read")),
				"data.tar" + kind: compress(t, kind, raw),
			}, []string{"debian-binary", "control.tar.xz", "data.tar" + kind})
			got := map[string]Entry{}
			if err := Files(bytes.NewReader(ar), func(e Entry) error { got[e.Name] = e; return nil }); err != nil {
				t.Fatal(err)
			}
			if e := got["usr/sbin/iscsistart"]; string(e.Data) != "ELF-odd" || e.Mode.Perm() != 0o755 {
				t.Errorf("file: %+v", e)
			}
			if e := got["usr/lib/libx.so.0"]; e.Link != "libx.so.0.2.0" || len(e.Data) != 0 {
				t.Errorf("link: %+v", e)
			}
			if e, ok := got["usr"]; !ok || !e.Mode.IsDir() {
				t.Errorf("directory entry: %+v %v", e, ok)
			}
			if _, ok := got["escape/up"]; !ok {
				t.Errorf("a path that climbs out is made relative, got %v", keys(got))
			}
			if _, ok := got["dev/null"]; ok {
				t.Error("a device node is taken")
			}
			if _, ok := got[""]; ok {
				t.Error("the root entry is reported")
			}
		})
	}
}

func keys(m map[string]Entry) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFilesErrors(t *testing.T) {
	if err := Files(strings.NewReader("hello, not an archive"), func(Entry) error { return nil }); err == nil {
		t.Error("garbage is accepted")
	}
	noData := makeAr(map[string][]byte{"debian-binary": []byte("2.0\n")}, []string{"debian-binary"})
	if err := Files(bytes.NewReader(noData), func(Entry) error { return nil }); err == nil || !strings.Contains(err.Error(), "no data archive") {
		t.Errorf("an ar without data.tar: %v", err)
	}
	// a damaged member header
	bad := append([]byte("!<arch>\n"), bytes.Repeat([]byte{'x'}, 60)...)
	if err := Files(bytes.NewReader(bad), func(Entry) error { return nil }); err == nil {
		t.Error("a damaged header is accepted")
	}
	// a truncated archive
	good := makeAr(map[string][]byte{"data.tar": makeTar(t, []tarFile{{name: "a", typ: tar.TypeReg, data: strings.Repeat("z", 5000), mode: 0o644}})}, []string{"data.tar"})
	if err := Files(bytes.NewReader(good[:len(good)/2]), func(Entry) error { return nil }); err == nil {
		t.Error("a truncated archive is accepted")
	}
	// fn can stop the walk
	stop := fmt.Errorf("stop")
	if err := Files(bytes.NewReader(good), func(Entry) error { return stop }); err != stop {
		t.Errorf("fn's error is not returned: %v", err)
	}
}

func TestGunzip(t *testing.T) {
	text := []byte("Package: a\n")
	for _, kind := range []string{"", ".gz", ".xz"} {
		r, err := Gunzip(compress(t, kind, text))
		if err != nil {
			t.Fatalf("%q: %v", kind, err)
		}
		b, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(b, text) {
			t.Errorf("%q: got %q, %v", kind, b, err)
		}
	}
	if _, err := Gunzip([]byte{0x1f, 0x8b, 1, 2, 3}); err == nil {
		t.Error("a damaged gzip is accepted")
	}
}
