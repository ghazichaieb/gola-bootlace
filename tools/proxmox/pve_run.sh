#!/bin/bash
# Boots several ISOs at the same time, each in its own throw-away VM on the Proxmox test server (real LAN, the Bootlace container hands out the boot), and takes pictures of their screens.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
#   tools/proxmox/pve_run.sh OUTDIR 'ubuntu-22:3:240' 'Fedora:4:300' ...      (BIOS VMs)
#
# A test is <part of the ISO's name>:<memory in GB>:<seconds to wait>. The container's admin password and the Proxmox host are the test server's (see the memory notes);
# the VMs are 9201 upwards and are destroyed at the end, with their rules. Needs: ssh to the host, curl, python with Pillow (to turn the screen dump into a PNG).
set -u
HOST=${PVE_HOST:-root@192.168.1.71}
BASE=${BOOTLACE_URL:-http://192.168.1.201:8080}
PW=${BOOTLACE_PW:-change-me}
OUT=$1; shift
mkdir -p "$OUT"
JAR=$(mktemp)
api() { curl -s -b "$JAR" -H 'X-Bootlace: 1' -H 'Content-Type: application/json' "$@"; }
curl -s -c "$JAR" -H 'X-Bootlace: 1' -H 'Content-Type: application/json' -d "{\"password\":\"$PW\"}" "$BASE/api/login" > /dev/null
ISOS=$(api "$BASE/api/isos")
vms=(); macs=(); names=(); waits=(); starts=()
n=0
for t in "$@"; do
  IFS=: read -r pat mem wait <<< "$t"
  id=$(echo "$ISOS" | python -c "
import sys, json
pat = sys.argv[1].lower()
m = [i for i in json.load(sys.stdin)['isos'] if pat in i['name'].lower()]
exact = [i for i in m if i['name'].lower() == pat or i['name'].lower() == pat + '.iso']
m = exact or m
print(m[0]['id'] if len(m) == 1 else 'AMBIGUOUS:%d' % len(m))" "$pat")
  case "$id" in AMBIGUOUS*) echo "'$pat' matches: $id"; continue;; esac
  vmid=$((9201 + n)); n=$((n + 1))
  mac=$(printf 'BC:24:11:%02X:%02X:%02X' $((RANDOM % 256)) $((RANDOM % 256)) $((RANDOM % 256)))
  ssh "$HOST" "qm stop $vmid >/dev/null 2>&1; qm destroy $vmid --purge >/dev/null 2>&1; qm create $vmid --name bl-t$vmid --memory $((mem * 1024)) --cores 2 --cpu host --net0 e1000e=$mac,bridge=vmbr0 --bios seabios --boot order=net0 --vga std --ostype l26 --scsihw virtio-scsi-pci >/dev/null" || { echo "could not create VM for $pat"; continue; }
  api -d "{\"mac\":\"$(echo $mac | tr A-Z a-z)\",\"action\":\"iso:$id\",\"note\":\"pve_run\"}" "$BASE/api/rules" > /dev/null
  vms+=($vmid); macs+=($(echo $mac | tr A-Z a-z)); names+=("$pat"); waits+=($wait); starts+=(0)
  echo "$pat: VM $vmid $mac -> iso:$id"
done
# start them one after the other (the container reads the ISOs from the NAS), then take pictures until every test has had its time
t0=$(date +%s)
for i in "${!vms[@]}"; do ssh "$HOST" "qm start ${vms[$i]}" > /dev/null; starts[$i]=$(date +%s); sleep 15; done
pending=${#vms[@]}
done_flags=()
shot() { # vmid file
  ssh "$HOST" "echo 'screendump /tmp/s$1.ppm' | qm monitor $1 >/dev/null 2>&1; cat /tmp/s$1.ppm" > "$2.ppm" 2>/dev/null
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
      ssh "$HOST" "qm stop ${vms[$i]} >/dev/null 2>&1; qm destroy ${vms[$i]} --purge >/dev/null 2>&1"
      api -X DELETE "$BASE/api/rules/${macs[$i]}" > /dev/null
      done_flags[$i]=1; pending=$((pending - 1))
      echo "${names[$i]}: final picture $OUT/$safe-final.png after ${el}s"
    fi
  done
done
ssh "$HOST" "pct exec 112 -- journalctl -u bootlace --since '-$(( $(date +%s) - t0 + 120 ))sec' --no-pager" > "$OUT/server.log" 2>/dev/null
rm -f "$JAR"
echo "done: $OUT"
