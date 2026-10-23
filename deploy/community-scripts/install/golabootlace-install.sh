#!/usr/bin/env bash

# Copyright (c) 2021-2026 community-scripts ORG
# Author: ghazichaieb
# License: MIT | https://github.com/community-scripts/ProxmoxVE/raw/main/LICENSE
# Source: https://github.com/ghazichaieb/gola-bootlace

source /dev/stdin <<<"$FUNCTIONS_FILE_PATH"
color
verb_ip6
catch_errors
setting_up_container
network_check
update_os

msg_info "Installing Dependencies"
$STD apt install -y wimtools
msg_ok "Installed Dependencies"

fetch_and_deploy_gh_release "bootlace" "ghazichaieb/gola-bootlace" "prebuild" "latest" "/opt/bootlace" "bootlace-*-linux-$(dpkg --print-architecture).tar.gz"

msg_info "Configuring Gola Bootlace"
mkdir -p /srv/bootlace/library /var/lib/bootlace
cat <<EOF >/etc/bootlace.env
BOOTLACE_CONFIG=/var/lib/bootlace/bootlace.json
BOOTLACE_ISO_DIR=/srv/bootlace/library
BOOTLACE_DHCP=proxy
EOF
cat <<EOF >/etc/systemd/system/bootlace.service
[Unit]
Description=Gola Bootlace network boot server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/bootlace.env
ExecStart=/opt/bootlace/bootlace serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
systemctl enable -q --now bootlace
msg_ok "Configured Gola Bootlace"

motd_ssh
customize
cleanup_lxc
