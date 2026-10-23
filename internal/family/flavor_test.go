package family

import (
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// listing is what an initramfs has: the names of its files and of its modules.
func listing(names []string, mods ...string) *initread.Listing {
	l := &initread.Listing{Names: map[string]bool{}, Have: map[string]bool{}, Version: "6.1.0"}
	for _, n := range names {
		l.Names[n] = true
	}
	for _, m := range mods {
		l.Have[m] = true
	}
	return l
}

func TestAStartUpDiskIsToldByTheScriptsItIsMadeOf(t *testing.T) {
	nics := []string{"e1000e", "r8169", "igb"}
	for name, tc := range map[string]struct {
		l    *initread.Listing
		want facts
	}{
		"casper":              {listing([]string{"scripts/casper", "scripts/live"}, nics...), facts{flavor: flavorCasper, nicDrivers: true}},
		"live-boot":           {listing([]string{"scripts/live", "usr/bin/nfsmount"}, "nbd"), facts{flavor: flavorLiveBoot, nbdDriver: true}},
		"archiso":             {listing([]string{"hooks/archiso", "hooks/archiso_loop_mnt"}), facts{flavor: flavorArchiso}},
		"archiso with hooks":  {listing([]string{"hooks/archiso", "hooks/archiso_pxe_nbd"}), facts{flavor: flavorArchiso, archisoNet: true}},
		"dracut":              {listing([]string{"usr/bin/dmsquash-live-root", "usr/bin/netroot", "usr/bin/iscsiroot", "usr/bin/livenetroot"}), facts{flavor: flavorDracut, netroot: true, iscsi: true, livenet: true}},
		"dracut, no network":  {listing([]string{"usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh"}), facts{flavor: flavorDracut}},
		"dracut without live": {listing([]string{"usr/bin/dracut-cmdline", "usr/bin/netroot"}), facts{}},
		"nothing known":       {listing([]string{"init", "bin/busybox"}, nics...), facts{nicDrivers: true}},
		"two cards are few":   {listing([]string{"scripts/casper"}, "e1000e", "r8169"), facts{flavor: flavorCasper}},
	} {
		got := identify(tc.l)
		want := tc.want
		want.version = "6.1.0"
		if got != want {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
}

func TestWhatWasFoundInAStartUpDiskIsKeptAndNotLookedForAgain(t *testing.T) {
	var kept Findings
	calls := 0
	look := func() (facts, bool) { calls++; return facts{flavor: flavorCasper}, true }
	for i := 0; i < 3; i++ {
		if f, ok := kept.remember("a", look); !ok || f.flavor != flavorCasper {
			t.Fatal("not found")
		}
	}
	kept.remember("b", look)
	if calls != 2 {
		t.Errorf("looked %d times, want once for each of two", calls)
	}
	// a look that failed is not kept: the file may be readable next time
	failing := func() (facts, bool) { calls++; return facts{}, false }
	kept.remember("c", failing)
	kept.remember("c", failing)
	if calls != 4 {
		t.Errorf("a failure was kept (%d looks)", calls)
	}
	// no cache: every call looks
	var none *Findings
	none.remember("a", look)
	none.remember("a", look)
	if calls != 6 {
		t.Errorf("%d looks", calls)
	}
}
