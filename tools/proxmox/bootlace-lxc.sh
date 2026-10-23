#!/usr/bin/env bash
# Gola Bootlace: make a Proxmox LXC container that runs the Bootlace network boot server, or update the one made earlier.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
# Run it as root on the Proxmox host, in a terminal:
#
#     bash -c "$(curl -fsSL https://raw.githubusercontent.com/ghazichaieb/gola-bootlace/main/tools/proxmox/bootlace-lxc.sh)"
#
# It asks a few questions (container, network, and where the ISOs are kept) and every question has a default: press Enter to take it. Nothing is asked when it is
# not run in a terminal, or with BOOTLACE_YES=1: the defaults, and the values given as environment variables, are used. Every answer can be given as a variable:
#
#   CTID              number of the container          (default: the next free one; give a Bootlace container's number to UPDATE it)
#   CT_HOSTNAME       name of the container            (bootlace)
#   CT_CORES          cores                            (1)
#   CT_MEMORY         memory in MB                     (512)
#   CT_DISK           disk in GB                       (8: the system, the program and a small cache; the ISOs are elsewhere unless LIB_MODE=container)
#   BRIDGE            network bridge, the one the PCs to install are on  (vmbr0)
#   CT_IP             a.b.c.d/nn or dhcp               (dhcp; a fixed address or a reservation is better: PCs are told where the server is)
#   CT_GW             gateway, with a fixed address
#   LIB_MODE          where the ISOs (the library) are:  container | host | share   (container)
#     host:  LIB_HOST_PATH   a folder on this host (ISO_PATH is the same, from earlier versions)
#     share: SHARE_KIND smb|nfs, SHARE_SERVER, SHARE_PATH (smb: the share's name; nfs: the export's path), SHARE_USER and SHARE_DOMAIN (smb, when it needs a login)
#   ROOTFS_STORAGE, TEMPLATE_STORAGE   storages for the container's disk and the template (the first that fit)
#   BOOTLACE_VERSION  a version such as 0.1.0          (latest)
#   BOOTLACE_REPO     the GitHub repository            (ghazichaieb/gola-bootlace)
#   BOOTLACE_FILE     a release archive (.tar.gz) or a program file on this host, installed instead of a download (for a test build)
#   BOOTLACE_ARGS     more options for "bootlace serve" (none; see "bootlace serve -h", and every option can also be a BOOTLACE_<NAME> variable)
set -Eeuo pipefail

: "${BOOTLACE_REPO:=ghazichaieb/gola-bootlace}"
: "${BOOTLACE_VERSION:=latest}"
: "${BOOTLACE_FILE:=}"
: "${BOOTLACE_ARGS:=}"
: "${LIB_HOST_PATH:=${ISO_PATH:-}}"

say() { printf '\033[1m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*"; }
die() { printf '\033[31mError: %s\033[0m\n' "$*" >&2; exit 1; }
trap 'die "stopped at line $LINENO"' ERR

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

INTERACTIVE=0
[ -z "${BOOTLACE_YES:-}" ] && [ -t 0 ] && INTERACTIVE=1

# ask VAR "Question" default: takes the value from the environment, else asks (in a terminal), else uses the default
ask() {
	local var=$1 question=$2 def=$3 answer=""
	[ -n "${!var:-}" ] && return 0
	if [ "$INTERACTIVE" = 1 ]; then
		read -r -p "$question [$def]: " answer || answer=""
	fi
	printf -v "$var" '%s' "${answer:-$def}"
}

# confirm "Question" default(y|n): true for yes
confirm() {
	local answer=""
	if [ "$INTERACTIVE" = 1 ]; then
		read -r -p "$1 [$2]: " answer || answer=""
	fi
	answer=${answer:-$2}
	[[ $answer =~ ^[Yy] ]]
}

[ "$(id -u)" = 0 ] || die "run this as root on the Proxmox host"
command -v pct >/dev/null && command -v pveam >/dev/null && command -v pvesm >/dev/null || die "this is not a Proxmox host (pct, pveam and pvesm are needed)"
command -v curl >/dev/null || die "curl is needed"
case $(dpkg --print-architecture) in
amd64) ARCH=amd64 ;;
arm64) ARCH=arm64 ;;
*) die "only amd64 and arm64 hosts are supported" ;;
esac

