package handoff

import (
	"testing"
	"time"
)

func TestTheNextBootGoesToTheLoaderUntilItIsFetched(t *testing.T) {
	now := time.Now()
	r := &Registry{Now: func() time.Time { return now }}
	if _, ok := r.BootFile("00:15:5D:08:02:B5", true); ok {
		t.Fatal("nothing was chosen yet")
	}
	r.Set("00-15-5d-08-02-b5", "10.0.0.5", "abc")
	if f, ok := r.BootFile("00:15:5d:08:02:b5", true); !ok || f != "boot/loader.efi" {
		t.Fatalf("efi: %q %v", f, ok)
	}
	if f, _ := r.BootFile("00:15:5d:08:02:b5", false); f != "boot/pxeboot" {
		t.Fatalf("bios: %q", f)
	}
	if id, ok := r.ISO("10.0.0.5"); !ok || id != "abc" {
		t.Fatalf("iso: %q %v", id, ok)
	}
	r.Fetched("10.0.0.5")
	if _, ok := r.BootFile("00:15:5d:08:02:b5", true); ok {
		t.Fatal("after the loader was fetched the next boot is an ordinary one")
	}
	if _, ok := r.ISO("10.0.0.5"); !ok {
		t.Fatal("the loader goes on reading the ISO")
	}
}

func TestChoicesExpire(t *testing.T) {
	now := time.Now()
	r := &Registry{Now: func() time.Time { return now }}
	r.Set("aa:bb:cc:dd:ee:ff", "10.0.0.6", "x")
	now = now.Add(pendingFor + time.Second)
	if _, ok := r.BootFile("aa:bb:cc:dd:ee:ff", true); ok {
		t.Fatal("the hand-off should have expired")
	}
	if _, ok := r.ISO("10.0.0.6"); !ok {
		t.Fatal("the ISO choice outlives the hand-off")
	}
	now = now.Add(chosenFor)
	if _, ok := r.ISO("10.0.0.6"); ok {
		t.Fatal("the ISO choice should have expired")
	}
}
