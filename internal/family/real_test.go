package family

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
)

// knownIsos is what each ISO that has been tested on a PC is recognised as, by the start of its file name. They are real files, so this runs only when
// BOOTLACE_TEST_ISODIR names the folder that has them (a file that is not there is skipped).
var knownIsos = []struct{ prefix, family string }{
	{"alpine-standard", "Alpine Linux"},
	{"archlinux-", "Arch Linux"},
	{"bazzite", "Fedora (live)"},
	{"cachyos", "Arch Linux"},
	{"chimeraos", "Arch Linux (kernel in the EFI image)"},
	{"clonezilla", "Debian live"},
	{"debian-13.7.0-amd64-netinst", "Debian installer"},
	{"debian-live", "Debian live"},
	{"Fedora-Workstation", "Fedora (live)"},
	{"gparted", "Debian live"},
	{"kali-linux", "Debian installer"},
	{"linuxmint", "Ubuntu (casper)"},
	{"Nobara", "Fedora (live)"},
	{"pop-os", "Ubuntu (casper)"},
	{"Porteus", "Porteus (whole ISO in memory)"},
	{"proxmox-ve", "Proxmox VE"},
	{"Qubes", "Red Hat family (installer)"},
	{"rescuezilla", "Ubuntu (casper)"},
	{"TrueNAS-SCALE", "Debian live"},
	{"ubuntu-", "Ubuntu (casper)"},
	{"vyos", "Debian live"},
	{"tails", "Tails"},
	{"Lakka", "Lakka / LibreELEC"},
}

func TestTheIsosThatWereTestedOnAPcAreStillRecognisedAndTheirArchivesStillBuild(t *testing.T) {
	dir := os.Getenv("BOOTLACE_TEST_ISODIR")
	if dir == "" {
		t.Skip("BOOTLACE_TEST_ISODIR is not set")
	}
	entries, _ := os.ReadDir(dir)
	u := withISCSI(withNBD(urls))
	for _, e := range entries {
		var want string
		for _, k := range knownIsos {
			if strings.HasPrefix(e.Name(), k.prefix) {
				want = k.family
			}
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if want == "" || (ext != ".iso" && ext != ".img") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			c := lookInside(t, f, st.Size(), ext == ".img", u)
			if c == nil {
				return
			}
			b, ok := DetectIn(c)
			if !ok || b.Family != want {
				t.Fatalf("recognised as %q (%v), want %q", b.Family, ok, want)
			}
			if data, err := b.BuildExtra(ExtraEnv{Settings: settings("abc", 0), Helper: []byte("helper")}); err != nil || len(data) == 0 {
				t.Fatalf("the extra archive: %d bytes, %v", len(data), err)
			}
		})
	}
}

// lookInside opens an ISO (or the first partition of a disk image that holds files) the way the server does.
func lookInside(t *testing.T, f *os.File, size int64, disk bool, u URLs) *Context {
	t.Helper()
	if disk {
		parts, err := disc.OpenDisk(f, size)
		if err != nil || len(parts) == 0 {
			t.Skip("no readable partition")
		}
		return &Context{FS: parts[0].FS, URLs: u}
	}
	img, err := disc.Open(f, size)
	if err != nil {
		t.Skip("no readable file system")
		return nil
	}
	c := &Context{FS: img, URLs: u}
	c.Volumes = func() []Volume {
		parts, err := disc.OpenDisk(f, size)
		if err != nil {
			return nil
		}
		var out []Volume
		for _, p := range parts {
			out = append(out, Volume{FS: p.FS, URL: "http://s/fs/abc@" + string(rune('0'+p.Number))})
		}
		return out
	}
	return c
}

// What the generic path (no names, only the ISO's menu and its start-up disk) must make of the ISOs that the named families know: the same make, with the arguments that make
// needs. An ISO of a make that is not handled generically is not listed.
var genericExpectations = map[string]struct{ label, args string }{
	"Ubuntu (casper)":                      {"Ubuntu-style (casper)", "boot=casper netboot=nfs nfsroot="},
	"Debian live":                          {"Debian-style live", "boot=live components"},
	"Arch Linux":                           {"Arch-style (archiso)", "archiso_nbd_srv="},
	"Arch Linux (kernel in the EFI image)": {"Arch-style (archiso, no network hooks)", "archisodevice=/dev/nbd0"},
	"Fedora (live)":                        {"dracut live", "netroot=nbd:"},
	"Tails":                                {"Debian-style live", "module=Tails"},
}

func TestTheGenericPathReachesTheSameMakeAsTheNamesOnTheTestedIsos(t *testing.T) {
	dir := os.Getenv("BOOTLACE_TEST_ISODIR")
	if dir == "" {
		t.Skip("BOOTLACE_TEST_ISODIR is not set")
	}
	entries, _ := os.ReadDir(dir)
	u := withISCSI(withNBD(urls))
	for _, e := range entries {
		var named string
		for _, k := range knownIsos {
			if strings.HasPrefix(e.Name(), k.prefix) {
				named = k.family
			}
		}
		want, ok := genericExpectations[named]
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !ok || (ext != ".iso" && ext != ".img") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			c := lookInside(t, f, st.Size(), ext == ".img", u)
			b, ok := DetectGeneric(c)
			if !ok || !strings.HasPrefix(b.Family, want.label) {
				t.Fatalf("generic: %q (%v), want %q (named: %q)", b.Family, ok, want.label, named)
			}
			if !strings.Contains(lines(b), want.args) {
				t.Errorf("lacks %q:\n%s", want.args, lines(b))
			}
			t.Logf("%s\n%s", b.Family, lines(b))
		})
	}
}