# ---- the program: download, check, unpack
fetch_program() {
	if [ -n "$BOOTLACE_FILE" ]; then
		[ -f "$BOOTLACE_FILE" ] || die "no file $BOOTLACE_FILE"
		case $BOOTLACE_FILE in
		*.tar.gz | *.tgz) tar -xzf "$BOOTLACE_FILE" -C "$tmp" --wildcards '*/bootlace' --strip-components=1 || die "no program in $BOOTLACE_FILE" ;;
		*) cp "$BOOTLACE_FILE" "$tmp/bootlace" ;;
		esac
	else
		local base file want have
		if [ "$BOOTLACE_VERSION" = latest ]; then
			base="https://github.com/$BOOTLACE_REPO/releases/latest/download"
		else
			base="https://github.com/$BOOTLACE_REPO/releases/download/v${BOOTLACE_VERSION#v}"
		fi
		say "Downloading Gola Bootlace ($BOOTLACE_VERSION) ..."
		curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "cannot download $base/SHA256SUMS (is there a release of that version?)"
		read -r want file < <(awk -v suffix="-linux-$ARCH.tar.gz" 'index($2, suffix) { sub(/^\*/, "", $2); print $1, $2; exit }' "$tmp/SHA256SUMS") || true
		[ -n "${file:-}" ] || die "that release has no program for linux-$ARCH"
		curl -fsSL "$base/$file" -o "$tmp/$file" || die "cannot download $base/$file"
		have=$(sha256sum "$tmp/$file" | awk '{print $1}')
		[ "$want" = "$have" ] || die "the download is damaged (checksum $have, expected $want)"
		tar -xzf "$tmp/$file" -C "$tmp" --wildcards '*/bootlace' --strip-components=1
	fi
	chmod 755 "$tmp/bootlace"
	"$tmp/bootlace" --version | head -n 1
}
# The "update" command of the container: it updates the program in the container itself, with nothing to do on the host. The text is written to $tmp/update.
write_update() {
	cat >"$tmp/update" <<'UPDATE'
#!/bin/sh
# Gola Bootlace: update the program in this container.
#   update            to the latest release          update --check   only say whether there is a newer one
#   update -v 0.2.0   to that version (also older)   update -y         do not ask before restarting the server
# BOOTLACE_BASE=<web folder with SHA256SUMS and the archives> takes the files from there instead of GitHub (a mirror, a test).
set -eu
. /etc/bootlace-update.conf
CHECK=0 ASSUME=0 WANT=latest
while [ $# -gt 0 ]; do
	case $1 in
	--check) CHECK=1 ;;
	-y | --yes) ASSUME=1 ;;
	-v | --version) WANT=${2:?the version is missing}; shift ;;
	-h | --help) sed -n '2,6p' "$0"; exit 0 ;;
	*) echo "update: unknown option $1" >&2; exit 2 ;;
	esac
	shift
done
[ "$(id -u)" = 0 ] || { echo "update: run it as root" >&2; exit 1; }
ARCH=$(dpkg --print-architecture)
if [ -n "${BOOTLACE_BASE:-}" ]; then
	BASE=$BOOTLACE_BASE
elif [ "$WANT" = latest ]; then
	BASE=https://github.com/$REPO/releases/latest/download
else
	BASE=https://github.com/$REPO/releases/download/v${WANT#v}
