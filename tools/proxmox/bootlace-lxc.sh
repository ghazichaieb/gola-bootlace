#!/usr/bin/env bash
# Gola Bootlace: create a Proxmox LXC container that runs the Bootlace network boot server (or update the one made earlier).
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
# Run it as root on the Proxmox host:
#
#     bash -c "$(curl -fsSL <link to this script>)"
#
# It asks nothing: every choice has a default that can be changed with an environment variable in front of the command, for example
#
#     CT_IP=192.168.1.50/24 CT_GW=192.168.1.1 ISO_PATH=/mnt/pve/nas/template/iso bash -c "$(curl -fsSL <link>)"
#
#   CTID            number of the container            (default: the next free one; give an existing Bootlace container's number to UPDATE it)
#   CT_HOSTNAME     name of the container              (default: bootlace)
#   CT_CORES        CPU cores                          (default: 1)
#   CT_MEMORY       memory in MB                       (default: 512)
#   CT_DISK         disk of the container in GB        (default: 4; the ISOs are not on it, see ISO_PATH)
#   BRIDGE          the network bridge                 (default: vmbr0; it must be the one the PCs to install are on)
#   CT_IP           address as a.b.c.d/nn or "dhcp"    (default: dhcp; a fixed address is better: PCs are told where the server is)
#   CT_GW           gateway, only with a fixed address
#   ROOTFS_STORAGE  storage for the container's disk   (default: the first that can hold containers)
#   TEMPLATE_STORAGE storage for the Debian template   (default: the first that can hold templates)
#   ISO_PATH        folder of the Proxmox host with the ISOs, shown to the container read-only at /srv/isos
#                   (default: /var/lib/vz/template/iso, where the Proxmox web interface keeps uploaded ISOs)
#   BOOTLACE_ARGS   more options for "bootlace serve"  (default: none; see "bootlace serve -h")
#   BOOTLACE_BASE   where the program is downloaded from: <BOOTLACE_BASE>/bootlace-linux-amd64 and a .sha256 file next to it
#   BOOTLACE_BIN    a program file on this host to install instead of downloading one (for a private repository or a test build)
set -Eeuo pipefail

: "${BOOTLACE_BASE:=https://github.com/ghazichaieb/gola-bootlace/releases/latest/download}"
: "${BOOTLACE_BIN:=}"
: "${CT_HOSTNAME:=bootlace}"
: "${CT_CORES:=1}"
: "${CT_MEMORY:=512}"
: "${CT_DISK:=4}"
: "${BRIDGE:=vmbr0}"
: "${CT_IP:=dhcp}"
: "${CT_GW:=}"
: "${ISO_PATH:=/var/lib/vz/template/iso}"
: "${BOOTLACE_ARGS:=}"

say() { printf '\033[1m%s\033[0m\n' "$*"; }
die() { printf '\033[31mError: %s\033[0m\n' "$*" >&2; exit 1; }
trap 'die "stopped at line $LINENO"' ERR

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

[ "$(id -u)" = 0 ] || die "run this as root on the Proxmox host"
command -v pct >/dev/null && command -v pveam >/dev/null && command -v pvesm >/dev/null || die "this is not a Proxmox host (pct, pveam and pvesm are needed)"
[ "$(uname -m)" = x86_64 ] || die "only x86-64 hosts are supported for now"
command -v curl >/dev/null || die "curl is needed"

# ---- the program: download, check, keep
if [ -n "$BOOTLACE_BIN" ]; then
	[ -f "$BOOTLACE_BIN" ] || die "no file $BOOTLACE_BIN"
	cp "$BOOTLACE_BIN" "$tmp/bootlace"
else
	say "Downloading Gola Bootlace ..."
	curl -fsSL "$BOOTLACE_BASE/bootlace-linux-amd64" -o "$tmp/bootlace" || die "cannot download $BOOTLACE_BASE/bootlace-linux-amd64"
	if curl -fsSL "$BOOTLACE_BASE/bootlace-linux-amd64.sha256" -o "$tmp/bootlace.sha256" 2>/dev/null; then
		want=$(awk '{print $1}' "$tmp/bootlace.sha256")
		have=$(sha256sum "$tmp/bootlace" | awk '{print $1}')
		[ "$want" = "$have" ] || die "the download is damaged (checksum $have, expected $want)"
	else
		say "(no checksum file next to the program: not checked)"
	fi
fi
chmod 755 "$tmp/bootlace"
"$tmp/bootlace" --version | head -n 1

write_unit() {
	cat >"$tmp/bootlace.service" <<'UNIT'
[Unit]
Description=Gola Bootlace network boot server
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/default/bootlace
ExecStart=/usr/local/bin/bootlace serve -iso-dir /srv/isos -dhcp proxy $BOOTLACE_ARGS
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
	printf 'BOOTLACE_ARGS="%s"\n' "$BOOTLACE_ARGS" >"$tmp/bootlace.default"
}

