# Gola Bootlace: how to start

> **This is the plan the project started from** (2026-10-19), kept for the reasoning. It is not how the program works today: for example, Linux ISOs are not downloaded into RAM any more but
> read over the network a piece at a time. For what was built see [architecture.md](architecture.md), and for what was tested [compatibility.md](compatibility.md).

Written 2026-10-19 from a read of Ventoy's source (https://github.com/ventoy/Ventoy, GPLv3) and its build notes.
Goal: an open source (GPL-3.0-or-later) server that PXE-boots any unmodified Windows or Linux ISO (and BSD where possible)
straight from an ISO folder. Runs in a Proxmox LXC or in Docker Compose.

## 1. What Ventoy really does (from its code)

Ventoy never modifies the ISO. It starts the ISO's own boot files and hooks in at the right moment.

**Linux** (`IMG/cpio/ventoy/`, about 60 per-family folders, `ventoy-hook-lib.sh` 1,091 lines)
1. GRUB2 starts the ISO's own kernel and initrd and appends one extra archive (a cpio) to the initrd.
2. That archive runs first. `ventoy_chain.sh` finds out which distro it is by reading `/proc/version`, `/etc/os-release`, files in the
   initrd and the kernel command line (Debian, Ubuntu, RHEL/Fedora/CentOS family `el7..el10`/`.fcNN.`, SUSE, Arch, Gentoo, Alpine, TinyCore ...).