fi
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS" || { echo "update: cannot fetch $BASE/SHA256SUMS (no such release, or no network)" >&2; exit 1; }
set -- $(awk -v s="-linux-$ARCH.tar.gz" 'index($2, s) { sub(/^\*/, "", $2); print $1, $2; exit }' "$TMP/SHA256SUMS")
SUM=${1:-} FILE=${2:-}
[ -n "$FILE" ] || { echo "update: that release has no program for linux-$ARCH" >&2; exit 1; }
NEW=${FILE#bootlace-}
NEW=${NEW%-linux-$ARCH.tar.gz}
CUR=$(/usr/local/bin/bootlace --version 2>/dev/null | head -n 1 | awk '{print $3}')
echo "installed: ${CUR:-unknown}   available: $NEW"
if [ "$CUR" = "$NEW" ]; then echo "Already up to date."; exit 0; fi
[ "$CHECK" = 1 ] && { echo "A newer version can be installed with: update    (notes: https://github.com/$REPO/releases/tag/v$NEW)"; exit 0; }
if [ "$ASSUME" = 0 ] && [ -t 0 ]; then
	echo "The server restarts: PCs that are installing from it are interrupted."
	printf 'Update to %s now? [y/N] ' "$NEW"
	read -r ANSWER
	case $ANSWER in [Yy]*) ;; *) echo "Not updated."; exit 0 ;; esac
fi
curl -fsSL "$BASE/$FILE" -o "$TMP/$FILE" || { echo "update: cannot download $FILE" >&2; exit 1; }
[ "$(sha256sum "$TMP/$FILE" | awk '{print $1}')" = "$SUM" ] || { echo "update: the download is damaged (the checksum is wrong); nothing was changed" >&2; exit 1; }
tar -xzf "$TMP/$FILE" -C "$TMP" --wildcards '*/bootlace' --strip-components=1
chmod 755 "$TMP/bootlace"
"$TMP/bootlace" --version >/dev/null 2>&1 || { echo "update: the new program does not run here; nothing was changed" >&2; exit 1; }
cp /usr/local/bin/bootlace /usr/local/bin/bootlace.previous
systemctl stop bootlace
mv "$TMP/bootlace" /usr/local/bin/bootlace
systemctl start bootlace
for _ in 1 2 3 4 5 6 7 8 9 10; do
	sleep 2
	if /usr/local/bin/bootlace health >/dev/null 2>&1; then
		echo "Updated to $NEW. The old program is kept as /usr/local/bin/bootlace.previous."
		echo "Notes: https://github.com/$REPO/releases/tag/v$NEW"
		exit 0
	fi
done
echo "update: the new version does not answer: going back to $CUR" >&2
systemctl stop bootlace
mv /usr/local/bin/bootlace.previous /usr/local/bin/bootlace
systemctl start bootlace
exit 1
UPDATE
	printf 'REPO=%s\n' "$BOOTLACE_REPO" >"$tmp/bootlace-update.conf"
}


write_unit() {
	cat >"$tmp/bootlace.service" <<'UNIT'
[Unit]
Description=Gola Bootlace network boot server
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/default/bootlace
StateDirectory=bootlace
ExecStart=/usr/local/bin/bootlace serve $BOOTLACE_ARGS
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
	cat >"$tmp/bootlace.default" <<ENVFILE
# Every option of "bootlace serve" can be set here as BOOTLACE_<NAME>.
BOOTLACE_CONFIG=/var/lib/bootlace/config.json
BOOTLACE_ISO_DIR=/srv/library
BOOTLACE_DHCP=proxy
BOOTLACE_ARGS="$BOOTLACE_ARGS"
ENVFILE
}

# ---- update an existing container
if [ -n "${CTID:-}" ] && pct status "$CTID" >/dev/null 2>&1; then
	pct config "$CTID" | grep -qE '^description:.*Gola(%20| )Bootlace' || die "container $CTID exists but was not made by this script"
	fetch_program
	say "Updating the Bootlace container $CTID ..."
	pct status "$CTID" | grep -q running || pct start "$CTID"
	pct exec "$CTID" -- systemctl stop bootlace || true
	# wimlib-imagex lets Bootlace start Windows with UEFI Secure Boot on (it puts its files into a copy of boot.wim)
	pct exec "$CTID" -- env LC_ALL=C DEBIAN_FRONTEND=noninteractive sh -c '(command -v wimlib-imagex >/dev/null && command -v curl >/dev/null) || (apt-get update -qq && apt-get install -y -qq wimtools curl ca-certificates >/dev/null 2>&1)' || true
	pct push "$CTID" "$tmp/bootlace" /usr/local/bin/bootlace --perms 755
	write_update # (also for a container made before the "update" command existed)
	pct push "$CTID" "$tmp/update" /usr/local/bin/update --perms 755
	pct push "$CTID" "$tmp/bootlace-update.conf" /etc/bootlace-update.conf
	if ! pct exec "$CTID" -- grep -q '^BOOTLACE_ISO_DIR=' /etc/default/bootlace 2>/dev/null; then
		# a container made by an older version of this script: its folder was /srv/isos and its unit had the options in it
		write_unit
		pct push "$CTID" "$tmp/bootlace.service" /etc/systemd/system/bootlace.service
		pct push "$CTID" "$tmp/bootlace.default" /etc/default/bootlace
		pct exec "$CTID" -- sed -i 's#^BOOTLACE_ISO_DIR=.*#BOOTLACE_ISO_DIR=/srv/isos#' /etc/default/bootlace
		pct exec "$CTID" -- systemctl daemon-reload
	fi
	pct exec "$CTID" -- systemctl start bootlace
	say "Done. Bootlace in container $CTID is updated and running."
	exit 0
