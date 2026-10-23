package handoff

import (
	"net"
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

func TestLoaderAddressIsKnownByMACForFreeBSDOnly(t *testing.T) {
	now := time.Now()
	r := &Registry{Now: func() time.Time { return now }}
	if _, ok := r.LoaderAddress("04:42:1a:ed:ec:e6"); ok {
		t.Fatal("nothing was chosen yet")
	}
	r.Set("04-42-1A-ED-EC-E6", "192.168.8.2", "abc")
	ip, ok := r.LoaderAddress("04:42:1a:ed:ec:e6")
	if !ok || !ip.Equal(net.IPv4(192, 168, 8, 2)) {
		t.Fatalf("got %v %v", ip, ok)
	}
	r.Fetched("192.168.8.2") // the loader was fetched: the PC is no longer pending, but its address is still known
	if _, ok := r.LoaderAddress("04:42:1a:ed:ec:e6"); !ok {
		t.Error("the address must survive the fetch of the loader")
	}
	r.SetKind("aa:bb:cc:dd:ee:01", "192.168.8.3", "abc", Windows)
	if _, ok := r.LoaderAddress("aa:bb:cc:dd:ee:01"); ok {
		t.Error("only FreeBSD hand-offs ask for this")
	}
	now = now.Add(chosenFor + time.Minute)
	if _, ok := r.LoaderAddress("04:42:1a:ed:ec:e6"); ok {
		t.Error("it must expire")
	}
}
