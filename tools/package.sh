#!/bin/sh
# Makes the Debian and RPM packages of the program in dist/: tools/package.sh   (needs Docker: the packages are made by nfpm, goreleaser/nfpm, in a container)
#
#   bootlace-<version>-linux-amd64.deb / .rpm    for x86-64 Debian, Ubuntu, Proxmox hosts, Fedora, RHEL and relatives
#   bootlace-<version>-linux-arm64.deb / .rpm    for ARM (a Raspberry Pi, an ARM server)
#
# Each package installs /usr/bin/bootlace, a systemd service (bootlace.service) that it enables and starts, /etc/default/bootlace for the settings (kept on upgrade), and the
# documents; removing the package stops and disables the service and leaves /var/lib/bootlace (the settings and the checksum index) alone.
# The version is the one tools/build.sh puts in the program (VERSION from the environment, or the latest tag).
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
set -eu
cd "$(dirname "$0")/.."
command -v docker >/dev/null 2>&1 || { echo "package: Docker is needed (nfpm runs in a container)" >&2; exit 1; }
if [ -z "${VERSION:-}" ]; then
	VERSION=$(git describe --tags --match 'v[0-9]*' --dirty --always 2>/dev/null || echo unknown)
	case $VERSION in v*) VERSION=${VERSION#v} ;; *) VERSION="0.0.0-dev+$VERSION" ;; esac
fi
# a package version has no "+" and no second hyphen: a snapshot reads 0.0.0~dev.<hash> (the tilde sorts before the release)
PKGVERSION=$(printf '%s' "$VERSION" | sed 's/-dev+/~dev./; s/-\([0-9][0-9]*\)-g/~\1.g/')
mkdir -p dist
for arch in amd64 arm64; do
	VERSION=$VERSION sh tools/build.sh linux "$arch" >/dev/null
	# the built program is moved out of the way of the release files (a release lists every bootlace-* file of dist/), and nfpm does not expand a variable in a file
	# name: the program's path is put in a copy of the configuration
	mv "dist/bootlace-linux-$arch" "dist/.package-bootlace-$arch"
	sed "s#\${BINARY}#dist/.package-bootlace-$arch#" deploy/linux/nfpm.yaml >"dist/.package-nfpm-$arch.yaml"
	for format in deb rpm; do
		out=dist/bootlace-$VERSION-linux-$arch.$format
		docker run --rm -v "$PWD:/work" -w /work -e VERSION="$PKGVERSION" -e ARCH="$arch" \
			goreleaser/nfpm package -f "dist/.package-nfpm-$arch.yaml" -p "$format" -t "$out" >/dev/null
		echo "made $out"
	done
	rm -f "dist/.package-bootlace-$arch" "dist/.package-nfpm-$arch.yaml"
done