fi

# ---- a new container: the questions
say "Gola Bootlace: a network boot server in a Proxmox container"
[ "$INTERACTIVE" = 1 ] && echo "Press Enter to take the answer in brackets."
if [ -z "${CTID:-}" ]; then ask CTID "Container number" "$(pvesh get /cluster/nextid)"; fi # (ask does nothing for a variable that has a value: the default is its argument)
[[ $CTID =~ ^[0-9]+$ ]] || die "the container number must be a number"
pct status "$CTID" >/dev/null 2>&1 && die "container or VM $CTID already exists"
ask CT_HOSTNAME "Name of the container" bootlace
[[ $CT_HOSTNAME =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$ ]] || die "$CT_HOSTNAME is not a valid host name"
ask BRIDGE "Network bridge (the one the PCs to install are on)" vmbr0
ip link show "$BRIDGE" >/dev/null 2>&1 || die "no network bridge named $BRIDGE"
ask CT_IP "Address: dhcp, or a.b.c.d/nn (a fixed address is better)" dhcp
if [ "$CT_IP" != dhcp ]; then
	[[ $CT_IP =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+/[0-9]+$ ]] || die "the address must be dhcp or like 192.168.1.50/24"
	ask CT_GW "Gateway" ""
fi
ask CT_CORES "Cores" 1
ask CT_MEMORY "Memory in MB" 512
ask CT_DISK "Disk in GB (the system and a small cache; ISOs are elsewhere unless you keep them in the container)" 8
for n in CT_CORES CT_MEMORY CT_DISK; do [[ ${!n} =~ ^[0-9]+$ ]] || die "$n must be a number"; done

if [ "$INTERACTIVE" = 1 ] && [ -z "${LIB_MODE:-}" ]; then
	echo
	echo "Where are the ISO files kept (the library)? It must be readable and writable."
	echo "  1) inside the container (simplest: the ISOs use the container's disk)"
	echo "  2) a folder on this Proxmox host"
	echo "  3) a network share (SMB or NFS), mounted on this host"
	read -r -p "Choose [1]: " choice || choice=""
	case ${choice:-1} in 2) LIB_MODE=host ;; 3) LIB_MODE=share ;; *) LIB_MODE=container ;; esac
fi
: "${LIB_MODE:=container}"
MP_ARG=()
case $LIB_MODE in
container) ;;
host)
	ask LIB_HOST_PATH "Folder on this host" /var/lib/vz/template/iso
	[ -d "$LIB_HOST_PATH" ] || { confirm "$LIB_HOST_PATH does not exist. Make it?" y && mkdir -p "$LIB_HOST_PATH" || die "no folder $LIB_HOST_PATH"; }
	MP_ARG=(--mp0 "$LIB_HOST_PATH,mp=/srv/library,ro=0")
	;;
