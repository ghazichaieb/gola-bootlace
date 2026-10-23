#!/bin/sh
# Builds the server for one system with its version number put in: tools/build.sh [os] [arch]   (default linux amd64; output in dist/).
# The version is the latest tag "vX.Y.Z" (a build after the tag reads X.Y.Z-<commits>-g<hash>), or VERSION=... from the environment.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
set -eu
cd "$(dirname "$0")/.."
os=${1:-linux}
arch=${2:-amd64}
if [ -z "${VERSION:-}" ]; then
	VERSION=$(git describe --tags --match 'v[0-9]*' --dirty --always 2>/dev/null || echo unknown)
	case $VERSION in
	v*) VERSION=${VERSION#v} ;;
	*) VERSION="0.0.0-dev+$VERSION" ;; # no tag yet: only the commit
	esac
fi
out=dist/bootlace-$os-$arch
[ "$os" = windows ] && out=$out.exe
mkdir -p dist
GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/ghazichaieb/gola-bootlace/internal/version.Version=$VERSION" -o "$out" ./cmd/bootlace
echo "$out  (version $VERSION)"
