# Running Bootlace in a Proxmox container

`tools/proxmox/bootlace-lxc.sh` makes a small Debian LXC container on a Proxmox host, puts the Bootlace server in it as a systemd service, and shows
the host's ISO folder to it, read and write: that folder is the library (see below). Run it as root on the Proxmox host:

```
bash -c "$(curl -fsSL <link to the script>)"
```

Defaults: next free container number, name `bootlace`, 1 core, 512 MB, 4 GB disk, bridge `vmbr0`, address by DHCP (a fixed one is better:
`CT_IP=192.168.1.50/24 CT_GW=192.168.1.1`), ISOs from `/var/lib/vz/template/iso` (where the Proxmox web interface keeps uploaded ISOs; change with
`ISO_PATH`). The list of every setting is at the top of the script.

* Web page: `http://<container address>:8080/admin/`. The script prints the password it was started with; it is also in `journalctl -u bootlace`.
* ISOs: copy them into the library folder of the host (or, soon, upload them in the web page); the menu looks at the folder for every PC that boots, so no restart is needed.
* The library has to be writable by the container. The container is unprivileged, so its root is user 100000 on the host: a folder that only the host's root can write to is read-only for it. The script checks, and says what to do (`chown 100000:100000 <folder>` or `setfacl -m u:100000:rwx <folder>`); it never changes the owner of your folder itself. You can choose another folder later in the web page under Server.
* Update: run the script again with `CTID=<number>`; it replaces the program and restarts the service.
* Remove: `pct stop <number> && pct destroy <number>`.
* The container must be on the same network (bridge) as the PCs to install. Bootlace answers PCs that ask to network-boot (proxy DHCP) and leaves
  the network's own DHCP server alone.
* While the repository is private the program cannot be downloaded without a token: copy `bootlace-linux-amd64` to the host and run the script
  with `BOOTLACE_BIN=/path/to/bootlace-linux-amd64`.

Secure Boot has to be off on the PCs to install (issue #10).

Build the program for Linux with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bootlace-linux-amd64 ./cmd/bootlace`.

## ISOs on a network share

Mount the share on the Proxmox host (read and write: it is the library) and point the script at the mount; the container sees it at `/srv/isos`. A network share is best mounted on the host, not inside the container:

```
mkdir -p /mnt/isos
echo '//nas/share/ISOs /mnt/isos cifs guest,vers=3.0,iocharset=utf8,uid=100000,gid=100000,file_mode=0664,dir_mode=0775,_netdev,nofail 0 0' >> /etc/fstab
mount /mnt/isos
ISO_PATH=/mnt/isos bash bootlace-lxc.sh        # or, for a container that exists: pct set <number> -mp0 /mnt/isos,mp=/srv/isos,ro=0
```

For a share that needs a login use `credentials=/root/.nas-isos` (a file with `username=` and `password=` lines, mode 600) instead of `guest`.
Mount only the folder with the ISOs (as in the path above), not the whole share.
