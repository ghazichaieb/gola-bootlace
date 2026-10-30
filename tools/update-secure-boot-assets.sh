#!/bin/sh
# Renews the iPXE project's signed files that are built into the program (internal/secureboot/assets): the shim signed by Microsoft and the iPXE it starts.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
#   tools/update-secure-boot-assets.sh <release> <sha256 of ipxeboot.tar.gz>
#   tools/update-secure-boot-assets.sh <release> --trust-printed-hash
#   tools/update-secure-boot-assets.sh <release> <sha256> --shim-release <tag> <sha256 of ipxe-shimx64.efi>
#
#   <release>   the iPXE release tag, for example v2.0.0 (https://github.com/ipxe/ipxe/releases)
#   <sha256>    the checksum the release file must have; get it from a place other than the download itself (the release page, a second computer, the project's build log).
#               The script prints the checksum of what it downloaded and stops when it is not this one.
#   --trust-printed-hash  take whatever was downloaded: the checksum is only printed and written into MANIFEST.json. Without a checksum argument this flag is required, so
#               that a file is never accepted without somebody having decided to.
#   --shim-release <tag> <sha256>   take the shim from the iPXE project's shim release (https://github.com/ipxe/shim/releases, file ipxe-shimx64.efi) instead of the archive,
#               and record that in MANIFEST.json. Needed when the project re-signed the shim after the iPXE release: ipxe-16.1 was re-uploaded on 2026-05-27 signed with both
#               Microsoft CAs (2011 and 2023), while the v2.0.0 archive carries the 2011-only build. The checksum is required like the archive's (or --trust-printed-hash).
#   --from <file>   use a copy of ipxeboot.tar.gz that is already on this computer instead of downloading it
#   --released <YYYY-MM-DD>   the release's date for MANIFEST.json (default: read from the archive)
#   --out <folder>  write the files here instead of internal/secureboot/assets (to look at the result without changing the program)
#
# MANIFEST.json also records, for the shim, which Microsoft CAs signed it ("signedWith": 2011, 2023): found by looking for their names in the file's signature data.
#
# Only the two files the program needs are taken out of the archive (x86_64-sb/shimx64.efi, which is served as ipxe-shimx64.efi, and x86_64-sb/ipxe.efi). The script rewrites
# them and MANIFEST.json, prints which checksums changed, and leaves the commit to you. Afterwards: go test ./internal/secureboot, then update NOTICE, third_party/SOURCES and
# docs/secure-boot.md (the release, the shim version, the checksums) as "Keeping the built-in files current" there says.
set -eu
cd "$(dirname "$0")/.."

usage() {
	sed -n '2,/^set -eu/p' "$0" | sed -n 's/^# \{0,1\}//p' | grep -v '^set -eu' >&2
	exit 2
}

release=""
want=""
trust=0
from=""
released=""
shimrel=""
shimwant=""
out="internal/secureboot/assets"
while [ $# -gt 0 ]; do
	case $1 in
	--trust-printed-hash) trust=1 ;;
	--from) shift; from=${1:?--from needs a file} ;;
	--released) shift; released=${1:?--released needs a date} ;;
	--shim-release) shift; shimrel=${1:?--shim-release needs a tag}; shift; shimwant=${1:?--shim-release needs the SHA-256 of the shim} ;;
	--out) shift; out=${1:?--out needs a folder} ;;
	-h | --help) usage ;;
	-*) echo "update-secure-boot-assets: unknown option $1" >&2; usage ;;
	*)
		if [ -z "$release" ]; then release=$1; elif [ -z "$want" ]; then want=$1; else usage; fi
		;;
	esac
	shift
done
[ -n "$release" ] || usage
case $release in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "update-secure-boot-assets: $release is not a release tag like v2.0.0" >&2; exit 2 ;;
esac
if [ -z "$want" ] && [ "$trust" = 0 ]; then
	echo "update-secure-boot-assets: give the SHA-256 the release file must have, or --trust-printed-hash to take whatever is downloaded" >&2
	exit 2
fi
for sum in $want $shimwant; do
	case $sum in
	*[!0-9a-f]* | "") echo "update-secure-boot-assets: the checksum must be 64 lower-case hexadecimal digits" >&2; exit 2 ;;
	esac
	[ ${#sum} = 64 ] || { echo "update-secure-boot-assets: the checksum must be 64 lower-case hexadecimal digits" >&2; exit 2; }
done
case $shimrel in
"" | ipxe-[0-9]*) ;;
*) echo "update-secure-boot-assets: $shimrel is not a shim release tag like ipxe-16.1" >&2; exit 2 ;;
esac

url="https://github.com/ipxe/ipxe/releases/download/$release/ipxeboot.tar.gz"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ -n "$from" ]; then
	cp "$from" "$work/ipxeboot.tar.gz"
	url="$url (read from $from)"
else
	curl -fsSL -o "$work/ipxeboot.tar.gz" "$url" || { echo "update-secure-boot-assets: cannot download $url" >&2; exit 1; }
fi
have=$(sha256sum "$work/ipxeboot.tar.gz" | awk '{print $1}')
echo "ipxeboot.tar.gz of $release: SHA-256 $have"
if [ -n "$want" ]; then
	[ "$have" = "$want" ] || { echo "update-secure-boot-assets: that is not the checksum that was asked for ($want): nothing was changed" >&2; exit 1; }
	[ "$trust" = 0 ] || echo "(--trust-printed-hash is not needed with a checksum and was ignored)"
	echo "matches the checksum given"
