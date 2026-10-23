#!/bin/sh
# After the package is installed or upgraded (Debian: $1 is "configure"; RPM: $1 is the number of installed copies, 1 for a new install, 2 for an upgrade).
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl enable bootlace >/dev/null 2>&1 || true
	systemctl restart bootlace || true
	echo
	echo "Gola Bootlace is running. Open http://<this computer's address>:8080/admin/"
	echo "The password was made up at the first start and is in the log: journalctl -u bootlace | grep -i password"
	echo
fi
exit 0
