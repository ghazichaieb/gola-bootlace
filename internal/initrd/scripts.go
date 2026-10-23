// Shell scripts that go into the extra archive, and helpers to write the ones that differ only in a few values.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package initrd

import (
	"fmt"
	"strings"
)

// ShellWord keeps a value from being read as anything but one word.
func ShellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'''`) + "'" }

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
host="${rest%%:*}"; rest="${rest#*:}"
port="${rest%%:*}"; name="${rest#*:}"
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

// UdhcpcScript is what busybox's udhcpc runs when it has an address (for start-up disks that have no network configuration of their own).
const UdhcpcScript = `#!/bin/sh
# Gola Bootlace: what busybox's udhcpc runs when it has an address.
case "$1" in
bound|renew)
    ifconfig "$interface" "$ip" netmask "${subnet:-255.255.255.0}" up
    for r in $router; do
        route add default gw "$r" dev "$interface" 2>/dev/null
    done
    ;;
esac
exit 0
`

// RdinitWrapper is the script a kernel runs in place of the initramfs's /init when "rdinit=/bootlace-init" is on its command line: body does what is needed before the
// initramfs starts (load drivers, fix a line of its script), and then the initramfs's own /init takes over. This works for any initramfs, whatever it is made of.
func RdinitWrapper(body string) string {
	return "#!/bin/sh\n# Gola Bootlace\n" + strings.TrimRight(body, "\n") + "\nexec /init\n"
}

// NetworkScript is the shell text that loads the network drivers, gets an address with busybox's udhcpc and attaches the ISO as /dev/nbd0: for start-up disks that
// bring none of this themselves. Say and Fail are the commands that print a message and an error (a start-up system may have its own: mkinitcpio's msg and err).
type NetworkScript struct {
	Helper    string // the helper program in the initramfs
	Dir       string // the folder in the initramfs with the drivers (modules/), the DHCP script (udhcpc.sh)
	Host      string
	Port      int
	Name      string
	Block     int
	Say, Fail string
}

// String is the script's text, to be put where it should run.
func (n NetworkScript) String() string {
	block := n.Block
	if block == 0 {
		block = 2048
	}
	return fmt.Sprintf(`    %[1]s ":: Gola Bootlace: starting the network"
    %[2]s insmod %[3]s/modules/* >/dev/null 2>&1
    modprobe -q nbd
    netok=
    for try in 1 2 3 4 5 6 7 8 9 10 11 12; do
        for n in /sys/class/net/*; do
            dev="${n##*/}"
            [ "$dev" = lo ] && continue
            [ -d "$n" ] || continue
            ip link set "$dev" up
            if udhcpc -i "$dev" -n -q -t 3 -T 2 -s %[3]s/udhcpc.sh >/dev/null 2>&1; then
                netok=$dev
                break
            fi
        done
        [ -n "$netok" ] && break
        sleep 2
    done
    if [ -n "$netok" ]; then
        %[1]s ":: Gola Bootlace: reading the ISO over the network ($netok)"
        %[2]s -host %[4]s -port %[5]d -name %[6]s -dev /dev/nbd0 -block %[7]d &
        for try in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
            [ "$(cat /sys/block/nbd0/size 2>/dev/null)" != 0 ] && break
            sleep 1
        done
    else
        %[8]s "Gola Bootlace: no network card answered, the ISO cannot be read over the network"
    fi
`, n.Say, n.Helper, n.Dir, ShellWord(n.Host), n.Port, ShellWord(n.Name), block, n.Fail)
}
