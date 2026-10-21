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

## ISOs on a network share

Mount the share on the Proxmox host (read-only) and point the script at the mount; the container sees it read-only at `/srv/isos`:

```
mkdir -p /mnt/isos
echo '//nas/share/ISOs /mnt/isos cifs guest,ro,vers=3.0,iocharset=utf8,file_mode=0444,dir_mode=0555,_netdev,nofail 0 0' >> /etc/fstab
mount /mnt/isos
ISO_PATH=/mnt/isos bash bootlace-lxc.sh        # or, for a container that exists: pct set <number> -mp0 /mnt/isos,mp=/srv/isos,ro=1
```

For a share that needs a login use `credentials=/root/.nas-isos` (a file with `username=` and `password=` lines, mode 600) instead of `guest`.
Mount only the folder with the ISOs (as in the path above), not the whole share.
