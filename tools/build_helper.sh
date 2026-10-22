#!/bin/sh
# Builds the helper that runs in a PC's initramfs (internal/assets/helper/bootlace-nbd-linux-amd64): a static Linux binary of cmd/bootlace-nbd.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
set -eu
cd "$(dirname "$0")/.."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o internal/assets/helper/bootlace-nbd-linux-amd64 ./cmd/bootlace-nbd
ls -l internal/assets/helper/bootlace-nbd-linux-amd64
