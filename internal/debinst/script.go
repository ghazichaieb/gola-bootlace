// The start-up script and the DHCP handler that go into the archive.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package debinst

import (
	"fmt"

	"github.com/ghazichaieb/gola-bootlace/internal/initrd"
)

// udhcpcScript is what the start-up disk's udhcpc (busybox) runs when it has an address: this start-up disk has no ifconfig, so the address and the route are set with ip.
const udhcpcScript = `#!/bin/sh
# Gola Bootlace: what udhcpc runs when it has an address
case "$1" in
bound|renew)
    ip addr flush dev "$interface" 2>/dev/null
    ip addr add "$ip/${mask:-24}" dev "$interface"
    ip link set "$interface" up
    for r in $router; do
        ip route add default via "$r" dev "$interface" 2>/dev/null
    done
    ;;
esac
exit 0
`

// Script is the text of the start-up script, run by the installer's start-up before anything else of the installer: it brings up a network card, logs in to the iSCSI
// CD-ROM target and waits for the disc, so that the installer's own detection of its CD finds it.
func Script(t Target) string {
	return fmt.Sprintf(scriptText, initrd.ShellWord(t.Host), t.Port, initrd.ShellWord(t.IQN), initiatorName)
}

const scriptText = `#!/bin/sh
# Gola Bootlace: lets the installer of a netinst ISO find its CD over the network. The ISO is offered as an iSCSI CD-ROM by the Bootlace server; this logs in to it, and
# the installer's own detection then finds /dev/sr0 like any disc. Network card drivers and iscsistart come from the ISO's own packages (added to this start-up disk).
HOST=%[1]s
PORT=%[2]d
IQN=%[3]s
INITIATOR=%[4]s

say() {
    logger -t bootlace "$*"
    echo "Gola Bootlace: $*"
}

# 1. the network card drivers (the packages were unpacked into the modules folder: make their index, then load what the devices present ask for)
KVER=$(uname -r)
depmod -a "$KVER" >/dev/null 2>&1
mods=""
for f in $(find /sys/devices -name modalias 2>/dev/null); do
    mods="$mods $(cat "$f" 2>/dev/null)"
done
[ -n "$mods" ] && modprobe -a -q $mods >/dev/null 2>&1

# 2. an address: the first card that gets one from the DHCP server
dev=""
try=0
while [ "$try" -lt 12 ] && [ -z "$dev" ]; do
    for n in /sys/class/net/*; do
        d="${n##*/}"
        [ "$d" = lo ] && continue
        [ -d "$n" ] || continue
        ip link set "$d" up >/dev/null 2>&1
        if udhcpc -i "$d" -n -q -t 3 -T 2 -s /etc/bootlace-udhcpc.sh >/dev/null 2>&1; then
            dev="$d"
            break
        fi
    done
    [ -n "$dev" ] && break
    try=$((try + 1))
    sleep 2
done
if [ -z "$dev" ]; then
    say "no network card got an address: the installation CD cannot be reached"
    exit 0
fi
say "network card $dev is up, logging in to the CD-ROM $IQN on $HOST:$PORT"

# 3. the iSCSI session (the kernel's iSCSI modules and the disc drivers)
for m in scsi_transport_iscsi libiscsi libiscsi_tcp iscsi_tcp sr_mod isofs; do
    modprobe -q "$m" >/dev/null 2>&1
done
iscsistart -i "$INITIATOR" -t "$IQN" -g 1 -a "$HOST" -p "$PORT" >/tmp/bootlace-iscsi.log 2>&1 &

# 4. the disc: the installer's detection looks for /dev/sr*
try=0
while [ "$try" -lt 45 ]; do
    if [ -b /dev/sr0 ]; then
        say "the installation CD is /dev/sr0"
        exit 0
    fi
    if [ -r /sys/block/sr0/dev ]; then
        mm=$(cat /sys/block/sr0/dev)
        mknod /dev/sr0 b "${mm%%%%:*}" "${mm##*:}" 2>/dev/null
    fi
    try=$((try + 1))
    sleep 1
done
say "the CD-ROM did not appear (see /tmp/bootlace-iscsi.log)"
exit 0
`
