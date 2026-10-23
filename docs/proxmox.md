# Running Gola Bootlace in a Proxmox container

Two ways, with the same result: a small Debian container with Bootlace running in it as a service.

## 1. The community-scripts script (recommended once it is published)

Bootlace is submitted to the [Proxmox VE Helper-Scripts / community-scripts](https://community-scripts.org/scripts). When it is there, it is installed from the Proxmox
host's shell with the command shown on the script's page, and updated by typing `update` in the container's console. The files for it are in
[deploy/community-scripts/](../deploy/community-scripts/) (`ct/`, `install/` and `json/`, in the layout that project asks for). The container has 1 core, 512 MB and 8 GB
by default; the library (the ISOs) is a folder in the container (`/srv/bootlace/library`) until you add a mount point and choose it under Server in the web interface.

## 2. This repository's own script

[`tools/proxmox/bootlace-lxc.sh`](../tools/proxmox/bootlace-lxc.sh) makes the container, asks the few questions that matter, and installs the program from the release
files. Run it as root on the Proxmox host, in a terminal:

```
bash -c "$(curl -fsSL https://raw.githubusercontent.com/ghazichaieb/gola-bootlace/main/tools/proxmox/bootlace-lxc.sh)"
```

**The questions** (press Enter to take the answer in brackets): the container's number and name, the network bridge, a fixed address or DHCP, cores, memory, disk, and
**where the ISOs are kept**:

1. **inside the container** (simplest; the ISOs use the container's disk, so give it more room);
2. **a folder on the Proxmox host**: bound into the container, read and write;
3. **a network share (SMB or NFS)**: mounted on the host and bound into the container.

Nothing is asked when the script is not run in a terminal, or with `BOOTLACE_YES=1`: it takes the defaults and the values given as environment variables
(`CTID`, `CT_HOSTNAME`, `CT_CORES`, `CT_MEMORY`, `CT_DISK`, `BRIDGE`, `CT_IP`, `CT_GW`, `LIB_MODE`, `LIB_HOST_PATH`, `SHARE_KIND`, `SHARE_SERVER`, `SHARE_PATH`, `SHARE_USER`,
`BOOTLACE_VERSION`, `BOOTLACE_ARGS`; the list is at the top of the script).

**What it changes on the host**, so that nothing is a surprise: it creates one container (it never touches another), may download a Debian template, and, for a share,
installs `cifs-utils` or `nfs-common` (after asking), adds two lines to `/etc/fstab` (a copy is kept as `/etc/fstab.bootlace-backup`), makes the folder
`/mnt/bootlace-library-<number>` and, for an SMB share that needs a login, an empty credentials file `/root/.bootlace-share-<number>.cred` (mode 600).

### The library: a folder or a share

The library must be readable **and writable** by the container. An unprivileged container's root is user 100000 on the host, so:

- **A host folder** must be writable for user 100000 (`chown 100000:100000 <folder>` or `setfacl -m u:100000:rwx <folder>`). The script checks this and says so; it never changes the owner of your folder itself.
- **An SMB share** is mounted with `uid=100000,gid=100000` so that its files look like the container's own. If it needs a login, fill in the password in the credentials file the script made (only root can read it). The password is never typed into the script, so it is not in a shell history.
- **An NFS export** has to map every user to one that can write (`all_squash,anonuid=<an owner of the export>`), because the server sees the container's user as 100000.

The share is mounted with `nofail,_netdev,x-systemd.automount`: the host starts without it, and mounts it the first time anything uses it (the container's own check does).
It does not matter whether the NAS starts before or after the container. Measured on Proxmox VE 9.2: a share mounted on the host after the container has started is seen by the
container at once; the host unmounting and mounting it again is followed too; and while the container has a file open the host cannot unmount it ("busy").
While the share is not there Bootlace waits ("the library is not available" in the web interface) and starts by itself when it is; if it goes away while the server runs
(a NAS restarting), the server keeps going for 30 seconds and then stops until it is back.

A wrong password is tried at each access (systemd limits how often): give a NAS that locks accounts after a few failures the right password first.

### Updating

Type `update` in the container's console (or `pct exec <number> update` on the host). It says which version is installed and which is the latest, asks before restarting the
server (PCs that are installing from it are interrupted), checks the download against the release's checksum, replaces the program and starts it. If the new version does
not answer, it goes back to the old one by itself. The old program is kept as `/usr/local/bin/bootlace.previous`. `update --check` only looks, `update -v 0.2.0` goes to a given
version, `update -y` does not ask.

To change how the container is set up (the unit, the library), run the script again with `CTID=<number>`; it recognises its own container, replaces the program and the
`update` command, and leaves the settings, the library and the mount alone.

### Other things

- The web page is `http://<container address>:8080/admin/`; the password was made up at the first start and is in the log (`pct exec <number> -- journalctl -u bootlace | grep -i password`). Change it under Account.
- The container must be on the same network (bridge) as the PCs to install. Bootlace answers PCs that ask to network-boot (proxy DHCP) and leaves the network's own DHCP server alone; it answers the local network only.
- Remove: `pct stop <number> && pct destroy <number>` (and, for a share, the two lines of `/etc/fstab`).
- Secure Boot has to be off on the PCs to install, except for Windows and Ubuntu (issue #10).
- What the container needs: [requirements.md](requirements.md).
- Until a release exists the program cannot be downloaded: give the script `BOOTLACE_FILE=/path/to/bootlace-<version>-linux-amd64.tar.gz` (made by `tools/release.sh --snapshot`).

Bootlace is made by [Gola Services](https://gola.tn).
