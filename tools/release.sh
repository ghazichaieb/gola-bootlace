#!/bin/sh
# Makes the files of a release in dist/: tools/release.sh [--snapshot]
#
#   bootlace-<version>-linux-amd64.tar.gz     the program for Linux (also what the Proxmox scripts install)
#   bootlace-<version>-linux-arm64.tar.gz     the same for ARM (a Raspberry Pi, an ARM Proxmox host)
#   bootlace-<version>-windows-amd64.zip      the program for Windows
#   SHA256SUMS                                the checksums of the files above
#
# Each archive holds the program (bootlace or bootlace.exe), README.md, CHANGELOG.md, LICENSE, ADDITIONAL-TERMS.md and NOTICE.
# A release is made from a commit that has a tag vX.Y.Z and a clean working tree; --snapshot allows any state (the version then reads X.Y.Z-N-gHASH).
# Nothing is published: the script ends by printing the commands for the tag, the release and the Docker image, to be run when the release is decided.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
set -eu
cd "$(dirname "$0")/.."

snapshot=0
[ "${1:-}" = "--snapshot" ] && snapshot=1

if [ "$snapshot" = 0 ]; then
	[ -z "$(git status --porcelain)" ] || { echo "release: the working tree has changes: commit them first (or use --snapshot)" >&2; exit 1; }
	tag=$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null) || { echo "release: this commit has no tag vX.Y.Z (make one, or use --snapshot)" >&2; exit 1; }
	VERSION=${tag#v}
else
	VERSION=$(git describe --tags --match 'v[0-9]*' --dirty --always 2>/dev/null || echo unknown)
	case $VERSION in v*) VERSION=${VERSION#v} ;; *) VERSION="0.0.0-dev+$VERSION" ;; esac
fi
case $VERSION in
[0-9]*.[0-9]*.[0-9]*) ;;
*) [ "$snapshot" = 1 ] || { echo "release: $VERSION is not a version number (X.Y.Z or X.Y.Z-rc.N)" >&2; exit 1; } ;;
esac
export VERSION
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)
export SOURCE_DATE_EPOCH

rm -rf dist
mkdir -p dist/stage
for target in linux/amd64 linux/arm64 windows/amd64; do
	os=${target%/*}
	arch=${target#*/}
	sh tools/build.sh "$os" "$arch" >/dev/null
	name=bootlace-$VERSION-$os-$arch
	dir=dist/stage/$name
	mkdir -p "$dir"
	bin=bootlace
	[ "$os" = windows ] && bin=bootlace.exe
	mv "dist/bootlace-$os-$arch$([ "$os" = windows ] && echo .exe)" "$dir/$bin"
	cp README.md CHANGELOG.md LICENSE ADDITIONAL-TERMS.md NOTICE "$dir/"
	ext=tar.gz
	[ "$os" = windows ] && ext=zip
	# tools/archive makes the same archive on every computer: owner 0 (an archive that names the builder's own user cannot be unpacked by root in an
	# unprivileged container), the program executable (Windows has no such bit), every file with the time of the commit
	go run ./tools/archive "$dir" "dist/$name.$ext"
	echo "made dist/$name.$ext"
done
rm -rf dist/stage
(cd dist && sha256sum bootlace-* >SHA256SUMS)
echo
cat dist/SHA256SUMS
echo

if [ "$snapshot" = 1 ]; then
	echo "snapshot $VERSION: the files are in dist/ (to test; nothing to publish)"
	exit 0
fi

# the Docker tags of this version: the exact one always; for a final release also X.Y, X (from 1.0) and latest
tags="$VERSION"
case $VERSION in
*-*) ;; # a pre-release (X.Y.Z-rc.N) is never "latest"
*)
	major=${VERSION%%.*}
	minor=${VERSION%.*}
	tags="$tags $minor latest"
	[ "$major" != 0 ] && tags="$tags $major"
	;;
esac
image=ghcr.io/ghazichaieb/gola-bootlace
opts=""
for t in $tags; do opts="$opts -t $image:$t"; done
echo "version $VERSION. To publish it (not done by this script):"
echo "  git push origin v$VERSION"
echo "  gh release create v$VERSION dist/bootlace-* dist/SHA256SUMS --repo ghazichaieb/gola-bootlace --title \"Gola Bootlace $VERSION\" --notes-file <the section of CHANGELOG.md>"
echo "  docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$VERSION$opts --push ."
