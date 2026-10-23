package httpd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func cacheEntry(t *testing.T, dir, name string, size int, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestAFullCacheRemovesTheLeastRecentlyUsedEntriesNothingIsUsing(t *testing.T) {
	local := t.TempDir()
	s := &Server{CacheDir: local, CacheSettings: func() (string, int64) { return "local", 1000 }}
	oldest := cacheEntry(t, local, "boot-aaaaaaaaaa-0123456789.wim", 400, 72*time.Hour)
	middle := cacheEntry(t, filepath.Join(local, "images"), "disk-bbbbbbbbbb-0123456789.img", 400, 48*time.Hour)
	id := isos.IDOf("Some/Folder/in-use.iso") // a PC is reading this ISO: its entry is the oldest of all but must stay
	inUse := cacheEntry(t, local, "boot-"+id+"-0123456789.wim", 100, 96*time.Hour)
	s.using = map[string]int{"Some/Folder/in-use.iso": 1}
	recent := cacheEntry(t, local, "boot-dddddddddd-0123456789.wim", 50, time.Minute)
	stranger := cacheEntry(t, local, "notes.txt", 7, 200*time.Hour) // not a name the cache gives: never touched

	// 950 bytes are in the cache: 300 more do not fit, and removing the oldest unused entry (400) is enough
	if got := s.cachePlace(300); got != local {
		t.Fatalf("local only: %s", got)
	}
	if exists(oldest) || !exists(middle) || !exists(inUse) || !exists(recent) || !exists(stranger) {
		t.Fatalf("only the oldest unused one should go: %v %v %v %v %v", exists(oldest), exists(middle), exists(inUse), exists(recent), exists(stranger))
	}
	if s.cacheFullNote() != "" {
		t.Error("there was room after that, no warning")
	}
	// 900 more: the next oldest goes, then only an entry in use and a recent one are left, so it still does not fit and the page is told why
	s.cachePlace(900)
	if exists(middle) || !exists(inUse) || !exists(recent) || !exists(stranger) {
		t.Fatalf("%v %v %v %v", exists(middle), exists(inUse), exists(recent), exists(stranger))
	}
	if s.cacheFullNote() == "" {
		t.Error("nothing more could be removed: the web page must say so")
	}
}

func TestEvictionStopsAsSoonAsThereIsRoom(t *testing.T) {
	local := t.TempDir()
	s := &Server{CacheDir: local, CacheSettings: func() (string, int64) { return "local", 1000 }}
	a := cacheEntry(t, local, "boot-aaaaaaaaaa-0123456789.wim", 400, 72*time.Hour)
	b := cacheEntry(t, local, "boot-bbbbbbbbbb-0123456789.wim", 400, 48*time.Hour)
	c := cacheEntry(t, local, "boot-cccccccccc-0123456789.wim", 100, 24*time.Hour)
	s.cachePlace(300) // 900 + 300 > 1000: one entry (the oldest, 400) is enough
	if exists(a) || !exists(b) || !exists(c) {
		t.Fatalf("only the oldest should go: %v %v %v", exists(a), exists(b), exists(c))
	}
	if s.cacheFullNote() != "" {
		t.Error("there was room, so no warning")
	}
}

func TestUsingAnEntryMakesItTheNewest(t *testing.T) {
	local := t.TempDir()
	s := &Server{CacheDir: local}
	p := cacheEntry(t, local, "boot-aaaaaaaaaa-0123456789.wim", 10, 100*time.Hour)
	if got, ok := s.cacheFind("boot-aaaaaaaaaa-0123456789.wim"); !ok || got != p {
		t.Fatal(got, ok)
	}
	if st, _ := os.Stat(p); time.Since(st.ModTime()) > time.Minute {
		t.Fatalf("a use must refresh the time: %v", st.ModTime())
	}
}

func TestTheUsageListsEntriesOldestFirstWithWhatIsInUse(t *testing.T) {
	lib := t.TempDir()
	id, err := library.Prepare(lib)
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	s := &Server{Library: library.New(lib, id), CacheDir: local, ISODir: lib}
	cacheEntry(t, local, "boot-aaaaaaaaaa-0123456789.wim", 10, 10*time.Hour)
	cacheEntry(t, filepath.Join(lib, library.MetaDir, "cache"), "boot-bbbbbbbbbb-0123456789.wim", 20, 20*time.Hour)
	u := s.CacheUsage()
	if len(u.Entries) != 2 || u.Entries[0].Where != "library" || u.Entries[1].Where != "container" || u.Entries[0].InUse {
		t.Fatalf("%+v", u.Entries)
	}
}
