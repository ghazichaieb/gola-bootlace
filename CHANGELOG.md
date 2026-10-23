# Changelog

All notable changes are listed here, newest first. The version numbers follow [Semantic Versioning](https://semver.org): while the first number is 0, the
program is not yet in its final form and any release may change how it is set up. See "Versioning" in [CONTRIBUTING.md](CONTRIBUTING.md).

## Unreleased

- **Proxmox script: no needless template download.** A Debian 12 or 13 template that is already on the host (on any storage that holds templates) is used as it is; the newest one on offer is downloaded only when there is none. (The script itself is fetched from `main`, so this applies to the next install without a new release.)

## 0.1.1 - 2026-10-23

- **Security fixes found by `govulncheck`.** The `.xz` reader is updated (v0.5.12 to v0.5.17: a damaged packed file could use up memory), and the build needs Go 1.27.3 (a fix in the certificate checks used when a file is downloaded from a web address).
- The Proxmox documentation now says plainly that the community-scripts project cannot take Bootlace until it is six months old with 600 stars (#50); `tools/proxmox/bootlace-lxc.sh` is the way to install in a container.

## 0.1.0 - 2026-10-23

The first release. What the program does is listed at the end of this section.

- **Legal and publishing.** The additional terms are now final: attribution, modified versions must say they are modified, and no rights to the names; the rules for the names are in `TRADEMARKS.md`. `NOTICE` carries a written offer of the source code of the GPL programs inside the release (iPXE, wimboot, GRUB), and every release has them as `bootlace-<version>-third-party-sources.tar`. `SECURITY.md` says how to report a problem privately.
- **Signed releases.** `SHA256SUMS` is signed (`SHA256SUMS.sig`, made with OpenSSH's `ssh-keygen -Y sign`); the public key is the file `allowed_signers`, and [docs/verifying.md](docs/verifying.md) shows how to check a download. `tools/release.sh` signs and refuses to make a release without a key.
- **The first password is easy to find.** It is made up at the first start and written once in the log; the Proxmox scripts now show it at the end of the installation, and the Docker, Proxmox and web interface documentation say where to look and how to set a new one if it is lost.
- **Docker.** A `Dockerfile` (42 MB image, Alpine, with wimlib for Windows with Secure Boot) and a real `deploy/docker-compose.yml` (host networking, a volume for the settings, a folder for the library). Every option of `bootlace serve` can be set as an environment variable `BOOTLACE_<NAME>`, and `bootlace health` is the container's health check. The server's address is now the one of the computer's default route, not the first private address (inside Docker that was the Docker bridge). (#11)
- **Release files.** `tools/release.sh` makes the archives for Linux (amd64, arm64) and Windows, a `SHA256SUMS` file and the Docker tags of a version. The archives are made by `tools/archive`, the same on every computer: owner 0 (an archive that names the builder's user cannot be unpacked by root in an unprivileged container), the program executable, one fixed time, so the same sources give the same archive. (#11)
- **Proxmox, own script rewritten.** It asks a few questions (container, network, and where the ISOs are: in the container, a host folder, or an SMB or NFS share), takes the program from the release files and checks its checksum, and writes the container's Notes with a link to gola.tn. A share is mounted on the host with an automount, so it does not matter whether the NAS starts before or after the container; the container follows it, and Bootlace waits for it. Tested on Proxmox VE 9.2, including a hard NFS mount that disappears and comes back. (#11)
- **`update` in the container.** Type `update` in the container's console: it checks the version, asks before restarting the server, verifies the download, and goes back to the old version if the new one does not answer. (#11)
- **community-scripts.** The files for the Proxmox VE Helper-Scripts (`deploy/community-scripts/`: ct, install and json), tested with their engine on Proxmox VE 9.2. (#11)
- **Library page.** Upload files, add a file from a web address, rename, move, make folders and delete, all in the web interface. A file is written to disk as it arrives and only put in place when it is whole; its first bytes are checked against its name; an optional SHA-256 or SHA-512 is verified; the same name, address, size or content is noticed before and after; nothing is ever replaced. Downloads are fetched by the server with a client that refuses local addresses at the moment it connects, resume after an interruption or a restart, and slow down while PCs are installing (a setting). Deleting is never recursive. The settings of an ISO follow it when it is renamed or moved. (#20)
- **Cache in two places.** Unpacked images and the copies of boot.wim go to the container's own small cache folder while it has room and then to `<library>/.bootlace/cache`; the size and the choice are settings on the Server page. (#20)
- **The login slides.** It lasts while something is done in the page (a transfer in progress counts, the page refreshing itself does not), ends after 30 quiet minutes, and never lasts longer than 12 hours.
- **Local network only.** Every service (web page and menu, Windows share, iSCSI, network disk, TFTP, DHCP) now refuses addresses that are not local (private ranges, loopback, link-local). More networks, or "from anywhere" with a confirmation, are set under Server in the web interface; refusals are counted there, and a public server address gives a warning. (#38)
- The ISO folder is now a **library**, chosen in the web interface (Server page, with a folder picker). It must be readable and writable and is recognised by a marker in its `.bootlace` folder: a missing network drive is never mistaken for it, and nothing is served or written then. `-iso-dir` only adds the library on the first start. (#36)
- The services PCs talk to can be started and stopped in the web interface, with a warning when PCs are connected; a port that is already taken is reported there instead of ending the program. The server stops after 30 seconds without the library and starts again by itself when it is back. (#36)
- Hidden folders and files (`.bootlace`, `._name.iso`) are never offered as ISOs.
- The Proxmox script now mounts the ISO folder read and write, and says what to do when the container cannot write to it.

What the program does:

- PXE network boot server in one program: proxy DHCP, TFTP, a web server, a network disk (NBD) server, an iSCSI target and a Windows file share, with iPXE for
  BIOS and UEFI built in.
- A boot menu made from a folder of ISO files and disk images (also packed `.xz`, `.gz`, `.zst`, `.bz2` and `.zip`); the files are never changed.
- Ubuntu, Debian, Fedora and relatives, Arch, openSUSE, Alpine, Proxmox VE, Tails, Windows (also with Secure Boot on), FreeBSD and many more start by name; other live
  ISOs are recognised from their own boot menus. Results per ISO: [docs/compatibility.md](docs/compatibility.md).
- A web interface to see the PCs, hide or rename ISOs, give a PC a rule, and read the log.
- A script that makes a Proxmox container with the server in it.
