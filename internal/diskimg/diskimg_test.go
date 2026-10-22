package diskimg

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

func TestAnImageThatIsNotPackedIsUsedWhereItIs(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	st := m.Status(isos.ISO{Path: "/x/a.img", Size: 5})
	if !st.Ready || st.Path != "/x/a.img" || st.Size != 5 {
		t.Fatalf("%+v", st)
	}
}

func TestPackedImagesAreUnpackedOnceInTheBackground(t *testing.T) {
	data := bytes.Repeat([]byte("disk"), 100000)
	dir := t.TempDir()
	for name, pack := range map[string]func() []byte{
		"a.img.gz": func() []byte {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(data)
			w.Close()
			return b.Bytes()
		},
		"b.img.xz": func() []byte {
			var b bytes.Buffer
			w, _ := xz.NewWriter(&b)
			w.Write(data)
			w.Close()
			return b.Bytes()
		},
	} {
		path := filepath.Join(dir, name)
		packed := pack()
		os.WriteFile(path, packed, 0o644)
		iso := isos.ISO{ID: name[:1], Name: name, Path: path, Size: int64(len(packed)), Packed: filepath.Ext(name)[1:], ModTime: time.Unix(1, 0), Disk: true}
		m := &Manager{Dir: filepath.Join(dir, "cache")}
		var st State
		for i := 0; i < 200; i++ {
			if st = m.Status(iso); st.Ready || st.Err != nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if st.Err != nil || !st.Ready || st.Size != int64(len(data)) {
			t.Fatalf("%s: %+v", name, st)
		}
		got, _ := os.ReadFile(st.Path)
		if !bytes.Equal(got, data) {
			t.Fatalf("%s: the unpacked image differs", name)
		}
	}
}

func TestABrokenPackedImageReportsTheError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.img.gz")
	os.WriteFile(path, []byte("this is not gzip"), 0o644)
	m := &Manager{Dir: filepath.Join(dir, "cache")}
	iso := isos.ISO{ID: "b", Name: "bad.img.gz", Path: path, Size: 16, Packed: "gz", ModTime: time.Unix(1, 0)}
	var st State
	for i := 0; i < 200 && st.Err == nil; i++ {
		st = m.Status(iso)
		time.Sleep(20 * time.Millisecond)
	}
	if st.Err == nil || st.Ready {
		t.Fatalf("%+v", st)
	}
}
