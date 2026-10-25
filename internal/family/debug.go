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
//
// None of these opens a shell: that is the separate "debug shell" switch (ShellWords).
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
		return nil // (its installer only has the shell word, see ShellWords)
	case has("gentoo"):
		return []string{"rd.debug"} // (OpenRC, not systemd)
	case has("fedora", "red hat", "dracut", "opensuse"):
		return []string{"rd.debug", "systemd.show_status=1"}
	case has("arch", "systemrescue", "ubuntu", "debian live", "debian-style live"):
		return []string{"systemd.show_status=1"}
	}
	return nil
}

// ShellWords are the words that the "debug shell" switch adds to the kernel line of a kind of system, so that its start-up stops at a shell where a person can look around
// (network, disks, the logs) before the system goes on. Only words that the system's own start-up disk honours are listed, and only the ones that were checked on a real
// virtual machine started through Bootlace; a kind with no word here (Alpine, Arch, SystemRescue, Debian's installer, Tails, Windows, FreeBSD ...) has no shell switch.
//
//   - proxdebug: Proxmox VE's installer opens its debug shell before the installer starts. Leave it with exit (or Ctrl+D): the installer goes on.
//   - rd.break=pre-mount: dracut (Fedora, Red Hat's installers, openSUSE) stops in its emergency shell before it looks for the root file system. Leave it with exit.
//   - break=premount: initramfs-tools (Debian's live ISOs, Ubuntu's casper) stops at an (initramfs) prompt before it mounts the live system. Leave it with exit.
func ShellWords(family string) []string {
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
	case has("debian installer", "windows", "freebsd", "tails", "as a disk", "disk image"):
		return nil
	case has("proxmox"):
		return []string{"proxdebug"}
	case has("fedora", "red hat", "dracut", "opensuse"):
		return []string{"rd.break=pre-mount"}
	case has("ubuntu", "debian live", "debian-style live"):
		return []string{"break=premount"}
	}
	return nil
}
