package initread

import (
	"bytes"
	"compress/gzip"
	"reflect"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func TestADriverIsLoadedAfterWhatItNeedsAndEachModuleOnce(t *testing.T) {
	dep := "kernel/net/ptp.ko.zst:\n" +
		"kernel/drivers/mdio.ko.zst: kernel/net/ptp.ko.zst\n" +
		"kernel/drivers/e1000e.ko.zst: kernel/net/ptp.ko.zst kernel/drivers/mdio.ko.zst\n" +
		"kernel/drivers/igb.ko.zst: kernel/net/ptp.ko.zst\n" +
		"kernel/drivers/unrelated.ko.zst:\n"
	got := loadOrder(dep, []string{"e1000e", "igb", "missing"})
	want := []string{"kernel/net/ptp.ko.zst", "kernel/drivers/mdio.ko.zst", "kernel/drivers/e1000e.ko.zst", "kernel/drivers/igb.ko.zst"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEveryKindOfCompressedModuleIsUnpacked(t *testing.T) {
	plain := []byte("\x7fELF a module")
	for suffix, pack := range map[string]func([]byte) []byte{
		".ko": func(b []byte) []byte { return b },
		".ko.gz": func(b []byte) []byte {
			var o bytes.Buffer
			w := gzip.NewWriter(&o)
			w.Write(b)
			w.Close()
			return o.Bytes()
		},
		".ko.xz": func(b []byte) []byte {
			var o bytes.Buffer
			w, _ := xz.NewWriter(&o)
			w.Write(b)
			w.Close()
			return o.Bytes()
		},
		".ko.zst": func(b []byte) []byte {
			var o bytes.Buffer
			w, _ := zstd.NewWriter(&o)
			w.Write(b)
			w.Close()
			return o.Bytes()
		},
	} {
		got, err := decompress("kernel/x/e1000e"+suffix, pack(plain))
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("%s: %q, %v", suffix, got, err)
		}
	}
	if _, err := decompress("kernel/x/bad.ko.xz", []byte("not xz")); err == nil {
		t.Error("a damaged module was taken as good")
	}
}

func TestTheCommonDriversAreNamedWithoutSuffixes(t *testing.T) {
	for _, want := range []string{"e1000e", "r8169", "igb", "virtio_net", "vmxnet3", "hv_netvsc"} {
		found := false
		for _, n := range NICModules {
			found = found || n == want
		}
		if !found {
			t.Errorf("%s is not among the drivers to bring", want)
		}
	}
}
