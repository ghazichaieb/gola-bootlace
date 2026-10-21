# Running Bootlace in a Proxmox container

`tools/proxmox/bootlace-lxc.sh` makes a small Debian LXC container on a Proxmox host, puts the Bootlace server in it as a systemd service, and shows
the host's ISO folder to it read-only. Run it as root on the Proxmox host:

```
bash -c "$(curl -fsSL <link to the script>)"
```

Defaults: next free container number, name `bootlace`, 1 core, 512 MB, 4 GB disk, bridge `vmbr0`, address by DHCP (a fixed one is better:
`CT_IP=192.168.1.50/24 CT_GW=192.168.1.1`), ISOs from `/var/lib/vz/template/iso` (where the Proxmox web interface keeps uploaded ISOs; change with
`ISO_PATH`). The list of every setting is at the top of the script.

* ISOs: upload or copy them into the ISO folder of the host; the menu looks at the folder for every PC that boots, so no restart is needed.
* Update: run the script again with `CTID=<number>`; it replaces the program and restarts the service.
* Remove: `pct stop <number> && pct destroy <number>`.
* The container must be on the same network (bridge) as the PCs to install. Bootlace answers PCs that ask to network-boot (proxy DHCP) and leaves
  the network's own DHCP server alone.
* While the repository is private the program cannot be downloaded without a token: copy `bootlace-linux-amd64` to the host and run the script
  with `BOOTLACE_BIN=/path/to/bootlace-linux-amd64`.

Secure Boot has to be off on the PCs to install (issue #10).

Build the program for Linux with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bootlace-linux-amd64 ./cmd/bootlace`.
