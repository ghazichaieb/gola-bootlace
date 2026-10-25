package family

import (
	"slices"
	"testing"
)

func TestDebugWordsAreChosenByTheKindOfSystem(t *testing.T) {
	for family, want := range map[string][]string{
		"Alpine Linux":                            {"debug_init"},
		"Proxmox VE":                              nil, // (its only word is the shell word)
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

func TestShellWordsAreOnlyThoseOfSystemsWhoseStartUpHonoursThem(t *testing.T) {
	for family, want := range map[string][]string{
		"Proxmox VE":                              {"proxdebug"},
		"Fedora (live)":                           {"rd.break=pre-mount"},
		"Red Hat family (installer)":              {"rd.break=pre-mount"},
		"openSUSE":                                {"rd.break=pre-mount"},
		"dracut live, by its boot menu":           {"rd.break=pre-mount"},
		"Debian live":                             {"break=premount"},
		"Debian-style live, by its boot menu":     {"break=premount"},
		"Ubuntu (casper)":                         {"break=premount"},
		"Ubuntu-style (casper), by its boot menu": {"break=premount"},
		"Debian installer":                        nil,
		"Alpine Linux":                            nil,
		"Arch Linux":                              nil,
		"SystemRescue":                            nil,
		"Gentoo (live)":                           nil,
		"Tails":                                   nil,
		"Windows setup":                           nil,
		"FreeBSD, experimental":                   nil,
		"Some ISO as a disk":                      nil,
		"":                                        nil,
	} {
		if got := ShellWords(family); !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", family, got, want)
		}
	}
}

func TestNoDebugWordOpensAShellAndNoShellWordIsADebugWord(t *testing.T) {
	for _, family := range []string{"Proxmox VE", "Fedora (live)", "Debian live", "Ubuntu (casper)", "Alpine Linux", "Arch Linux", "openSUSE"} {
		for _, shell := range ShellWords(family) {
			if slices.Contains(DebugWords(family), shell) {
				t.Errorf("%s: verbose start-up must not carry the shell word %q", family, shell)
			}
		}
	}
}
