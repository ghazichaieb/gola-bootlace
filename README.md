<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/bootlace-logo-dark.svg">
    <img alt="Gola Bootlace" src="branding/bootlace-logo.svg" width="460">
  </picture>
</p>

# Gola Bootlace

**Install Windows and Linux on any PC without a USB stick.** Put your ISO files in a folder on one computer; every other PC on the network can start any of them from its
boot menu, run it, and install it. Think of [Ventoy](https://github.com/ventoy/Ventoy), but over the network. Made by [Gola Services](https://gola.tn).

**Status: early.** It works on the ISOs listed in [docs/compatibility.md](docs/compatibility.md), tested in a lab of virtual machines (Proxmox and Hyper-V), with a
handful of real PCs still to try. The ISO files are never changed.

---

## For everyone

### What is it, in plain words

Normally, to install an operating system you copy an ISO file onto a USB stick, plug it into the PC, and start the PC from the stick. Do that for twenty PCs and ten different
systems and you have a drawer of sticks and a lot of waiting.

Modern PCs can also start **from the network** (this is called *PXE*, or "network boot"). Bootlace is the program that answers them. It lives on one always-on computer (a small
server, a spare PC, a Proxmox container), looks in a folder of ISO files, and shows every PC that asks a **menu** of what it can start. Choose Windows 11 or
Ubuntu or a rescue tool, and the PC runs it, reading the ISO from the server across the network.

You get:
- one folder of ISOs instead of a drawer of sticks; add an ISO by copying a file, remove it by deleting the file;
- the same menu on every PC, UEFI or older BIOS, without touching its settings beyond "boot from network";
- ISOs that start **as they are**: nothing is unpacked or edited on the way.

### What you need

- **A server**: any computer that stays on while PCs are installed (Windows, Linux, or a [Proxmox container](docs/proxmox.md)), on the same network as the PCs, with space for the ISOs.
- **The ISO files** you want to offer (Windows, Linux, rescue tools, ...).
- **PCs that can network boot** (almost all can: look for "PXE", "Network boot" or "IPv4" in the boot menu or BIOS), connected by **cable**. Wi-Fi does not work for this.
- A normal home or office network. **You do not have to change your router**: Bootlace answers only PCs that ask to network boot and leaves the network's own DHCP server alone.

### Using it, step by step

1. **Get the program** (`bootlace`, one file): download it for Linux or Windows from the [Releases page](https://github.com/ghazichaieb/gola-bootlace/releases) (the releases are signed: [docs/verifying.md](docs/verifying.md)), use the Proxmox script or Docker image below, or build it as described in [For developers](#for-developers).
2. **Make a folder** for the ISOs and copy them in. It becomes the **library**: Bootlace needs to read *and* write it (it adds a hidden `.bootlace` folder), and you can choose another folder later in the web interface. You can also add ISOs from the web interface (**Library**): upload them, or give a web address and the server downloads the file itself, checks it and keeps it. (ISOs can be in sub-folders. `.iso` files, disk images `.img`, and packed ones such as `.iso.xz` or `.zip` are all accepted.)
3. **Start it** on the server:
   ```
   bootlace serve -iso-dir /path/to/the/isos
   ```
   The first time it makes up a password for the management page and prints it **once** (look for "the password was made up" in its output or log; see [docs/web-interface.md](docs/web-interface.md#password) if you miss it).
4. **On the PC you want to install**: switch it on and choose **boot from network** (often F12, F11 or Esc at the start; or set the boot order). It gets a boot screen with the
   Bootlace logo and a menu of your ISOs.
5. **Choose one with the arrow keys and Enter.** It starts. Install as you always would.
6. **After the installation, the PC restarts** and asks the network again, and the menu waits for a choice. Pick **"Boot from this PC's own disk"**, or start the server with
   `-menu-local -menu-timeout 5`, which does it by itself after five seconds (or give one PC a rule on the management page).

That is all. Stop the program and nothing is left on the PCs.

### The management page

Open `http://<server address>:8080/admin/` in a browser (the password is in the server's log the first time, shown once, or set it yourself with `-admin-password`). There you can see the PCs that
asked for a menu, rename or hide an ISO, choose what starts by itself after some seconds, give one PC a rule ("always boot from your own disk", "start this ISO without
asking"), and watch what is happening. It is described in [docs/web-interface.md](docs/web-interface.md).

### Running it on your own server

Pick the one that fits the computer you have. All four give the same program and the same web page; they differ in how it is kept running. Whichever you choose, the server must be on
the **same network as the PCs** (cabled), and the first thing it shows is a password for the web page (see [docs/web-interface.md](docs/web-interface.md#password) if you miss it).
What each one needs in memory and disk: [docs/requirements.md](docs/requirements.md).

**1. A Proxmox container (the easiest if you have Proxmox).** On the Proxmox host, in a terminal as root:

```
bash -c "$(curl -fsSL https://raw.githubusercontent.com/ghazichaieb/gola-bootlace/main/tools/proxmox/bootlace-lxc.sh)"
```

It asks a few questions (container number, network, and where the ISOs are kept: inside the container, a folder of the host, or a network share), makes a small Debian container,
installs Bootlace, starts it and prints the address and the password. Later, type `update` in the container's console to update it. Details: [docs/proxmox.md](docs/proxmox.md).

**2. Docker (any Linux machine).** Needs Docker with Compose, on a Linux host (not Docker Desktop on Windows or macOS). Save this as `compose.yaml` in a new folder, put your ISOs in a folder `library` next to it:

```yaml
services:
  bootlace:
    image: ghazichaieb/gola-bootlace:latest
    network_mode: host
    restart: unless-stopped
    volumes:
      - bootlace-data:/data
      - ./library:/library
volumes:
  bootlace-data:
```

Then `docker compose up -d`, and `docker compose logs bootlace | grep -i password` for the password. The page is `http://<the host's address>:8080/admin/`. Options go in the
`environment:` part as `BOOTLACE_<NAME>` (for example `BOOTLACE_IP`); to update, `docker compose pull && docker compose up -d`. Details: [docs/docker.md](docs/docker.md).

**3. Linux, as a service (a PC, a mini PC, a Raspberry Pi, a virtual machine).** The simplest way on Debian, Ubuntu, Raspberry Pi OS and Proxmox hosts is the package: download
`bootlace-<version>-linux-amd64.deb` (or `arm64`) from the [Releases page](https://github.com/ghazichaieb/gola-bootlace/releases) and `apt install ./bootlace-*.deb`
(`dnf install ./bootlace-*.rpm` on Fedora and RHEL). It installs the program, a systemd service that it starts, the folder `/srv/library` for your ISOs, and `/etc/default/bootlace` for the settings (kept when you
upgrade); `journalctl -u bootlace | grep -i password` shows the password. Or install it by hand from the archive: download `bootlace-<version>-linux-amd64.tar.gz` (or `arm64`),
then, as root (Debian or Ubuntu shown; install `wimtools` with your system's package manager otherwise):

```
apt install -y wimtools                      # only needed to start Windows with Secure Boot on
mkdir -p /opt/bootlace /srv/isos /var/lib/bootlace
tar xzf bootlace-*-linux-amd64.tar.gz --strip-components=1 -C /opt/bootlace
cat >/etc/systemd/system/bootlace.service <<'EOF'
[Unit]
Description=Gola Bootlace network boot server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/bootlace/bootlace serve -config /var/lib/bootlace/bootlace.json -iso-dir /srv/isos
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
systemctl enable --now bootlace
journalctl -u bootlace | grep -i password
```

Copy your ISOs into `/srv/isos` (or add them from the web page). If the server has several networks, add `-ip <the address the PCs reach it at>` to the `ExecStart` line. It runs as
root because it listens on low ports (67, 69, 445). To update, unpack the new archive over `/opt/bootlace` and `systemctl restart bootlace`.

**4. Windows (for a quick job, or a PC that is on for the day).** Download `bootlace-<version>-windows-amd64.zip` from the Releases page and unzip it. In a terminal in that folder:

```
bootlace.exe serve -iso-dir D:\isos
```

When Windows asks, allow it through the firewall on **private** networks. Windows' own file sharing holds port 445: if the Windows installer cannot reach its files, start with
`-smb-port 4445` (a different port works only with a recent Windows installer) or use one of the other three choices. It runs only while the window is open, so this is for jobs, not for a server that stays on. (Without Secure Boot,
`wimlib-imagex` is not needed.)

In all four, check it from a PC: restart it, choose **boot from network**, and the Bootlace menu should appear. If it does not, the web page's **Log** shows what the PC asked for.

### Good to know

- **Which systems work, and how well:** [docs/compatibility.md](docs/compatibility.md) lists every ISO tried and what happened. An ISO is marked as working only after it started in the test lab.
- **A system that is not on the list may still work.** Bootlace recognises well-known systems by their files, and for others it reads the ISO's own boot menu and looks at its
  start-up disk to see what it is made of. A few seconds after you add an ISO, the menu shows what it found.
- **The ISO's own boot menu is not shown.** Bootlace starts one way of each ISO (the ordinary one). What the ISO's own menu would offer (safe graphics, an OEM or minimal install, a
  rescue or text mode, "check the disc", persistence, memtest) is not offered yet; for now you can add extra kernel arguments for an ISO on the management page. Showing those entries
  is planned ([#35](https://github.com/ghazichaieb/gola-bootlace/issues/35)).
- **Does it change what is installed or how the system runs?** The installed system is the installer's own, and Bootlace never writes to the ISO or the PC's disks. But a system that runs from
  the network does not start exactly as it would from a disc: the network has to stay up, and a few features that need the boot stick (Tails's persistent storage, for example) are not
  there. Read [docs/what-bootlace-changes.md](docs/what-bootlace-changes.md) before relying on it for something important.
- **Secure Boot** (a firmware setting) blocks the programs Bootlace starts with. Switch it off on the PC, except for Windows and Ubuntu, which have a way through ([docs/secure-boot.md](docs/secure-boot.md)).
- **Windows ISOs** read their installation files from a small read-only file share the server runs itself (port 445). A Windows server already uses that port for its own
  sharing: use `-smb-port` or run Bootlace on a Linux machine, a container or a Proxmox host.
- **Speed:** a gigabit network is fine. An installer reads from the server as it goes, so a slow network makes a slow installation.
- **It is a tool for a local network, and answers only the local network.** Every service refuses addresses that are not private (10.x.x.x, 172.16-31.x.x, 192.168.x.x), loopback or link-local; other networks (a VPN's, for example) are added under Server in the web interface. Do not forward its ports to the internet: the network disk and iSCSI have no login, the web page is not encrypted, and the Windows share is a small server written for this project. To install a PC from outside, use a VPN to reach the local network.

---

## For developers

### How it works

```
 PC (firmware, PXE)              Bootlace server                          what it reads
 ──────────────────              ───────────────                          ─────────────
 asks for a boot file ─ DHCP ──> proxyDHCP (67, 4011)  answers only PXE requests; the
                                                      network's own DHCP gives the address
 fetches iPXE ──────── TFTP ───> TFTP (69)            ipxe.efi / undionly.kpxe (built in)
 iPXE asks for a menu ─ HTTP ──> web server (8080)    /menu?mac=...  a script made for that PC
 iPXE loads the system ─ HTTP ─> /fs/<iso>/<path>     files taken out of the ISO as they are asked for
 the system reads its ISO ─────> NBD (10809)          the ISO (or a partition of an image) as a network
                                  iSCSI (3260)        block disk / a network CD, read-only, piece by piece
 Windows Setup ──────── SMB ───> share (445)          the ISO's files, read-only
```

Everything is one Go program (`cmd/bootlace`), with the iPXE binaries and a small helper (`cmd/bootlace-nbd`, a static Linux program that runs in the PC's initramfs) built in.

1. **The menu.** For every file of the ISO folder the server decides *what it is* and writes the iPXE commands that start it (`internal/httpd`, `internal/family`). A file it does not
   know is handed to the PC as a CD or disk over iSCSI.
2. **Starting a Linux.** iPXE loads the kernel and start-up disk *from inside the ISO* (`/fs/...`, read without unpacking by the `iso9660`, `udf`, `fat` and `partition` packages),
   plus an **extra initramfs archive** that the kernel unpacks over the ISO's own. The kernel is told where the ISO is, and the start-up disk mounts it as a network block disk, so a PC
   needs no memory for the ISO.
3. **What the extra archive adds** depends on how the start-up disk is made: a stand-in for the `nfsmount` command (casper, live-boot), a handler for `netroot=nbd:` (dracut), a hook (mkinitcpio),
   a changed copy of the start-up script (Proxmox), network card drivers it lacks (read out of the ISO's own squashfs), a script run before `/init` (`rdinit=`).
4. **Starting Windows.** wimboot loads the ISO's `boot.wim` (WinPE); a small command file connects the share and runs the real `setup.exe` from it. With Secure Boot on, Windows' own boot
   manager is used instead.
5. **Systems that are not known by name.** `family.DetectGeneric` reads the ISO's boot menus (`internal/bootcfg`: isolinux, GRUB, systemd-boot), looks into the start-up disk to say what it is
   (casper, live-boot, archiso, dracut) and applies the strategy of that make, in the background so the menu is not held up.
6. **Disk images and packed files.** `.img`/`.raw` are shown as hard disks (or started from the files on their partitions when they hold a system Bootlace knows, e.g. Tails, Lakka); packed
   (`.xz`, `.gz`, `.zst`, `.bz2`) and `.zip` files are unpacked once, on request, into a cache folder.

The model is **locator → strategy → extra archive**, with four **transports** (network disk, iSCSI, HTTP, memory). It is written up, with a table of the injection techniques, the pitfalls that
shaped the code, and a recipe for adding a system, in **[docs/architecture.md](docs/architecture.md)**.

### Layout

| Path | What is in it |
|---|---|
| `cmd/bootlace` | the server; `bootlace serve`, `bootlace inspect <iso>` which says how the server would start an ISO, and `bootlace health` |
| `cmd/bootlace-nbd` | the network disk client that runs in the PC's initramfs |
| `internal/family`, `bootcfg` | recognising systems and how to start them; reading boot menus |
| `internal/initrd`, `initread` | writing the extra archive; reading initramfs files and kernel modules |
| `internal/httpd`, `admin`, `menu` | the web server, the management page, the boot menu |
| `internal/library`, `importer`, `service`, `access` | the library (folder, checks, index), uploads and downloads, starting and stopping the services, the local-network-only rule |
| `internal/nbd`, `iscsi`, `smbshare`, `tftp`, `dhcp` | the network services |
| `internal/disc`, `iso9660`, `udf`, `fat`, `partition`, `diskimg`, `isos` | reading ISOs, images and archives |
| `internal/winpe`, `handoff` | Windows set-up, and the chains that need the PC's own firmware (FreeBSD, Secure Boot) |
| `tools/` | the lab: Proxmox and Hyper-V scripts that boot ISOs in virtual machines and take pictures; the iPXE build; `release.sh` and the Proxmox container script |
| `deploy/` | the Docker Compose file and the community-scripts files for Proxmox |
| `docs/` | architecture, compatibility results, Secure Boot, Proxmox, Docker, requirements, the web interface |

### Building and testing

You need Go (see `go.mod`). Windows boot with Secure Boot needs `wimlib-imagex` on the path.

```
go build ./cmd/bootlace
go vet ./... && go test ./...
```

Two sets of tests need real files and are skipped without them: `BOOTLACE_TEST_ISODIR=<folder of ISOs> go test ./internal/family` checks that every ISO that was tried is still recognised the
same way (by name and by the generic path) and that its archive still builds, and `BOOTLACE_TEST_DISK=<an image>` reads a disk image's partitions. The lab scripts in `tools/proxmox` and
`tools/*.ps1` boot ISOs in virtual machines (`pve_run.sh` and `lab_run.ps1`), BIOS and UEFI, and keep a screenshot of each.

Build for a Linux server or container: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bootlace-linux-amd64 ./cmd/bootlace`. The helper is rebuilt with `tools/build_helper.sh`.

### Teaching it a new system

Add a function to the file of its group in `internal/family`, list it in `detectors`, test it with a fake tree and (if you have the ISO) in `real_test.go`, try it in the lab, and write
the result in `docs/compatibility.md`. [docs/architecture.md](docs/architecture.md) has the details.

---

## Licence

Gola Bootlace is free software under the **GNU General Public License, version 3 or (at your option) any later version**; see
[LICENSE](LICENSE). Additional terms about attribution and the project's name are in [ADDITIONAL-TERMS.md](ADDITIONAL-TERMS.md); the rules for the names are in [TRADEMARKS.md](TRADEMARKS.md).
Third-party code and credits are in [NOTICE](NOTICE), together with the offer of the source code of the GPL programs inside the release (it is also a file of every release).

"Gola" and "Gola Bootlace" are names of Gola Services SUARL. The licence gives you the code, not the right to use the names for a modified version.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
