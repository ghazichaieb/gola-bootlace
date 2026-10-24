// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import "strings"

// DebugWords are the words that "verbose start-up" adds to the kernel line of a kind of system, besides the ones every kernel knows (loglevel=7, ignore_loglevel, debug).
// Each one is a word that the system's own start-up disk or init honours, so that it shows where it stops:
//
//   - rd.debug: dracut (Fedora and what is built on it, Red Hat's installers, openSUSE, Gentoo's live ISOs) prints every step of its start-up scripts.
//   - systemd.show_status=1: systemd lists the services as they start, which "quiet" otherwise hides (the live systems of Fedora, openSUSE, Arch, SystemRescue, Debian,
//     Ubuntu and what is built on them).
//   - debug_init: Alpine's start-up script traces itself.
//   - proxdebug: Proxmox VE's installer opens its debug shell before the installer starts (leave it with exit).
//
// The words are chosen by the family name Boot.Family has; a kind that has no word of its own (Tiny Core, ShredOS, Tails, a Windows or a FreeBSD start) gets none.
func DebugWords(family string) []string {
	f := strings.ToLower(family)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(f, p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("tails", "debian installer", "windows", "freebsd", "tiny core", "shredos", "porteus", "libreelec", "lakka", "as a disk", "disk image"):
		return nil
	case has("alpine"):
		return []string{"debug_init"}
	case has("proxmox"):
		return []string{"proxdebug"}
	case has("gentoo"):
		return []string{"rd.debug"} // (OpenRC, not systemd)
	case has("fedora", "red hat", "dracut", "opensuse"):
		return []string{"rd.debug", "systemd.show_status=1"}
	case has("arch", "systemrescue", "ubuntu", "debian live", "debian-style live"):
		return []string{"systemd.show_status=1"}
	}
	return nil
}
