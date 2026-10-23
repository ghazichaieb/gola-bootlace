# Changelog

All notable changes are listed here, newest first. The version numbers follow [Semantic Versioning](https://semver.org): while the first number is 0, the
program is not yet in its final form and any release may change how it is set up. See "Versioning" in [CONTRIBUTING.md](CONTRIBUTING.md).

## Unreleased

Nothing has been released yet. What the program does so far:

- PXE network boot server in one program: proxy DHCP, TFTP, a web server, a network disk (NBD) server, an iSCSI target and a Windows file share, with iPXE for
  BIOS and UEFI built in.
- A boot menu made from a folder of ISO files and disk images (also packed `.xz`, `.gz`, `.zst`, `.bz2` and `.zip`); the files are never changed.
- Ubuntu, Debian, Fedora and relatives, Arch, openSUSE, Alpine, Proxmox VE, Tails, Windows (also with Secure Boot on), FreeBSD and many more start by name; other live
  ISOs are recognised from their own boot menus. Results per ISO: [docs/compatibility.md](docs/compatibility.md).
- A web interface to see the PCs, hide or rename ISOs, give a PC a rule, and read the log.
- A script that makes a Proxmox container with the server in it.