3. The matching hook (`hook/debian`, `hook/rhel7`, `hook/arch`, `hook/suse` ...) makes the ISO appear as a block device named
   `/dev/mapper/ventoy` (device-mapper linear map over the ISO file's disk blocks, or loop devices wrapped by device-mapper) and rewrites
   what the distro's own start-up scripts look for (kernel parameters, udev rules, dracut hooks) so they find their "CD" there.
4. The distro then boots normally, believing it was started from a disc.
Everything depends on that device. On a USB stick it is a map of the ISO's blocks; in Bootlace it must be something else (see section 3).

**Windows** (`vtoyjump/vtoyjump.c`, 3,200 lines of C)
1. wimboot loads the ISO's `boot.wim` and the helper `vtoyjump` replaces the setup launcher inside it.
2. The helper finds the ISO and attaches it with the Windows virtual-disk API (`OpenVirtualDisk` / `AttachVirtualDisk`), falling back to the
   ImDisk driver on systems without ISO support. The ISO then shows up as a CD drive.
3. It runs the real `setup.exe` from it. Extras it handles: unattended install files with variable expansion, the Windows 11 hardware
   check bypass, a few vendor recovery ISOs.

**Other pieces:** a modified iPXE (`ipxe.krn`) as a BIOS chain loader, an iSCSI boot table so Windows sees a disk, `vblade` (a small
ATA-over-Ethernet server) for distros without device-mapper, `memdisk`, GRUB2 and EDK2 builds for UEFI/ARM64, `ventoy_unix.cpio` for BSD.

Licence note: the Ventoy files carry "GPL version 3 or later" headers, so Bootlace (GPL-3.0-or-later) may reuse them if the
copyright headers stay and the origin is credited in `NOTICE`.

## 2. How Bootlace differs (it is the network version)

| Part | Ventoy (USB) | Bootlace (network) |
|---|---|---|
| Boot loader | GRUB2 from the stick | iPXE (BIOS and UEFI) plus wimboot, fetched by PXE |
| Finding the ISO | blocks of a file on the stick | the server holds the ISO; clients fetch it over HTTP (or SMB for Windows) |
| Linux block device | device-mapper over the ISO's extents | the ISO is downloaded into RAM, `losetup` it, device-mapper on top, then the **same family hooks** as Ventoy |
| Windows ISO | `AttachVirtualDisk` on a path on the stick | `AttachVirtualDisk` on a network path (`\\server\isos\win11.iso`), to be proven in a spike |
| Settings | per-stick menu | per-server menu, optionally per client MAC |

## 3. Architecture

- **Server**: one Go program (single static binary), so the same file runs in an LXC and in a Docker container.
  - proxyDHCP (answers PXE clients only, leaves the existing DHCP server alone; optional full DHCP mode)
  - TFTP (serves the first iPXE file), HTTP with range requests (ISOs, scripts, kernels), small web interface and API
  - generates the iPXE menu script per client
- **Samba** in the same container, only for Windows ISOs. It is a separate GPL program, so no licence mixing.
- **Import step**: when an ISO is added, the server lists it with 7-Zip, works out the family (Windows 10/11/Server, Ubuntu, Debian, Fedora ...), finds
  kernel and initrd paths, and stores the result as a small metadata file. The ISO itself is never changed.
- **Linux shim**: a cpio built from the reused Ventoy hooks plus a fetch-the-ISO step (static BusyBox `wget` or a tiny Go downloader).
- **Windows shim**: a small helper (Go, cross-compiled to Windows, no Visual Studio needed) that takes the place of `winpeshl.exe`, as Ventoy's `vtoyjump`
  does, added by wimboot at boot time (wimboot accepts extra files and merges them into the boot file system; its source has a cpio reader for this;
  confirm in the spike), so even `boot.wim` stays untouched. Credentials for the share are generated per boot.
- **Not shipped**: any Microsoft file. Everything Windows comes from the user's own ISO.
- **Packaging**: a Debian-based LXC install script (Proxmox helper-script style) and a `docker-compose.yml` using host networking
  (broadcast traffic does not pass Docker's normal network; Linux hosts only).

## 4. Phases, each with a test that decides whether to go on

**Phase 0, setup (3 days).** New private repo, GPL-3.0-or-later LICENSE, NOTICE (Ventoy, iPXE, wimboot, Samba credits), CI skeleton, test rig.
Test rig: QEMU/KVM on a Linux box or a Proxmox node, plus our existing Hyper-V scripts (Gen 2 VMs PXE-boot from the synthetic adapter; Gen 1 needs the
legacy adapter). Exit: a script that creates an empty VM, boots it from PXE and takes a screenshot.

**Phase 1, hello PXE (1 week).** proxyDHCP + TFTP + HTTP + a hand-written iPXE menu. Exit: a BIOS VM and a UEFI VM both reach the menu and
boot Alpine's own netboot files.

**Phase 2, Windows spike (1 to 2 weeks). This is the riskiest part, do it before any user interface.**
Prove, in order: (a) wimboot boots `boot.wim` of a Windows 11 ISO with an injected `winpeshl.ini`; (b) WinPE brings up the network and reaches Samba;
(c) `AttachVirtualDisk` on `\\server\...\win11.iso` works inside WinPE and gives a CD drive; (d) `setup.exe` starts and installs to a blank disk.
Fallbacks, only if (c) fails: extract the ISO and let setup run from the share directly; then an HTTP-backed disk driver (what iVentoy does, with
a signing headache, so last). Exit: Windows 11 26H2, Windows 10 22H2 and Server 2022 official ISOs install unattended in a VM.
Known catches: WinPE has no driver for some network cards (an admin driver folder fixes it); Windows 11 24H2 and later tighten SMB
(signing, no guest) so the share needs a user and a password.

**Phase 3, Linux engine (2 to 3 weeks).** Kernel and initrd taken from the ISO over HTTP, shim appended, ISO fetched into RAM, hook applied.
Order: Ubuntu (casper), Debian live, Fedora/Rocky/Alma (dracut), Arch, openSUSE, Alpine, Mint, then the long tail. Distros that support network
boot natively (Ubuntu `url=`, Fedora `root=live:http://`) can use that to save RAM. Exit: the 20 most used desktop and server ISOs boot to their
installer or live desktop on BIOS and UEFI.

**Phase 4, product (2 weeks).** ISO import and checks, web interface, per-client rules by MAC, unattended Windows answer files (our image builder's answer
file knowledge applies here), logs, an ISO "does it boot" badge from the automated tests.

**Phase 5, packaging and reach.** LXC script, Docker Compose, documentation, QEMU test farm in CI that boots every ISO in the compatibility list,
FreeBSD/OpenBSD (check Ventoy's `ventoy_unix.cpio` and memory-disk images), UEFI Secure Boot (signed shim, enrolment), ARM64.

### Releases (later)

Each release ships in two formats: a **Docker image** (with a `docker-compose.yml`) and an **LXC template** for Proxmox. On top of that, a
**one-line install script in the style of the Proxmox community "helper scripts"**, run on the Proxmox host, that creates the container, installs
Bootlace in it, sets up the network (bridge, static address) and prints the web address. Not before the server works; tracked as a ticket.

## 5. Risks to keep in mind

- **RAM**: downloading an ISO into memory needs about the ISO's size plus the running system. A 6 GB desktop ISO wants 8 to 12 GB. Say so in the docs.
- **Secure Boot**: iPXE is not signed by Microsoft. First release: Secure Boot off, or enrol a key. Later: shim.
- **"Any ISO" is a goal, not a promise.** The README lists the ISOs that were actually booted, with the date and result.
- **Licence compliance**: keep Ventoy's copyright headers in reused files, ship source for iPXE and wimboot, keep Samba separate.
- **Existing DHCP servers**: proxyDHCP must never answer clients that are not asking for PXE.

## 6. First tickets to open when the repo exists

1. Repo, licence, NOTICE, CI.  2. PXE test rig (QEMU + Hyper-V).  3. Go skeleton: TFTP + HTTP + proxyDHCP.  4. iPXE build with embedded script
(BIOS, UEFI x64).  5. Windows spike (a) to (d).  6. ISO scanner (7-Zip listing, family detection).  7. Linux shim from Ventoy hooks.
8. Ubuntu, Fedora, Debian, Arch boot tests.  9. LXC and Compose packaging.  10. Compatibility list generated from CI.
