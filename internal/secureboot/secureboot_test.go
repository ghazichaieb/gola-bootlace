package secureboot

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// sample is a manifest and files that match each other.
func sample() (Manifest, map[string][]byte) {
	files := map[string][]byte{Shim: []byte("MZ shim"), SecondStage: []byte("MZ ipxe")}
	m := Manifest{Source: "https://example.test/ipxeboot.tar.gz", Release: "v9.9.9", Released: "2030-01-01", Added: "2030-01-02", TarballSHA256: strings.Repeat("a", 64), Folder: "x86_64-sb"}
	m.Files = []File{{Name: Shim, From: "shimx64.efi", Size: int64(len(files[Shim])), SHA256: sum(files[Shim])}, {Name: SecondStage, From: "ipxe.efi", Size: int64(len(files[SecondStage])), SHA256: sum(files[SecondStage])}}
	return m, files
}

func TestTheBuiltInFilesAreTheOnesTheManifestListsAndNothingElse(t *testing.T) {
	s := Builtin()
	if err := s.Err(); err != nil {
		t.Fatalf("the files in the program do not match assets/MANIFEST.json: %v", err)
	}
	if s != Builtin() {
		t.Error("checked once, the same set every time")
	}
	m := s.Manifest
	if m.Release != "v2.0.0" || m.Folder != "x86_64-sb" || m.TarballSHA256 != "01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1" ||
		m.Source != "https://github.com/ipxe/ipxe/releases/download/v2.0.0/ipxeboot.tar.gz" {
		t.Errorf("the manifest does not describe the iPXE project's v2.0.0 x86_64-sb files: %+v", m)
	}
	if got := strings.Join(s.Names(), " "); got != Shim+" "+SecondStage {
		t.Errorf("the set must hold the two x86-64 files and nothing else (no ARM64): %q", got)
	}
	for _, name := range s.Names() {
		b, ok := s.Open(name)
		if !ok || len(b) < 100000 || string(b[:2]) != "MZ" {
			t.Errorf("%s: %d bytes, ok=%v: not an EFI program", name, len(b), ok)
		}
	}
	infos := s.Infos()
	if len(infos) != 2 || infos[0].Name != Shim || infos[1].Name != SecondStage || infos[0].From != "ipxe-shimx64.efi" || infos[0].Role == "" {
		t.Errorf("infos: %+v", infos)
	}
}

// A PC whose firmware dropped Microsoft's 2011 third-party CA (found on an ASUS ExpertBook B3405CCA) refuses a shim that only the 2011 CA signed, and a PC that has not
// received the 2023 CA yet refuses one that only the 2023 CA signed: the shim has to carry both signatures. The manifest says so and the bytes have to agree.
func TestTheShimIsSignedWithBothMicrosoftCAs(t *testing.T) {
	s := Builtin()
	shim, _ := s.Open(Shim)
	for ca, name := range map[string]string{"2011": "Microsoft Corporation UEFI CA 2011", "2023": "Microsoft UEFI CA 2023"} {
		if !strings.Contains(string(shim), name) {
			t.Errorf("the shim does not carry the %q certificate: it would be refused by a firmware that trusts only the %s CA", name, ca)
		}
	}
	info := s.Infos()[0]
	if got := strings.Join(info.SignedWith, " "); got != "2011 2023" {
		t.Errorf("the manifest says the shim is signed with %q, want \"2011 2023\"", got)
	}
	if info.Release != "ipxe-16.1" || info.Source != "https://github.com/ipxe/shim/releases/download/ipxe-16.1/ipxe-shimx64.efi" {
		t.Errorf("the manifest does not say where the re-signed shim came from: %+v", info)
	}
}

func TestOnlyTheExactNamesAreServed(t *testing.T) {
	m, files := sample()
	s := NewSet(m, files)
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{Shim, SecondStage} {
		if _, ok := s.Open(name); !ok {
			t.Errorf("%s must be served", name)
		}
	}
	for _, name := range []string{"", "IPXE.EFI", "Ipxe-shimx64.efi", "shimx64.efi", "MANIFEST.json", "../ipxe.efi", "sb/ipxe.efi", `..\ipxe.efi`, "ipxe.efi ", "ipxe.efi/", "./ipxe.efi", "ipxe.efi\x00", "grubx64.efi"} {
		if b, ok := s.Open(name); ok {
			t.Errorf("%q must not be served (%d bytes)", name, len(b))
		}
	}
}

func TestASetThatDoesNotMatchItsManifestServesNothing(t *testing.T) {
	cases := map[string]func(m *Manifest, files map[string][]byte){
		"a changed byte":           func(m *Manifest, f map[string][]byte) { f[Shim] = []byte("MZ shiM") },
		"a changed length":         func(m *Manifest, f map[string][]byte) { f[SecondStage] = append(f[SecondStage], 0) },
		"a wrong size":             func(m *Manifest, f map[string][]byte) { m.Files[0].Size++ },
		"a wrong checksum":         func(m *Manifest, f map[string][]byte) { m.Files[1].SHA256 = strings.Repeat("0", 64) },
		"a checksum with capitals": func(m *Manifest, f map[string][]byte) { m.Files[1].SHA256 = strings.ToUpper(m.Files[1].SHA256) },
		"a missing file":           func(m *Manifest, f map[string][]byte) { delete(f, SecondStage) },
		"an unlisted file":         func(m *Manifest, f map[string][]byte) { f["extra.efi"] = []byte("x") },
		"a missing entry":          func(m *Manifest, f map[string][]byte) { m.Files = m.Files[:1] },
		"a doubled entry":          func(m *Manifest, f map[string][]byte) { m.Files = append(m.Files, m.Files[0]) },
		"another file instead": func(m *Manifest, f map[string][]byte) {
			m.Files[0].Name = "shimaa64.efi"
			f["shimaa64.efi"] = f[Shim]
			delete(f, Shim)
		},
		"no release":          func(m *Manifest, f map[string][]byte) { m.Release = "" },
		"no archive checksum": func(m *Manifest, f map[string][]byte) { m.TarballSHA256 = "" },
	}
	for name, mutate := range cases {
		m, files := sample()
		m.Files = append([]File(nil), m.Files...)
		mutate(&m, files)
		s := NewSet(m, files)
		if s.Err() == nil {
			t.Errorf("%s: must be refused", name)
		}
		if _, ok := s.Open(Shim); ok {
			t.Errorf("%s: a set that does not match serves nothing", name)
		}
		if _, ok := s.Open(SecondStage); ok {
			t.Errorf("%s: a set that does not match serves nothing", name)
		}
		if len(s.Names()) != 0 {
			t.Errorf("%s: names of a refused set: %v", name, s.Names())
		}
	}
	var none *Set
	if none.Err() == nil {
		t.Error("no set is an error")
	}
	if _, ok := none.Open(Shim); ok {
		t.Error("no set serves nothing")
	}
}
