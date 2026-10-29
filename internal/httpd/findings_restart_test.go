package httpd

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
)

// A small ISO 9660 tree, built here instead of checking in a distribution image.
// Its initrd is intentionally undecodable: a cache hit must not inspect it.
func restartFixtureISO(t *testing.T, dir string) {
	t.Helper()
	type node struct {
		name     string
		sector   int
		children []*node
		data     []byte
	}
	root := &node{name: "root"}
	boot := &node{name: "boot"}
	grub := &node{name: "grub"}
	live := &node{name: "newos"}
	root.children = []*node{boot, live}
	boot.children = []*node{grub}
	grub.children = []*node{{name: "grub.cfg", data: []byte("menuentry 'Live' {\nlinux /newos/vmlinuz boot=casper quiet ---\ninitrd /newos/initrd\n}\nmenuentry 'Safe graphics' {\nlinux /newos/vmlinuz boot=casper nomodeset quiet ---\ninitrd /newos/initrd\n}\n")}}
	live.children = []*node{{name: "vmlinuz", data: []byte("kernel")}, {name: "initrd", data: []byte("must not be inspected")}, {name: "filesystem.squashfs", data: []byte("filesystem")}}
	var all []*node
	var assign func(*node)
	assign = func(n *node) {
		n.sector = 20 + len(all)
		all = append(all, n)
		for _, c := range n.children {
			assign(c)
		}
	}
	assign(root)
	image := make([]byte, (20+len(all))*2048)
	record := func(n *node, name string) []byte {
		size := len(n.data)
		flags := byte(0)
		if len(n.children) > 0 {
			size = 2048
			flags = 2
		}
		length := 33 + len(name)
		if length%2 != 0 {
			length++
		}
		b := make([]byte, length)
		b[0] = byte(length)
		binary.LittleEndian.PutUint32(b[2:], uint32(n.sector))
		binary.LittleEndian.PutUint32(b[10:], uint32(size))
		b[25] = flags
		b[32] = byte(len(name))
		copy(b[33:], name)
		return b
	}
	pvd := image[16*2048 : 17*2048]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	pvd[6] = 1
	copy(pvd[156:], record(root, "\x00"))
	for _, n := range all {
		out := image[n.sector*2048 : (n.sector+1)*2048]
		if len(n.children) == 0 {
			copy(out, n.data)
		} else {
			offset := 0
			for _, c := range n.children {
				offset += copy(out[offset:], record(c, c.name))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "test.iso"), image, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredBootEntryIsAvailableOnFirstRequestAfterRestart(t *testing.T) {
	dir := t.TempDir()
	restartFixtureISO(t, dir)
	list, err := isos.Scan(dir)
	if err != nil || len(list) != 1 {
		t.Fatal(err, list)
	}
	iso := list[0]
	cache := t.TempDir()
	base := "http://server:8080"
	prime := &Server{ISODir: dir, CacheDir: cache, Base: base, NBDPort: 10809}
	payload, _ := json.Marshal(map[string]any{"format": 1, "stamp": stamp(iso), "facts": map[string]any{stamp(iso) + "|" + base + "/fs/" + iso.ID + "/newos/initrd": map[string]any{"flavor": "casper", "kernelVersion": "6.1", "NIC": false, "NBD": true}}})
	if err := prime.findings.RestoreStamp(stamp(iso), payload); err != nil {
		t.Fatal(err)
	}
	prime.saveFindings(iso, prime.findingsGeneration())
	probe := &Server{ISODir: dir, CacheDir: cache, Base: base, NBDPort: 10809}
	primary, ok := probe.cachedGeneric(iso)
	if !ok || !primary.HasOwnExtra() {
		t.Fatalf("cannot rebuild custom-extra primary: %+v", primary)
	}
	ways, ok := probe.cachedWays(iso, primary)
	if !ok || len(ways) != 1 {
		t.Fatalf("cannot rebuild alternatives: %+v", ways)
	}
	store, _, err := config.Open(filepath.Join(t.TempDir(), "settings.json"), config.Config{ISOEntries: map[string]config.ISOEntrySettings{iso.ID: {Default: ways[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	cold := &Server{ISODir: dir, CacheDir: cache, Base: base, NBDPort: 10809, Config: store}
	// No background inspection can complete during these first requests.
	cold.lookMu.Lock()
	defer cold.lookMu.Unlock()
	entries, err := cold.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatal(err, entries)
	}
	if !strings.Contains(strings.Join(entries[0].Commands, "\n"), "nomodeset") {
		t.Fatal("first menu ignored configured default", entries)
	}
	// Uploads and renames of other files use this same routine refresh.
	cold.Rescan()
	entries, err = cold.Entries()
	if err != nil || len(entries) != 1 || !strings.Contains(strings.Join(entries[0].Commands, "\n"), "nomodeset") {
		t.Fatal("routine refresh lost configured alternative", err, entries)
	}
	infos, err := cold.ISOInfos()
	if err != nil || len(infos[0].Ways) != 1 || infos[0].Ways[0].ID != ways[0].ID {
		t.Fatal("first admin request lost entry choices", err, infos)
	}
	// Independent requests may race to reconstruct, but must agree on the payload.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := cold.Entries()
			if err != nil || len(got) != 1 || !strings.Contains(strings.Join(got[0].Commands, "\n"), "nomodeset") {
				t.Errorf("concurrent request: %v %v", got, err)
			}
		}()
	}
	wg.Wait()
	if len(cold.looking) != 0 {
		t.Fatal("cache hit queued a background initramfs inspection")
	}
	moved := &Server{ISODir: dir, CacheDir: cache, Base: "http://other:8080", NBDPort: 10809}
	if _, ok := moved.cachedGeneric(iso); ok {
		t.Fatal("changed address reused old URL keys")
	}
}
