package family

import (
	"slices"
	"testing"
)

func TestDebugWordsAreChosenByTheKindOfSystem(t *testing.T) {
	for family, want := range map[string][]string{
		"Alpine Linux":                            {"debug_init"},
		"Proxmox VE":                              {"proxdebug"},
		"Fedora (live)":                           {"rd.debug", "systemd.show_status=1"},
		"Red Hat family (installer)":              {"rd.debug", "systemd.show_status=1"},
		"openSUSE":                                {"rd.debug", "systemd.show_status=1"},
		"dracut live, by its boot menu":           {"rd.debug", "systemd.show_status=1"},
		"Gentoo (live)":                           {"rd.debug"},
		"Arch Linux":                              {"systemd.show_status=1"},
		"Arch Linux (kernel in the EFI image)":    {"systemd.show_status=1"},
		"SystemRescue":                            {"systemd.show_status=1"},
		"Ubuntu (casper)":                         {"systemd.show_status=1"},
		"Ubuntu-style (casper), by its boot menu": {"systemd.show_status=1"},
		"Debian live":                             {"systemd.show_status=1"},
		"Debian-style live, by its boot menu":     {"systemd.show_status=1"},
		"Debian installer":                        nil,
		"Tails":                                   nil,
		"Tiny Core":                               nil,
		"ShredOS":                                 nil,
		"Porteus (whole ISO in memory)":           nil,
		"Lakka / LibreELEC":                       nil,
		"FreeBSD, experimental":                   nil,
		"Windows setup":                           nil,
		"":                                        nil,
	} {
		if got := DebugWords(family); !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", family, got, want)
		}
	}
}
