package family

import (
	"bytes"
	"strings"
	"testing"
)

func TestSavedFindingsRebuildExtraBuildersWithoutInspectingInitrds(t *testing.T) {
	iso := casperISO(manyEntries)
	iso["/newos/initrd"] = initramfs(append([]string{"scripts/casper"}, modules(false, true)...)...)
	var original Findings
	c := &Context{FS: iso, URLs: withNBD(urls), Stamp: "image|123|456", Findings: &original}
	b, ok := DetectGeneric(c)
	if !ok || !b.HasOwnExtra() {
		t.Fatal("fixture not recognized")
	}
	want := Ways(c, b)
	data, err := original.MarshalStamp(c.Stamp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Commands")) || bytes.Contains(data, []byte("ExtraInitrd")) {
		t.Fatal("generated commands serialized")
	}
	var restored Findings
	if err := restored.RestoreStamp(c.Stamp, data); err != nil {
		t.Fatal(err)
	}
	// The ISO tree remains, but its large start-up disk can no longer be decoded.
	iso["/newos/initrd"] = []byte("not an initramfs")
	c.Findings = restored.CachedOnly()
	got, ok := DetectGeneric(c)
	if !ok || got.Kind != b.Kind || got.HasOwnExtra() != b.HasOwnExtra() {
		t.Fatalf("cannot rebuild boot: %+v", got)
	}
	ways := Ways(c, got)
	if len(ways) != len(want) {
		t.Fatalf("lost alternatives: %d want %d", len(ways), len(want))
	}
	for i := range ways {
		if ways[i].ID != want[i].ID || strings.Join(ways[i].Commands, "\n") != strings.Join(want[i].Commands, "\n") {
			t.Fatal("changed alternative")
		}
	}
	if err := restored.RestoreStamp("image|123|457", data); err == nil {
		t.Fatal("changed ISO accepted")
	}
	empty := (&Findings{}).CachedOnly()
	if _, ok := empty.remember("missing", func() (facts, bool) { t.Fatal("cache-only look inspected disk"); return facts{}, true }); ok {
		t.Fatal("missing entry accepted")
	}
}

func TestSavedFindingsRejectCorruptionAtomically(t *testing.T) {
	for _, bad := range []string{`{`, `{"format":2,"stamp":"iso"}`, `{"format":1,"stamp":"iso","facts":{"other|x":{"flavor":"casper"}}}`, `{"format":1,"stamp":"iso","facts":{"iso|x":{"flavor":"new-strategy"}}}`, strings.Repeat("x", maxSavedFindingsBytes+1)} {
		var kept Findings
		if err := kept.RestoreStamp("iso", []byte(bad)); err == nil {
			t.Fatalf("accepted malformed snapshot %.80s", bad)
		}
		if len(kept.facts) != 0 {
			t.Fatal("invalid cache partially installed")
		}
	}
}
