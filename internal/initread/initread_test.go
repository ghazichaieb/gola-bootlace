package initread_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// archive is a small initramfs: a start-up script, a few modules of one kernel and an ORDER file.
func archive() []byte {
	return initrd.Archive([]initrd.File{
		{Name: "usr", Mode: 0o755, Dir: true},
		{Name: "init", Mode: 0o755, Data: []byte("#!/bin/sh\necho start\n")},
		{Name: "scripts/casper", Mode: 0o755, Data: []byte("casper")},
		{Name: "scripts/init-top/ORDER", Mode: 0o644, Data: []byte("/scripts/init-top/udev\n")},
		{Name: "usr/lib/modules/6.1.0/kernel/drivers/block/nbd.ko.zst", Mode: 0o644, Data: []byte("m1")},
		{Name: "usr/lib/modules/6.1.0/kernel/net/ptp.ko", Mode: 0o644, Data: []byte("m2")},
		{Name: "usr/lib/modules/6.1.0/modules.dep", Mode: 0o644, Data: []byte("dep")},
	})
}

func pack(t *testing.T, kind string, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	var w io.WriteCloser
	switch kind {
	case "gzip":
		w = gzip.NewWriter(&b)
	case "zstd":
		zw, err := zstd.NewWriter(&b)
		if err != nil {
			t.Fatal(err)
		}
		w = zw
	case "xz":
		xw, err := xz.NewWriter(&b)
		if err != nil {
			t.Fatal(err)
		}
		w = xw
	}
	w.Write(data)
	w.Close()
	return b.Bytes()
}

func TestAnInitramfsIsReadWhateverItIsCompressedWith(t *testing.T) {
	microcode := initrd.Archive([]initrd.File{{Name: "kernel", Mode: 0o755, Dir: true}, {Name: "kernel/x86/microcode/GenuineIntel.bin", Mode: 0o644, Data: []byte("ucode")}})
	for _, kind := range []string{"plain", "gzip", "zstd", "xz"} {
		main := archive()
		if kind != "plain" {
			main = pack(t, kind, main)
		}
		// the first archive is microcode, plain; the last one is the initramfs proper
		in, err := initread.Read(bytes.NewReader(append(append([]byte(nil), microcode...), main...)), "init", "*/modules.dep")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if in.Version != "6.1.0" || !in.Have["nbd"] || !in.Have["ptp"] || in.Have["modules"] {
			t.Errorf("%s: version %q, modules %v", kind, in.Version, in.Have)
		}
		if string(in.Files["init"]) != "#!/bin/sh\necho start\n" || string(in.Files["*/modules.dep"]) != "dep" {
			t.Errorf("%s: files %q", kind, in.Files)
		}
	}
}

func TestReadSaysWhatIsMissing(t *testing.T) {
	if _, err := initread.Read(bytes.NewReader(archive()), "no/such/file"); err == nil || !strings.Contains(err.Error(), "no/such/file") {
		t.Errorf("a file that is not there: %v", err)
	}
	bare := initrd.Archive([]initrd.File{{Name: "init", Mode: 0o755, Data: []byte("x")}})
	if _, err := initread.Read(bytes.NewReader(bare), "init"); err == nil || !strings.Contains(err.Error(), "modules") {
		t.Errorf("an initramfs with no modules: %v", err)
	}
	if _, err := initread.Read(strings.NewReader("this is not an initramfs at all")); err == nil {
		t.Error("garbage was read as an initramfs")
	}
}

func TestInspectListsEverythingAndKeepsTheBeginningOfTheFilesAskedFor(t *testing.T) {
	big := append([]byte("#!/bin/sh\n"), bytes.Repeat([]byte("x"), 100<<10)...)
	data := initrd.Archive([]initrd.File{{Name: "init", Mode: 0o755, Data: big}, {Name: "scripts/casper", Mode: 0o755, Data: []byte("c")}, {Name: "usr/lib/modules/5.0/kernel/a/e1000e.ko.xz", Mode: 0o644, Data: []byte("m")}})
	l, err := initread.Inspect(bytes.NewReader(pack(t, "zstd", data)), "init")
	if err != nil {
		t.Fatal(err)
	}
	if !l.Has("scripts/casper") || l.Has("scripts/live") || !l.HasSuffix("/casper") || l.HasSuffix("/live") {
		t.Errorf("names: %v", l.Names)
	}
	if len(l.Texts["init"]) != 64<<10 || !bytes.HasPrefix(l.Texts["init"], []byte("#!/bin/sh")) {
		t.Errorf("kept %d bytes of init", len(l.Texts["init"]))
	}
	if l.Version != "5.0" || !l.Have["e1000e"] {
		t.Errorf("modules %q %v", l.Version, l.Have)
	}
}

func TestWalkCanBeStoppedEarlyWithoutAnError(t *testing.T) {
	seen := 0
	err := initread.Walk(bytes.NewReader(archive()), func(name string, size int64, body io.Reader) error {
		seen++
		return initread.Stop
	})
	if err != nil || seen != 1 {
		t.Errorf("seen %d, error %v", seen, err)
	}
}

func TestModuleNamesLoseTheirSuffixes(t *testing.T) {
	for file, want := range map[string]string{"kernel/net/e1000e.ko": "e1000e", "a/b/r8169.ko.xz": "r8169", "x/virtio_net.ko.zst": "virtio_net", "y/z.ko.gz": "z", "modules.dep": "", "kernel/x.txt": ""} {
		if got := initread.ModuleName(file); got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}
