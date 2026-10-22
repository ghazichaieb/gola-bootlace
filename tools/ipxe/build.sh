#!/bin/sh
# Builds the three iPXE files Bootlace ships (internal/assets/ipxe) from source, with the options in config/ below.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
#   docker run --rm -v "$PWD/tools/ipxe:/work" -v "$PWD/internal/assets/ipxe:/out" debian:12 sh /work/build.sh [commit]
#
# Why not the files from boot.ipxe.org: its BIOS file (undionly.kpxe) is from 2019 and is too old for wimboot 2.9 (it does not wrap the files of an
# "initrd --name" in the CPIO headers wimboot reads: "Bad CPIO magic"), and it has no picture, colour or console command.
set -eu
COMMIT="${1:-a5eed98d964bc48790041aed41a8876fa0b7eba5}"   # the commit the shipped files were built from (NOTICE)
apt-get update -qq
apt-get install -y -qq git make gcc binutils perl liblzma-dev mtools xz-utils ca-certificates >/dev/null
git clone -q https://github.com/ipxe/ipxe.git /ipxe
cd /ipxe
git checkout -q "$COMMIT"
echo "iPXE commit: $(git rev-parse HEAD)"
cp /work/config/*.h src/config/local/
cd src
make -j"$(nproc)" bin/undionly.kpxe bin-x86_64-efi/ipxe.efi bin-x86_64-efi/snponly.efi
cp bin/undionly.kpxe bin-x86_64-efi/ipxe.efi bin-x86_64-efi/snponly.efi /out/
sha256sum /out/undionly.kpxe /out/ipxe.efi /out/snponly.efi
