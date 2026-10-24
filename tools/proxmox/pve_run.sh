#!/bin/bash
# Boots several ISOs at the same time, each in its own throw-away VM on the Proxmox test server (real LAN, the Bootlace container hands out the boot), and takes pictures of their screens.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
#   tools/proxmox/pve_run.sh OUTDIR 'ubuntu-22:3:240' 'Fedora:4:300:uefi' ...      (BIOS VMs, or UEFI with :uefi)
#
# A test is <part of the ISO's name>:<memory in GB>:<seconds to wait>[:uefi]. A UEFI VM has no network boot of its own on Proxmox (its OVMF has no PXE), so it starts iPXE
# from a small EFI disk (/root/espdisk.img on the host: a 64 MB GPT disk with EFI/BOOT/BOOTX64.EFI = Bootlace's ipxe.efi) and the rest is the usual way. The Proxmox host, the Bootlace address and its web interface password come from PVE_HOST, BOOTLACE_URL and BOOTLACE_PW;
# the VMs are 9201 upwards (PVE_VMID_BASE=9301 for a second run at the same time: two runs must never share VM numbers) and are destroyed at the end, with their rules. Needs: ssh to the host, curl, python with Pillow (to turn the screen dump into a PNG).
set -u
HOST=${PVE_HOST:?set PVE_HOST to the Proxmox host to test on, like root@192.168.1.10}
BASE=${BOOTLACE_URL:?set BOOTLACE_URL to the Bootlace under test, like http://192.168.1.50:8080}
PW=${BOOTLACE_PW:?set BOOTLACE_PW to the password of its web interface}
OUT=$1; shift
mkdir -p "$OUT"
JAR=$(mktemp)
api() { curl -s -b "$JAR" -H 'X-Bootlace: 1' -H 'Content-Type: application/json' "$@"; }
curl -s -c "$JAR" -H 'X-Bootlace: 1' -H 'Content-Type: application/json' -d "{\"password\":\"$PW\"}" "$BASE/api/login" > /dev/null
ISOS=$(api "$BASE/api/isos")
vms=(); macs=(); names=(); waits=(); starts=(); modes=()
n=0
for t in "$@"; do
  IFS=: read -r pat mem wait mode <<< "$t"
  id=$(echo "$ISOS" | python -c "
import sys, json
pat = sys.argv[1].lower()
m = [i for i in json.load(sys.stdin)['isos'] if pat in i['name'].lower()]
exact = [i for i in m if i['name'].lower() == pat or i['name'].lower() == pat + '.iso']
m = exact or m
print(m[0]['id'] if len(m) == 1 else 'AMBIGUOUS:%d' % len(m))" "$pat")
  case "$id" in AMBIGUOUS*) echo "'$pat' matches: $id"; continue;; esac
  vmid=$((${PVE_VMID_BASE:-9201} + n)); n=$((n + 1))
  vmname="bl-$(echo "$pat" | tr A-Z a-z | tr -c 'a-z0-9' '-' | sed 's/--*/-/g; s/^-//; s/-$//')-${mode:-bios}"
  # the same test always gets the same MAC: a router hands out a lease to every new address and its pool runs dry after many runs
  mac=$(echo -n "$vmname" | md5sum | sed 's/^\(..\)\(..\)\(..\).*/BC:24:11:\1:\2:\3/' | tr a-f A-F)
  if [ "$mode" = uefi ]; then
    ssh "$HOST" "bash -s $vmid $mem $mac $vmname ${PVE_NIC:-virtio}" < "$(dirname "$0")/pve_uefi_vm.sh" || { echo "could not create the UEFI VM for $pat"; continue; }
  else
    ssh "$HOST" "qm stop $vmid >/dev/null 2>&1; qm destroy $vmid --purge >/dev/null 2>&1; qm create $vmid --name $vmname --memory $((mem * 1024)) --cores 2 --cpu host --net0 ${PVE_NIC:-e1000e}=$mac,bridge=vmbr0 --bios seabios --boot order=net0 --vga std --ostype l26 --scsihw virtio-scsi-pci --serial0 socket >/dev/null" || { echo "could not create VM for $pat"; continue; }
  fi
  api -d "{\"mac\":\"$(echo $mac | tr A-Z a-z)\",\"action\":\"iso:$id\",\"note\":\"pve_run\"}" "$BASE/api/rules" > /dev/null
  vms+=($vmid); macs+=($(echo $mac | tr A-Z a-z)); names+=("$pat"); waits+=($wait); starts+=(0); modes+=("${mode:-bios}")
  echo "$pat: VM $vmid $mac -> iso:$id"
done
# start them one after the other (the container reads the ISOs from the NAS), then take pictures until every test has had its time
t0=$(date +%s)
for i in "${!vms[@]}"; do
  ssh "$HOST" "qm start ${vms[$i]}" > /dev/null; starts[$i]=$(date +%s)
  # a new UEFI VM stops at its firmware's boot manager until a key is pressed (the key is lost if it comes before the menu is up): press Enter twice, late
  [ "${modes[$i]:-}" = uefi ] && ( sleep 80; ssh "$HOST" "qm sendkey ${vms[$i]} ret" > /dev/null; sleep 40; ssh "$HOST" "qm sendkey ${vms[$i]} ret" > /dev/null ) &
  # PVE_SERIAL=1 also keeps what the VM writes to its serial port (a test can add "console=ttyS0,115200n8" as extra kernel arguments in the web interface)
  [ -n "${PVE_SERIAL:-}" ] && ssh "$HOST" "rm -f /tmp/serial${vms[$i]}.log; (timeout $(( ${waits[$i]} + 60 )) socat -u UNIX-CONNECT:/var/run/qemu-server/${vms[$i]}.serial0 - > /tmp/serial${vms[$i]}.log 2>&1 &)" > /dev/null
  sleep 15
done
pending=${#vms[@]}
done_flags=()
shot() { # vmid file
  # (the old dump is removed first: a VM that has stopped must not show the picture of an earlier VM with the same number)
  ssh "$HOST" "rm -f /tmp/s$1.ppm; qm status $1 | grep -q running || { echo 'VM is not running' >&2; exit 1; }; echo 'screendump /tmp/s$1.ppm' | qm monitor $1 >/dev/null 2>&1; cat /tmp/s$1.ppm" > "$2.ppm" 2>/dev/null || { rm -f "$2.ppm" "$2.png"; echo "$1: the VM is not running (stopped or crashed)"; return; }
  python - "$2" <<'PY'
import sys
from PIL import Image
n = sys.argv[1]
try:
    Image.open(n + '.ppm').save(n + '.png')
except Exception as e:
    print('no picture:', e)
PY
  rm -f "$2.ppm"
}
while [ $pending -gt 0 ]; do
  sleep 20
  for i in "${!vms[@]}"; do
    [ "${done_flags[$i]:-}" = 1 ] && continue
    el=$(( $(date +%s) - ${starts[$i]} ))
    safe=$(echo "${names[$i]}" | tr -c 'A-Za-z0-9\n' '_')
    shot "${vms[$i]}" "$OUT/$safe"
    if [ $el -ge ${waits[$i]} ]; then
      cp "$OUT/$safe.png" "$OUT/$safe-final.png" 2>/dev/null
      [ -n "${PVE_SERIAL:-}" ] && ssh "$HOST" "cat /tmp/serial${vms[$i]}.log" > "$OUT/$safe.serial.log" 2>/dev/null
      if [ -z "${PVE_KEEP:-}" ]; then # (PVE_KEEP=1 leaves the VMs and their rules for looking at by hand: remove them afterwards)
        ssh "$HOST" "qm stop ${vms[$i]} >/dev/null 2>&1; qm destroy ${vms[$i]} --purge >/dev/null 2>&1"
        api -X DELETE "$BASE/api/rules/${macs[$i]}" > /dev/null
      fi
      done_flags[$i]=1; pending=$((pending - 1))
      echo "${names[$i]}: final picture $OUT/$safe-final.png after ${el}s"
    fi
  done
done
ssh "$HOST" "pct exec 112 -- journalctl -u bootlace --since '-$(( $(date +%s) - t0 + 120 ))sec' --no-pager" > "$OUT/server.log" 2>/dev/null
rm -f "$JAR"
echo "done: $OUT"
