#!/bin/sh
# Packs the source code of the GPL programs shipped inside Gola Bootlace (third_party/SOURCES) into one file: tools/third_party_sources.sh <out.tar>
# Every file is downloaded and checked against the checksum in the list; a file that does not match stops the release.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
set -eu
cd "$(dirname "$0")/.."
out=${1:?usage: third_party_sources.sh <out.tar>}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/third-party-sources"
grep -v '^#' third_party/SOURCES | grep -v '^$' | while read -r sum name url; do
	curl -fsSL -o "$work/third-party-sources/$name" "$url" || { echo "third_party_sources: cannot download $url" >&2; exit 1; }
	have=$(sha256sum "$work/third-party-sources/$name" | awk '{print $1}')
	[ "$have" = "$sum" ] || { echo "third_party_sources: $name does not match third_party/SOURCES (got $have)" >&2; exit 1; }
done
cp third_party/SOURCES "$work/third-party-sources/SOURCES"
cat >"$work/third-party-sources/README.txt" <<'TXT'
The source code of the programs that are inside the Gola Bootlace program (iPXE, wimboot, Ubuntu's signed GRUB, and the iPXE project's signed Secure Boot shim with gnu-efi), exactly as they were when they were
shipped. SOURCES lists each file with its checksum and where it comes from. How the iPXE files are built from this source: tools/ipxe/ in the Gola Bootlace source.
TXT
tar -C "$work" --owner=0 --group=0 --numeric-owner -cf "$out" third-party-sources
echo "made $out"
