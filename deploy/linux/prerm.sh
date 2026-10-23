#!/bin/sh
# Before the package is removed or upgraded (Debian: $1 is "remove" or "upgrade"; RPM: 0 when the last copy is removed, 1 for an upgrade). An upgrade keeps the service enabled.
set -e
case "$1" in
remove | 0)
	if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
		systemctl disable --now bootlace >/dev/null 2>&1 || true
	fi
	;;
esac
exit 0