else
	echo "no checksum was given: taking the printed one because --trust-printed-hash was used"
fi

folder=ipxeboot/x86_64-sb
mkdir "$work/x"
tar -xzf "$work/ipxeboot.tar.gz" -C "$work/x" "$folder/shimx64.efi" "$folder/ipxe.efi" || { echo "update-secure-boot-assets: $folder/shimx64.efi and $folder/ipxe.efi are not in the archive" >&2; exit 1; }
for f in shimx64.efi ipxe.efi; do
	p=$work/x/$folder/$f
	[ -f "$p" ] && [ ! -L "$p" ] && [ -s "$p" ] || { echo "update-secure-boot-assets: $folder/$f is not a regular, non-empty file" >&2; exit 1; }
	[ "$(head -c 2 "$p")" = "MZ" ] || { echo "update-secure-boot-assets: $folder/$f is not an EFI program (no MZ header)" >&2; exit 1; }
done
if [ -z "$released" ]; then
	released=$(tar -tvzf "$work/ipxeboot.tar.gz" "$folder/ipxe.efi" 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/) { print $i; exit }}')
	[ -n "$released" ] || released=unknown
fi

mkdir -p "$out"
oldsums=$(sed -n 's/.*"name": *"\([^"]*\)".*"sha256": *"\([0-9a-f]*\)".*/\1 \2/p' "$out/MANIFEST.json" 2>/dev/null || true)
cp "$work/x/$folder/shimx64.efi" "$out/ipxe-shimx64.efi"
shimentry='"from": "shimx64.efi"'
if [ -n "$shimrel" ]; then
	shimurl="https://github.com/ipxe/shim/releases/download/$shimrel/ipxe-shimx64.efi"
	curl -fsSL -o "$work/ipxe-shimx64.efi" "$shimurl" || { echo "update-secure-boot-assets: cannot download $shimurl" >&2; exit 1; }
	shimhave=$(sha256sum "$work/ipxe-shimx64.efi" | awk '{print $1}')
	echo "ipxe-shimx64.efi of $shimrel: SHA-256 $shimhave"
	[ -n "$shimwant" ] && [ "$shimhave" != "$shimwant" ] && { echo "update-secure-boot-assets: the shim is not the checksum that was asked for ($shimwant): nothing was changed" >&2; exit 1; }
	[ "$(head -c 2 "$work/ipxe-shimx64.efi")" = "MZ" ] || { echo "update-secure-boot-assets: $shimurl is not an EFI program (no MZ header)" >&2; exit 1; }
	cp "$work/ipxe-shimx64.efi" "$out/ipxe-shimx64.efi"
	shimentry="\"from\": \"ipxe-shimx64.efi\", \"release\": \"$shimrel\", \"source\": \"$shimurl\""
fi
signed=""
grep -a -q "Microsoft Corporation UEFI CA 2011" "$out/ipxe-shimx64.efi" && signed='"2011"'
grep -a -q "Microsoft UEFI CA 2023" "$out/ipxe-shimx64.efi" && signed="${signed:+$signed, }\"2023\""
echo "signature data of the shim names the Microsoft CA(s): ${signed:-none found}"
cp "$work/x/$folder/ipxe.efi" "$out/ipxe.efi"
shimsum=$(sha256sum "$out/ipxe-shimx64.efi" | awk '{print $1}')
shimsize=$(wc -c <"$out/ipxe-shimx64.efi" | tr -d ' ')
ipxesum=$(sha256sum "$out/ipxe.efi" | awk '{print $1}')
ipxesize=$(wc -c <"$out/ipxe.efi" | tr -d ' ')
cat >"$out/MANIFEST.json" <<EOF
{
  "source": "https://github.com/ipxe/ipxe/releases/download/$release/ipxeboot.tar.gz",
  "release": "$release",
  "released": "$released",
  "added": "$(date -u +%Y-%m-%d)",
  "tarballSha256": "$have",
  "folder": "x86_64-sb",
  "files": [
    {"name": "ipxe-shimx64.efi", $shimentry, "signedWith": [$signed], "size": $shimsize, "sha256": "$shimsum"},
    {"name": "ipxe.efi", "from": "ipxe.efi", "size": $ipxesize, "sha256": "$ipxesum"}
  ]
}
EOF

echo
echo "written to $out:"
for pair in "ipxe-shimx64.efi $shimsum $shimsize" "ipxe.efi $ipxesum $ipxesize"; do
	set -- $pair
	old=$(printf '%s\n' "$oldsums" | awk -v n="$1" '$1 == n {print $2}')
	if [ -z "$old" ]; then
		echo "  $1: new, $3 bytes, SHA-256 $2"
	elif [ "$old" = "$2" ]; then
		echo "  $1: unchanged ($3 bytes, SHA-256 $2)"
	else
		echo "  $1: CHANGED, $3 bytes"
		echo "      was $old"
		echo "      now $2"
	fi
done
echo
echo "Next: go test ./internal/secureboot; read the release notes of iPXE $release and of the shim it uses (SBAT / revocation level); update NOTICE, third_party/SOURCES and docs/secure-boot.md."
