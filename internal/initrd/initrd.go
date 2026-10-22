// Package initrd makes the extra initramfs archive that Bootlace appends to a live Linux's own: a few files that overwrite the ones of the initramfs of the same name. The
// kernel unpacks every archive of the initrd in turn, and later files replace earlier ones, so the ISO's initramfs is used as it is except for those files.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initrd

import (
	"bytes"
	"fmt"
	"strings"
)

// File is one entry of an archive.
type File struct {
	Name string // without a leading slash
	Mode uint32 // permission bits; directories get 040000 added
	Data []byte
	Dir  bool
}

// Archive writes the files in the "newc" cpio format the kernel reads.
func Archive(files []File) []byte {
	var b bytes.Buffer
	ino := 1
	put := func(name string, mode uint32, data []byte) {
		fmt.Fprintf(&b, "070701%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X", ino, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, len(name)+1, 0)
		b.WriteString(name)
		b.WriteByte(0)
		for b.Len()%4 != 0 {
			b.WriteByte(0)
		}
		b.Write(data)
		for b.Len()%4 != 0 {
			b.WriteByte(0)
		}
		ino++
	}
	for _, f := range files {
		if f.Dir {
			put(f.Name, 0o040000|f.Mode, nil)
		} else {
			put(f.Name, 0o100000|f.Mode, f.Data)
		}
	}
	put("TRAILER!!!", 0, nil)
	return b.Bytes()
}

// Settings say where the ISO is.
type Settings struct {
	Host string // the server
	Port int    // its NBD port
	Name string // the ISO's id: the NBD export
}

// nfsmount is what casper runs where it would mount an NFS share: "nfsmount -o options server:path mountpoint". It attaches the ISO as a network block disk
// and mounts it from there.
const nfsmount = `#!/bin/sh
# Gola Bootlace: stands in for klibc's nfsmount. The ISO is attached as a network block disk (read over the network a piece at a time) and mounted from there,
# so it is not copied into memory.
for a in "$@"; do mnt="$a"; done
mkdir -p "$mnt"
modprobe nbd >/dev/null 2>&1
modprobe isofs >/dev/null 2>&1
/usr/bin/bootlace-nbd -host %[1]s -port %[2]d -name %[3]s -dev /dev/nbd0 &
i=0
while [ "$i" -lt 30 ]; do
	size=$(cat /sys/block/nbd0/size 2>/dev/null)
	if [ -n "$size" ] && [ "$size" -gt 0 ]; then
		if mount -t iso9660 -o ro /dev/nbd0 "$mnt"; then
			exit 0
		fi
		break
	fi
	sleep 1
	i=$((i + 1))
done
echo "Gola Bootlace: the network disk did not work" >&2
%[4]s
exit 1
`

// Extra is the archive for a live Linux that casper starts from the ISO: nfsmount and the helper that attaches the network block disk.
func Extra(s Settings, helper []byte) []byte {
	// The whole ISO is never copied into the PC's memory, not even when the network disk fails: that needs RAM for all of it and fails in a way that is hard to read.
	// A PC that cannot use the network disk says so.
	fallback := fmt.Sprintf("echo 'Gola Bootlace: this PC could not read the ISO over the network (port %d on the server). Is a firewall in the way?' >&2", s.Port)
	return Archive([]File{
		{Name: "usr", Mode: 0o755, Dir: true},
		{Name: "usr/bin", Mode: 0o755, Dir: true},
		{Name: "usr/bin/nfsmount", Mode: 0o755, Data: []byte(fmt.Sprintf(nfsmount, shellWord(s.Host), s.Port, shellWord(s.Name), fallback))},
		{Name: "usr/bin/bootlace-nbd", Mode: 0o755, Data: helper},
	})
}

// shellWord keeps a value from being read as anything but one word.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