# ---- update an existing container
if [ -n "${CTID:-}" ] && pct status "$CTID" >/dev/null 2>&1; then
	pct config "$CTID" | grep -q '^description:.*Gola Bootlace' || die "container $CTID exists but was not made by this script"
	say "Updating the Bootlace container $CTID ..."
	pct status "$CTID" | grep -q running || pct start "$CTID"
	pct exec "$CTID" -- systemctl stop bootlace || true
	pct push "$CTID" "$tmp/bootlace" /usr/local/bin/bootlace --perms 755
	pct exec "$CTID" -- systemctl start bootlace
	say "Done. Bootlace in container $CTID is updated and running."
	exit 0
fi

# ---- a new container
: "${CTID:=$(pvesh get /cluster/nextid)}"
pct status "$CTID" >/dev/null 2>&1 && die "container or VM $CTID already exists"
ip link show "$BRIDGE" >/dev/null 2>&1 || die "no network bridge named $BRIDGE (set BRIDGE=...)"

if [ -z "${ROOTFS_STORAGE:-}" ]; then
	ROOTFS_STORAGE=$(pvesm status --content rootdir 2>/dev/null | awk 'NR>1 && $3=="active" {print $1; exit}')
fi
[ -n "$ROOTFS_STORAGE" ] || die "no storage can hold containers (set ROOTFS_STORAGE=...)"
if [ -z "${TEMPLATE_STORAGE:-}" ]; then
	TEMPLATE_STORAGE=$(pvesm status --content vztmpl 2>/dev/null | awk 'NR>1 && $3=="active" {print $1; exit}')
fi
[ -n "$TEMPLATE_STORAGE" ] || die "no storage can hold templates (set TEMPLATE_STORAGE=...)"

say "Getting a Debian template ..."
pveam update >/dev/null 2>&1 || true
template=$(pveam available --section system | awk '$2 ~ /^debian-12-standard_.*_amd64\.tar\.(zst|gz|xz)$/ {print $2}' | sort -V | tail -n 1)
[ -n "$template" ] || die "no Debian 12 template is on offer (is this host online?)"
pveam list "$TEMPLATE_STORAGE" | grep -q "$template" || pveam download "$TEMPLATE_STORAGE" "$template"

net="name=eth0,bridge=$BRIDGE,firewall=0,ip=$CT_IP"
[ "$CT_IP" = dhcp ] || { [ -n "$CT_GW" ] && net="$net,gw=$CT_GW"; }

args=(--hostname "$CT_HOSTNAME" --cores "$CT_CORES" --memory "$CT_MEMORY" --swap 0 --rootfs "$ROOTFS_STORAGE:$CT_DISK"
	--net0 "$net" --unprivileged 1 --onboot 1 --ostype debian --description "Gola Bootlace network boot server (made by bootlace-lxc.sh)")
if [ -d "$ISO_PATH" ]; then
	args+=(--mp0 "$ISO_PATH,mp=/srv/isos,ro=1")
else
	say "(no folder $ISO_PATH: the container gets an empty /srv/isos; put ISOs there, or set ISO_PATH=...)"
fi

say "Creating container $CTID ($CT_HOSTNAME) ..."
pct create "$CTID" "$TEMPLATE_STORAGE:vztmpl/$template" "${args[@]}"
pct start "$CTID"

say "Waiting for the network ..."
addr=""
for _ in $(seq 1 30); do
	addr=$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}') || true
	[ -n "$addr" ] && break
	sleep 2
done
[ -n "$addr" ] || die "the container got no address (is the bridge $BRIDGE connected to the network?)"
pct exec "$CTID" -- mkdir -p /srv/isos

say "Installing Bootlace in the container ..."
write_unit
pct push "$CTID" "$tmp/bootlace" /usr/local/bin/bootlace --perms 755
pct push "$CTID" "$tmp/bootlace.service" /etc/systemd/system/bootlace.service
pct push "$CTID" "$tmp/bootlace.default" /etc/default/bootlace
pct exec "$CTID" -- systemctl daemon-reload
pct exec "$CTID" -- systemctl enable --now bootlace
sleep 2
pct exec "$CTID" -- systemctl is-active bootlace >/dev/null || { pct exec "$CTID" -- journalctl -u bootlace -n 20 --no-pager; die "Bootlace did not start"; }

cat <<DONE

Gola Bootlace is running in container $CTID at $addr.

  ISOs        put them in $ISO_PATH on this host (shown to the container read-only), then they appear in the menu at once
  menu        http://$addr:8080/boot.ipxe
  log         pct exec $CTID -- journalctl -u bootlace -f
  update      CTID=$CTID bash -c "\$(curl -fsSL <link to this script>)"
  remove      pct stop $CTID && pct destroy $CTID

On the PC to install: switch Secure Boot off (not supported yet), start it from the network (PXE, usually F8 or F12 for the boot menu),
choose an ISO in the menu. Bootlace answers PCs that ask to network-boot and leaves your DHCP server alone.
DONE
[ "$CT_IP" != dhcp ] || echo "Note: the container's address came from DHCP. Give it a fixed address (CT_IP=...) or a reservation in your router."