share)
	ask SHARE_KIND "Kind of share (smb or nfs)" smb
	[[ $SHARE_KIND =~ ^(smb|nfs)$ ]] || die "the kind of share must be smb or nfs"
	ask SHARE_SERVER "Server (name or address)" ""
	[ -n "$SHARE_SERVER" ] || die "the server is needed"
	if [ "$SHARE_KIND" = smb ]; then
		ask SHARE_PATH "Name of the share (a folder inside it can follow: share/folder)" ""
	else
		ask SHARE_PATH "Path of the export on the server (like /mnt/pool/isos)" ""
	fi
	[ -n "$SHARE_PATH" ] || die "the share is needed"
	if [ "$SHARE_KIND" = smb ] && [ -z "${SHARE_USER+x}" ]; then
		if confirm "Does the share need a user name and password?" n; then ask SHARE_USER "User name" ""; else SHARE_USER=""; fi
	fi
	: "${SHARE_USER:=}"
	[ "$SHARE_KIND" = smb ] && [ -n "$SHARE_USER" ] && ask SHARE_DOMAIN "Domain or workgroup (empty for none)" ""
	: "${SHARE_DOMAIN:=}"
	SHARE_MOUNT=/mnt/bootlace-library-$CTID
	LIB_HOST_PATH=$SHARE_MOUNT
	MP_ARG=(--mp0 "$SHARE_MOUNT,mp=/srv/library,ro=0")
	;;
*) die "LIB_MODE must be container, host or share" ;;
esac

# ---- the storages and the template
if [ -z "${ROOTFS_STORAGE:-}" ]; then
	ROOTFS_STORAGE=$(pvesm status --content rootdir 2>/dev/null | awk 'NR>1 && $3=="active" {print $1; exit}')
fi
[ -n "$ROOTFS_STORAGE" ] || die "no storage can hold containers (set ROOTFS_STORAGE=...)"
if [ -z "${TEMPLATE_STORAGE:-}" ]; then
	TEMPLATE_STORAGE=$(pvesm status --content vztmpl 2>/dev/null | awk 'NR>1 && $3=="active" {print $1; exit}')
fi
[ -n "$TEMPLATE_STORAGE" ] || die "no storage can hold templates (set TEMPLATE_STORAGE=...)"

fetch_program

say "Getting a Debian template ..."
pveam update >/dev/null 2>&1 || true
# (the template of this host's own architecture: the arm64 one sorts after the amd64 one of the same version)
template=$(pveam available --section system | awk '{print $2}' | grep -E "^debian-1[23]-standard_.*_${ARCH}\.tar\.(zst|gz|xz)$" | sort -V | tail -n 1 || true)
[ -n "$template" ] || die "no Debian template for $ARCH is on offer (is this host online?)"
pveam list "$TEMPLATE_STORAGE" | grep -q "$template" || pveam download "$TEMPLATE_STORAGE" "$template"

# ---- a network share on the host: the entry in /etc/fstab, a login file the person fills in, and the folder it is mounted on.
# The share is an "automount": the host starts without it and mounts it the first time anything uses it, so a NAS that boots later than this host is no problem. A
# share that is mounted, unmounted or mounted again while the container runs is followed by the container, and Bootlace waits for its library to be there.
if [ "$LIB_MODE" = share ]; then
	need=mount.nfs
	[ "$SHARE_KIND" = smb ] && need=mount.cifs
	if ! command -v "$need" >/dev/null 2>&1; then
		pkg=nfs-common
		[ "$SHARE_KIND" = smb ] && pkg=cifs-utils
		if [ "$INTERACTIVE" = 0 ] || confirm "$pkg is needed on this host to mount the share. Install it?" y; then
			apt-get update -qq && apt-get install -y -qq "$pkg" >/dev/null
		else
			die "install $pkg and run this again"
		fi
	fi
	mkdir -p "$SHARE_MOUNT"
	opts="_netdev,nofail,x-systemd.automount,x-systemd.mount-timeout=30"
	if [ "$SHARE_KIND" = smb ]; then
		if [ -n "$SHARE_USER" ]; then
			cred=/root/.bootlace-share-$CTID.cred
			umask 077
			{ echo "username=$SHARE_USER"; echo "password="; [ -n "$SHARE_DOMAIN" ] && echo "domain=$SHARE_DOMAIN"; true; } >"$cred"
			chmod 600 "$cred"
			auth="credentials=$cred"
		else
			auth="guest"
		fi
		# the container's root is user 100000 on the host: the files are shown as owned by it
		entry="//$SHARE_SERVER/$SHARE_PATH $SHARE_MOUNT cifs $auth,vers=3.0,iocharset=utf8,uid=100000,gid=100000,file_mode=0664,dir_mode=0775,$opts 0 0"
	else
		entry="$SHARE_SERVER:$SHARE_PATH $SHARE_MOUNT nfs hard,timeo=600,$opts 0 0"
	fi
	if ! grep -qF " $SHARE_MOUNT " /etc/fstab; then
		[ -f /etc/fstab.bootlace-backup ] || cp /etc/fstab /etc/fstab.bootlace-backup
		printf '\n# Gola Bootlace: the library of container %s\n%s\n' "$CTID" "$entry" >>/etc/fstab
	fi
	systemctl daemon-reload
	systemctl start "$(systemd-escape -p --suffix=automount "$SHARE_MOUNT")" 2>/dev/null || true
