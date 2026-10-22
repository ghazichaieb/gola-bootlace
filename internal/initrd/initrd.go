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
	// Block is the block size of the disk: 0 for an ISO's 2048 bytes, 512 for a partition of a disk image (a FAT file system cannot be mounted on a device with larger blocks)
	Block int
}

func (s Settings) block() int {
	if s.Block == 0 {
		return 2048
	}
	return s.Block
}

// nfsmount is what casper runs where it would mount an NFS share: "nfsmount -o options server:path mountpoint". It attaches the ISO as a network block disk
// and mounts it from there.
const nfsmount = `#!/bin/sh
# Gola Bootlace: stands in for klibc's nfsmount. The ISO is attached as a network block disk (read over the network a piece at a time) and mounted from there,
# so it is not copied into memory.
for a in "$@"; do mnt="$a"; done
mkdir -p "$mnt"
# The live system configures the network again once it has started (netplan, systemd-networkd). Asking the DHCP server again as a new client can give it another address,
# and the open connection to the network disk then breaks (every read fails). So the address this PC has now is kept: the netplan file made from "ip=dhcp" gets it as a
# fixed address, with the gateway and name servers.
for conf in /run/net-*.conf; do
	[ -f "$conf" ] || continue
	DEVICE=""; IPV4ADDR=""; IPV4NETMASK=""; IPV4GATEWAY=""; IPV4DNS0=""; IPV4DNS1=""
	. "$conf"
	y="/run/netplan/$DEVICE.yaml"
	[ -n "$DEVICE" ] && [ -n "$IPV4ADDR" ] && [ -n "$IPV4NETMASK" ] && [ -f "$y" ] || continue
	prefix=0
	oldifs=$IFS; IFS=.
	for o in $IPV4NETMASK; do
		case "$o" in 255) prefix=$((prefix + 8));; 254) prefix=$((prefix + 7));; 252) prefix=$((prefix + 6));; 248) prefix=$((prefix + 5));; 240) prefix=$((prefix + 4));; 224) prefix=$((prefix + 3));; 192) prefix=$((prefix + 2));; 128) prefix=$((prefix + 1));; esac
	done
	IFS=$oldifs
	ns=""
	for d in "$IPV4DNS0" "$IPV4DNS1"; do
		case "$d" in ""|0.0.0.0) ;; *) ns="$ns${ns:+, }$d";; esac
	done
	# (the file may have routes and name servers already: a key twice makes it invalid and netplan then ignores the whole file)
	has_routes=""; has_ns=""
	grep -q "routes:" "$y" && has_routes=1
	grep -q "nameservers:" "$y" && has_ns=1
	while IFS= read -r line; do
		case "$line" in
		*dhcp-identifier:*) ;;
		*dhcp4:*)
			echo "      dhcp4: false"
			echo "      addresses: [$IPV4ADDR/$prefix]"
			if [ -z "$has_routes" ]; then
				case "$IPV4GATEWAY" in ""|0.0.0.0) ;; *) echo "      routes:"; echo "        - to: default"; echo "          via: $IPV4GATEWAY";; esac
			fi
			if [ -z "$has_ns" ] && [ -n "$ns" ]; then echo "      nameservers:"; echo "        addresses: [$ns]"; fi
			;;
		*) echo "$line";;
		esac
	done < "$y" > "$y.bootlace" && mv "$y.bootlace" "$y"
done
modprobe nbd >/dev/null 2>&1
modprobe isofs >/dev/null 2>&1
modprobe vfat >/dev/null 2>&1
/usr/bin/bootlace-nbd -host %[1]s -port %[2]d -name %[3]s -dev /dev/nbd0 -block %[5]d &
i=0
while [ "$i" -lt 30 ]; do
	size=$(cat /sys/block/nbd0/size 2>/dev/null)
	if [ -n "$size" ] && [ "$size" -gt 0 ]; then
		# an ISO, or (a disk image's partition: Tails) a FAT file system
		if mount -t iso9660 -o ro /dev/nbd0 "$mnt" 2>/dev/null || mount -t vfat -o ro /dev/nbd0 "$mnt"; then
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

// nbdroot is what dracut (Fedora and its relatives) runs for "netroot=nbd:host:port:name": it attaches the ISO as a network block disk, and the live system's
// "root=live:/dev/nbd0" finds it there. Their start-up disks have the kernel's nbd driver but not always iSCSI, which is why this is used instead of an iSCSI login.
const nbdroot = `#!/bin/sh
# Gola Bootlace: dracut's netroot handler for netroot=nbd:host:port:name
rest="${2#nbd:}"
host="${rest%%%%:*}"; rest="${rest#*:}"
port="${rest%%%%:*}"; name="${rest#*:}"
modprobe nbd >/dev/null 2>&1
modprobe isofs >/dev/null 2>&1
/usr/bin/bootlace-nbd -host "$host" -port "$port" -name "$name" -dev /dev/nbd0 &
i=0
while [ "$i" -lt 60 ]; do
	size=$(cat /sys/block/nbd0/size 2>/dev/null)
	[ -n "$size" ] && [ "$size" -gt 0 ] && exit 0
	sleep 1
	i=$((i + 1))
done
echo "Gola Bootlace: the network disk did not come up" >&2
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
		{Name: "usr/bin/nfsmount", Mode: 0o755, Data: []byte(fmt.Sprintf(nfsmount, shellWord(s.Host), s.Port, shellWord(s.Name), fallback, s.block()))},
		{Name: "usr/bin/bootlace-nbd", Mode: 0o755, Data: helper},
		// the same program under the name of the client that Arch's start-up disk calls: it connects again when the connection is lost (see cmd/bootlace-nbd)
		{Name: "usr/bin/nbd-client", Mode: 0o755, Data: helper},
		// (no directory entry for sbin: in Fedora's start-up disk /usr/sbin is a link to bin, and a directory entry would replace it with an empty folder, hiding NetworkManager and the rest)
		{Name: "sbin/nbdroot", Mode: 0o755, Data: []byte(fmt.Sprintf(nbdroot))},
	})
}

// shellWord keeps a value from being read as anything but one word.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
