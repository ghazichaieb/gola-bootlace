package family

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// fake is a tree made of file names. Nothing in it can be opened: the archives that read files are tested on the real ISOs (real_test.go).
type fake map[string]bool

func (f fake) Exists(name string) bool {
	if f[name] {
		return true
	}
	for k := range f {
		if strings.HasPrefix(k, name+"/") {
			return true
		}
	}
	return false
}

func (f fake) List(name string) ([]string, error) {
	seen := map[string]bool{}
	prefix := strings.TrimRight(name, "/") + "/"
	for k := range f {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			first, more, _ := strings.Cut(rest, "/")
			if more != "" {
				first += "/"
			}
			seen[first] = true
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no such folder")
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (f fake) Open(name string) (disc.File, error) { return nil, fs.ErrNotExist }

// urls is a server with nothing on but the web server: every ISO is started the plainest way.
var urls = URLs{ISO: "http://s/iso/x.iso", FS: "http://s/fs/abc", Base: "http://s"}

// withNBD is a server that also has the network block disk server on.
func withNBD(u URLs) URLs {
	u.NBD, u.NBDHost, u.NBDPort, u.NBDName, u.Extra = "10.0.0.1:/abc", "10.0.0.1", 10809, "abc", "http://s/x/abc/extra.cpio"
	return u
}

// withISCSI is a server that also has the iSCSI server on.
func withISCSI(u URLs) URLs {
	u.ISCSI, u.ISCSIHost, u.ISCSIPort, u.ISCSIIQN = "iscsi:10.0.0.1::3260:0:iqn.x:abc", "10.0.0.1", 3260, "iqn.x:abc"
	return u
}

// settings is where the network disk server is, for the extra archives.
func settings(name string, block int) initrd.Settings {
	return initrd.Settings{Host: "10.0.0.1", Port: 10809, Name: name, Block: block}
}

func lines(b Boot) string { return strings.Join(b.Commands, "\n") }

// detect checks that the tree is recognised as the family, and that each wanted text is in the commands (and none of the unwanted, which start with "!").
func detect(t *testing.T, name string, tree Tree, u URLs, family string, wants ...string) Boot {
	t.Helper()
	b, ok := Detect(tree, u)
	if !ok || b.Family != family {
		t.Errorf("%s: recognised as %q (%v), want %q\n%s", name, b.Family, ok, family, lines(b))
		return b
	}
	for _, w := range wants {
		if neg, found := strings.CutPrefix(w, "!"); found {
			if strings.Contains(lines(b), neg) {
				t.Errorf("%s: has %q\n%s", name, neg, lines(b))
			}
		} else if !strings.Contains(lines(b), w) {
			t.Errorf("%s: lacks %q\n%s", name, w, lines(b))
		}
	}
	return b
}

// bytesFile is a disc file made of bytes.
type bytesFile []byte

func (b bytesFile) Size() int64 { return int64(len(b)) }
func (b bytesFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (b bytesFile) Reader() io.ReadSeeker { return bytes.NewReader(b) }

// memTree is a tree whose files have content, so that menus and start-up disks can be read from it.
type memTree map[string][]byte

func (m memTree) names() fake {
	f := fake{}
	for k := range m {
		f[k] = true
	}
	return f
}
func (m memTree) Exists(name string) bool            { return m.names().Exists(name) }
func (m memTree) List(name string) ([]string, error) { return m.names().List(name) }
func (m memTree) Open(name string) (disc.File, error) {
	if d, ok := m[name]; ok {
		return bytesFile(d), nil
	}
	return nil, fs.ErrNotExist
}