fi

# ---- the container
# the container's Notes in the Proxmox web page (it shows them as Markdown)
notes() {
	cat <<NOTES
# Gola Bootlace
Network boot server for ISO files: PCs on this network start and install Windows and Linux from the boot menu, without a USB stick.

- Web interface: \`http://<this container's address>:8080/admin/\` (the password is in \`journalctl -u bootlace\`, first lines)
- Library (the ISOs): ${LIB_NOTE}
- Made by [Gola Services](https://gola.tn). Documentation and source: [github.com/${BOOTLACE_REPO}](https://github.com/${BOOTLACE_REPO})
- Update: type \`update\` in this container's console. (Changing how the container is set up: \`CTID=${CTID} bash -c "\$(curl -fsSL https://raw.githubusercontent.com/${BOOTLACE_REPO}/main/tools/proxmox/bootlace-lxc.sh)"\`)
NOTES
}

case $LIB_MODE in
container) LIB_NOTE="inside the container (/srv/library)" ;;
host) LIB_NOTE="the folder $LIB_HOST_PATH of the Proxmox host" ;;
share) LIB_NOTE="the share $SHARE_SERVER:$SHARE_PATH, mounted on the host at $SHARE_MOUNT" ;;
esac
net="name=eth0,bridge=$BRIDGE,firewall=0,ip=$CT_IP"
[ "$CT_IP" = dhcp ] || { [ -n "${CT_GW:-}" ] && net="$net,gw=$CT_GW"; }
say "Creating container $CTID ($CT_HOSTNAME) ..."
pct create "$CTID" "$TEMPLATE_STORAGE:vztmpl/$template" --hostname "$CT_HOSTNAME" --cores "$CT_CORES" --memory "$CT_MEMORY" --swap 0 \
	--rootfs "$ROOTFS_STORAGE:$CT_DISK" --net0 "$net" --unprivileged 1 --features nesting=1 --onboot 1 --ostype debian \
	--description "$(notes)" "${MP_ARG[@]}"
pct start "$CTID"

say "Waiting for the network ..."
addr=""
for _ in $(seq 1 30); do
	addr=$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}') || true
	[ -n "$addr" ] && break
	sleep 2
done
[ -n "$addr" ] || die "the container got no address (is the bridge $BRIDGE connected to the network?)"
pct exec "$CTID" -- mkdir -p /srv/library
pct exec "$CTID" -- env LC_ALL=C DEBIAN_FRONTEND=noninteractive sh -c 'apt-get update -qq && apt-get install -y -qq wimtools curl ca-certificates >/dev/null 2>&1' || warn "wimtools or curl could not be installed (Windows needs wimtools for Secure Boot, and the update command needs curl): apt install wimtools curl in the container"

# The library has to be writable from inside the container. An unprivileged container's root is user 100000 on the host, so a folder that only the host's
# root can write to is read-only for it: say what to do, but never change the owner of the person's own folder from here.
library_note=""
if [ "$LIB_MODE" = host ] && ! pct exec "$CTID" -- sh -c 'f=/srv/library/.bootlace-write-test-$$; : > "$f" && rm -f "$f"' 2>/dev/null; then
	library_note="The container cannot write to $LIB_HOST_PATH, and the library must be writable. On this host, either give the container's root user (100000) access:
      chown 100000:100000 '$LIB_HOST_PATH'      (or: setfacl -m u:100000:rwx '$LIB_HOST_PATH')
    and then: pct exec $CTID -- systemctl restart bootlace   (or choose another folder in the web page)"
fi

say "Installing Bootlace in the container ..."
write_unit
pct push "$CTID" "$tmp/bootlace" /usr/local/bin/bootlace --perms 755
pct push "$CTID" "$tmp/bootlace.service" /etc/systemd/system/bootlace.service
pct push "$CTID" "$tmp/bootlace.default" /etc/default/bootlace
write_update
pct push "$CTID" "$tmp/update" /usr/local/bin/update --perms 755
pct push "$CTID" "$tmp/bootlace-update.conf" /etc/bootlace-update.conf
pct exec "$CTID" -- systemctl daemon-reload
pct exec "$CTID" -- systemctl enable --now bootlace
sleep 3
pct exec "$CTID" -- systemctl is-active bootlace >/dev/null || { pct exec "$CTID" -- journalctl -u bootlace -n 20 --no-pager; die "Bootlace did not start"; }

password=$(pct exec "$CTID" -- journalctl -u bootlace --no-pager 2>/dev/null | sed -n 's/.*the password was made up: \([^ ]*\) .*/\1/p' | tail -n 1)

cat <<DONE

Gola Bootlace is running in container $CTID at $addr.

  web page    http://$addr:8080/admin/   (ISOs, PCs and their rules, menu settings, live log)
  password    ${password:-"set earlier: see Account in the web page, or: pct exec $CTID -- journalctl -u bootlace | grep -i password"}
  menu        http://$addr:8080/boot.ipxe
  log         pct exec $CTID -- journalctl -u bootlace -f
  settings    kept in the container: /var/lib/bootlace/config.json (changed in the web page)
  update      type "update" in the container's console (or: pct exec $CTID update): it checks, asks, replaces the program and goes back if the new one does not answer
  set up again (the unit, the update command)   CTID=$CTID bash -c "\$(curl -fsSL https://raw.githubusercontent.com/$BOOTLACE_REPO/main/tools/proxmox/bootlace-lxc.sh)"
  remove      pct stop $CTID && pct destroy $CTID

DONE
case $LIB_MODE in
container) echo "  ISOs        upload them in the web page (Library), or copy them into /srv/library in the container. They use the container's disk ($CT_DISK GB)." ;;
host) echo "  ISOs        upload them in the web page (Library) or put them in $LIB_HOST_PATH on this host: that folder is the library." ;;
share)
	echo "  ISOs        the library is the share $([ "$SHARE_KIND" = smb ] && echo "//$SHARE_SERVER/$SHARE_PATH" || echo "$SHARE_SERVER:$SHARE_PATH"), mounted on this host at $SHARE_MOUNT (see the last lines of /etc/fstab)."
	if [ "$SHARE_KIND" = smb ] && [ -n "$SHARE_USER" ]; then
		echo "              Put the password in $cred (the line password=, only root can read the file). The next time the container looks at its library the share"
		echo "              mounts by itself. Give it the right password the first time: a NAS may lock an account after a few wrong ones."
	fi
	[ "$SHARE_KIND" = nfs ] && echo "              The container's root is user 100000 on this host: let the export map every user to one that can write (all_squash, anonuid), or Bootlace cannot write to it."
	echo "              Until the share is there, the server waits (the web page says why) and starts by itself when it is."
	;;
esac
[ -n "$library_note" ] && warn "  NOTE        $library_note"
cat <<DONE

On the PC to install: switch Secure Boot off (not supported yet, except for Windows and Ubuntu), start it from the network (PXE, usually F8 or F12 for the boot menu),
choose an ISO in the menu. Bootlace answers PCs that ask to network-boot and leaves your DHCP server alone. Nothing outside the local network is answered.
DONE
[ "$CT_IP" != dhcp ] || echo "Note: the container's address came from DHCP. Give it a fixed address (CT_IP=...) or a reservation in your router."
