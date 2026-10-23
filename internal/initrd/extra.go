// The usual extra archive: what a live Linux needs to read its ISO from the network disk server instead of copying the ISO into memory.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initrd

import "fmt"

// Settings say where the ISO is offered as a network block disk.
type Settings struct {
	Host string // the server
	Port int    // its NBD port
	Name string // the export's name: the ISO's id, or "<id>@<partition>"
	// Block is the block size of the disk: 0 for an ISO's 2048 bytes, 512 for a partition of a disk image (a FAT file system cannot be mounted on a device with larger blocks)
	Block int
}

// BlockSize is the disk's block size in bytes.
func (s Settings) BlockSize() int {
	if s.Block == 0 {
		return 2048
	}
	return s.Block
}

// Extra is the archive for the live systems that find their ISO through a mount command or a network handler of their own (casper, live-boot, dracut, archiso): the helper
// that attaches the network block disk, under the names those systems call.
func Extra(s Settings, helper []byte) []byte {
	var b Builder
	b.Archive(s, helper)
	return b.Bytes()
}

// Archive adds the usual files to the builder.
func (b *Builder) Archive(s Settings, helper []byte) {
	// The whole ISO is never copied into the PC's memory, not even when the network disk fails: that needs RAM for all of it and fails in a way that is hard to read.
	// A PC that cannot use the network disk says so.
	fallback := fmt.Sprintf("echo 'Gola Bootlace: this PC could not read the ISO over the network (port %d on the server). Is a firewall in the way?' >&2", s.Port)
	b.Dir("usr")
	b.Dir("usr/bin")
	b.Script("usr/bin/nfsmount", fmt.Sprintf(nfsmount, ShellWord(s.Host), s.Port, ShellWord(s.Name), fallback, s.BlockSize()))
	b.File("usr/bin/bootlace-nbd", 0o755, helper)
	// the same program under the name of the client that Arch's start-up disk calls: it connects again when the connection is lost (see cmd/bootlace-nbd)
	b.File("usr/bin/nbd-client", 0o755, helper)
	// (no folder entry for sbin: in Fedora's start-up disk /usr/sbin is a link to bin, and a folder entry would replace it with an empty folder, hiding NetworkManager and the rest)
	b.Script("sbin/nbdroot", nbdroot)
}
