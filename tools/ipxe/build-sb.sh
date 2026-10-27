#!/bin/sh
# Builds the iPXE file for the UEFI Secure Boot route (docs/secure-boot.md, "the signed-iPXE route") and signs it with a key given by path.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
#   docker run --rm -v "$PWD/tools/ipxe:/work" -v "$PWD/out:/out" -v "/path/to/keys:/keys:ro" debian:12 sh /work/build-sb.sh [commit]
#
# /keys holds the signing key and its certificate: db.key (PEM, private) and db.crt (PEM). Other names: KEY=... CERT=... in the environment (docker run -e).
# Nothing in this repository is a key; a key is never copied into the image or the output. The certificate is the one a person enrols in each PC
# (MokManager: "Enroll key from disk"); it is also written to /out as secureboot.crt (and secureboot.cer, the DER form MokManager lists) for that purpose.
#
# What it makes in /out:
#   ipxe-sb.efi        iPXE built with SECUREBOOT=1 (iPXE's own safe set: no GDB stub, no unsafe drivers or commands) and signed. The shim starts it as its second stage,
#                      so the server answers a PC's request for grubx64.efi with this file.
#   ipxe-unsigned.efi  the same build before signing (to compare, or to sign elsewhere)
#   secureboot.crt, secureboot.cer
# Two runs give different files (iPXE puts a build id into the file): to keep one build, keep the signed file, not the recipe.
#
# Only the SECUREBOOT=1 build is made: the full feature set holds commands that can start unsigned code, and a signed hole is a Secure Boot bypass for every PC that trusts the key.
set -eu
COMMIT="${1:-a5eed98d964bc48790041aed41a8876fa0b7eba5}"   # the commit the shipped files are built from (NOTICE)
KEY="${KEY:-/keys/db.key}"
CERT="${CERT:-/keys/db.crt}"
[ -r "$KEY" ] || { echo "no signing key at $KEY (mount the folder with -v DIR:/keys:ro)"; exit 2; }
[ -r "$CERT" ] || { echo "no certificate at $CERT"; exit 2; }
apt-get update -qq
apt-get install -y -qq git make gcc binutils perl liblzma-dev mtools xz-utils ca-certificates sbsigntool openssl >/dev/null
git clone -q https://github.com/ipxe/ipxe.git /ipxe
cd /ipxe
git checkout -q "$COMMIT"
echo "iPXE commit: $(git rev-parse HEAD)"
cp /work/config/*.h /work/config-sb/*.h src/config/local/
cd src
make -j"$(nproc)" bin-x86_64-efi-sb/ipxe.efi
OUT=bin-x86_64-efi-sb/ipxe.efi
cp "$OUT" /out/ipxe-unsigned.efi
sbsign --key "$KEY" --cert "$CERT" --output /out/ipxe-sb.efi /out/ipxe-unsigned.efi
sbverify --cert "$CERT" /out/ipxe-sb.efi
cp "$CERT" /out/secureboot.crt
openssl x509 -in "$CERT" -outform DER -out /out/secureboot.cer
sha256sum /out/ipxe-unsigned.efi /out/ipxe-sb.efi
