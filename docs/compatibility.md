# Tested ISOs

An ISO is listed as **works** only after it booted to its installer or live desktop through Bootlace in our test rig (a Hyper-V generation 2 VM on a private
switch, UEFI with Secure Boot off, started by `tools/lab.ps1`). Nothing here is a promise that another ISO of the same distribution boots: each line is one
file we tried. BIOS (legacy) boot is covered in its own section at the end.

| ISO | Version | BIOS | UEFI | Result | Date | Notes |
|---|---|---|---|---|---|---|
| Alpine Linux virt | 3.24.2 | not tried | yes | **works** | 2026-10-21 | Kernel, modules disk and packages are read out of the ISO over HTTP; reaches the login prompt. |
| Ubuntu Desktop | 22.04.5 | not tried | yes | **works** | 2026-10-21 | The initramfs downloads the whole ISO into memory (`url=`): the PC needs RAM for the ISO and then some (12 GB for the 4.4 GB ISO; 10 GB was not enough). Reaches the desktop and the installer icon. |
| Ubuntu Desktop | 25.04 | not tried | yes | **works** | 2026-10-21 | Same method as 22.04, plus `cloud-init=disabled`: without it cloud-init grew to 10 GB while the ISO was in memory and the kernel killed it (out of memory at 14 GB and at 17 GB). Reached the installer's language page with 17 GB; the smallest amount that works was not measured. |
| CachyOS Desktop | 260426 | not tried | yes | **works** | 2026-10-21 | archiso network boot (`archiso_http_srv`), image copied to memory. Needs `ip=:::::eth0:dhcp`: with plain `ip=dhcp` the start-up script ran before the network card existed. Reaches the KDE desktop. |
| Proxmox VE | 9.1-1 | not tried | yes | **works** | 2026-10-21 | The ISO is given to the kernel as a second start-up disk named `/proxmox.iso`. The graphical installer starts (it warns that the VM has no hardware virtualisation: a property of the test VM). |
| TrueNAS SCALE | 25.10.7 | not tried | yes | **works** | 2026-10-21 | Debian live style (`fetch=` the squashfs). The installer menu appears. |
| TrueNAS SCALE | 25.04.2.5 | not tried | yes | **works** | 2026-10-21 | Same as 25.10.7: the console setup menu (Install/Upgrade) appears. |
| TrueNAS SCALE | 25.10.3.1 | not tried | yes | **works** | 2026-10-21 | Same: the console setup menu appears. |
| Debian netinst | 13.7.0 | yes | yes | **works** | 2026-10-25 | The ISO is offered to the installer as an iSCSI CD-ROM (extra archive), see "Debian and Kali netinst over the network" below. Installed to the end and booted, by hand and with a preseed file. |
| Debian netinst | 12.15.0 | not tried | yes | **works** | 2026-10-25 | Same method, an older installer (kernel 6.1): installed to the end with a preseed file; the installed system was checked on its disk. |
| Kali Linux installer (netinst) | 2026.2 | not tried | yes | **works** | 2026-10-25 | Same method, kernel 7.1.5+kali: installed to the end with a preseed file and booted to the login prompt. |
| pfSense CE | 2.7.2 | not tried | yes | **fails** | 2026-10-21 | Handed to the PC as a disk, the FreeBSD kernel cannot see the disk. Second method (menu entry "FreeBSD, experimental"): iPXE tells the server which ISO was chosen and restarts the PC; the PC's own firmware then loads the ISO's loader.efi from this server by TFTP (this part works, see internal/handoff). The loader starts and does its own DHCP, then stops: it sends no TFTP or NFS request, so it never reaches the kernel. Issue #9. |
| Windows 11 24H2 (FW11HEN, an a Windows image trimming tool build, Pro, English) | 24H2 | not tried | yes | **works** | 2026-10-21 | wimboot loads the ISO's WinPE, which maps Bootlace's built-in SMB share and runs `X:\sources\setup.exe /installfrom:Z:\<id>\sources\install.wim /unattend:...` from the ISO's folder. Installed to an 80 GB disk, rebooted from the disk into the new Windows. Setup went through the whole copy over the network (about 25 minutes in the lab). |
| Windows 11, custom-built (EN, Home+Pro, 9 GB ISO) | 26300.9711 | not tried | yes | **works** | 2026-10-21 | Same method. Setup showed both editions read over the share; Pro installed to an 80 GB disk (the whole 9 GB image copied over the network in about 12 minutes) and the new Windows reached the first-boot computer-name screen. The first attempt failed with 0x8007003B because the SMB server lost a half-read frame under the heavy copy (fixed in 046cc31). |
| Windows 11, custom-built (FR, Home+Pro) | 26300.9711 | not tried | yes | **partial** | 2026-10-21 | Same method. French Setup starts from the share; not installed to the end (the English build of the same program was). |
| Windows 11 FW11PFR (custom-trimmed build, Pro, French) | 24H2 | not tried | yes | **partial** | 2026-10-21 | Same method. French Setup starts and reaches the disk page; the install itself was not run to the end (it is the same path as FW11HEN). |
| Windows Server 2025 (Standard/Datacenter, Jan 2025 media) | 2025 | not tried | yes | **partial** | 2026-10-21 | Same method. Windows Server Setup starts, and lists its four editions read out of install.wim over the share; not installed to the end. |
| Windows 11 FW11HFR / FW11PEN, custom-built Windows 11 -sb2023 builds on Secure Boot hardware | | | | **untested** | | Same method; not run yet (the lab has Secure Boot off). |

Result values: **works**, **partial** (boots but a step needs help; see notes), **fails**, **untested**.

## Real PCs

| PC | Mode | What happened |
|---|---|---|
| ASUS P8Z77-V LX (Intel Z77, Realtek 8111E/F, 2012 AMI firmware), wired, router as the DHCP server, Bootlace 0.1.1 in a Proxmox container (2026-10-23) | **BIOS (legacy)**: Realtek PXE option ROM enabled, "UEFI/Legacy Boot" set to "Enable both" | **Works.** The menu appears and a custom-built English Windows 11 ISO (built for the 2023 Secure Boot certificates) starts. Secure Boot was set to "enabled" in the firmware, which has no effect on a BIOS-mode boot (it only applies to UEFI). |
| the same PC | **UEFI** ("Network Stack" enabled in Advanced) | **Does not work:** the PC asks four times, Bootlace answers each time, the PC never takes an offer and falls back to its disk, although it gets an address from the same router in Windows, Proxmox and Ubuntu. Cause not known yet: [#51](https://github.com/ghazichaieb/gola-bootlace/issues/51). Workaround: use BIOS mode. |
| an MSI PC, wired, UEFI with **Secure Boot on**, proxy DHCP, Bootlace 0.5.0-rc.1 in a Proxmox container (2026-10-27) | **UEFI, Secure Boot on** | **Works with the Secure Boot route** (the iPXE project's signed shim and `ipxe.efi`, `-secure-boot-all`; [secure-boot.md](secure-boot.md)): the menu comes up, and Ubuntu Server and Ubuntu 25.04 booted; a Windows 11 ISO picked from the menu installed to the end and Windows started. Without the route the PC stops with `Security Violation` (plain `ipxe.efi` offered). These files are now built into the program (the same ones, byte for byte). |

Not yet seen on a real PC: Windows and Ubuntu through the firmware's own boot manager with Secure Boot on (the `native:` rules), and the built-in Secure Boot route itself (the real-PC run above used the same files from a folder, before they were built into the program).

## BIOS (legacy) boot, 2026-10-22

Tested in a Proxmox VM (SeaBIOS, 6 GB) started from the network; the menu, with the logo picture, comes up through iPXE's VESA console.

- **Windows Setup environment (wimboot) now starts on BIOS** (Windows Server 2025 and FW11HEN: WinPE reaches the Bootlace command window). It failed with `Bad CPIO magic`
  because on a BIOS PC wimboot reads each file's name from the image's command line, and the script only gave `--name` (which UEFI uses). Every `initrd` line now has both:
  `initrd --name boot.wim <address> boot.wim`.
- A QEMU VM must not use the default `kvm64` CPU: bootmgr restarts the VM the moment wimboot hands over. Use CPU type `host` (or `x86-64-v2-AES`).
- The PC needs a network card WinPE has a driver for (the VM's `virtio`, `e1000` and `e1000e` cards have none: "installation files could not be reached", ipconfig lists no adapter). So
  the install itself was not run through on BIOS; the network and share part is the same as on UEFI, where it works.
- iPXE's old BIOS file (from boot.ipxe.org, 2019) has no `console`, `cpair` or PNG support. Bootlace now ships its own build (see NOTICE and tools/ipxe).

## ISOs that are not started piece by piece: iSCSI (2026-10-22)

An ISO that Bootlace does not recognise is handed to the PC as a **CD-ROM over iSCSI** (`internal/iscsi`, port 3260, `-iscsi-port`; 0 turns it off and brings back the old way,
where iPXE downloads the whole file into the PC's memory). iPXE's `sanboot iscsi:<server>::3260:0:iqn.2026-10.org.gola:bootlace:<id>` reads it a block at a time. Tested in the
Hyper-V lab (UEFI, Secure Boot off) with the Ubuntu 22.04.5 ISO forced through it from the iPXE shell: login, the ISO's own GRUB menu came up, and GRUB read the kernel and
initramfs through the iSCSI disk (the connection is closed when idle and opened again on demand). What it cannot do: a kernel that wants the disk after it has started (Linux
without an iSCSI initiator in its initramfs, Windows) cannot see an iPXE SAN disk, which is why recognised ISOs have their own ways (Ubuntu: network block disk; Windows: file share).
It suits what boots and then runs from memory or only needs its loader's disk: rescue and test tools, wipers, firmware updaters.

### Run C and the Windows tests (2026-10-22, Hyper-V lab, UEFI, Secure Boot off)

- Working: CachyOS (4 GB VM, network block disk), Debian 13.7 and Kali 2026.2 netinst (at that time the installer started and showed its language page but could not complete an install, #59; it can since 2026-10-25, see the section on it below), SystemRescue (a graphical session via `startx` works).
- **ShredOS** needs more memory than it looks: its kernel is 352 MB with the whole system inside, iPXE holds it and the kernel unpacks it. With 2 GB nothing appears on screen
  (no message at all); with 4 GB it boots to nwipe. There is no streaming alternative (one file), so the menu cannot do better than "needs about 4 GB".
- **Hiren's BootCD PE** (a Windows PE with no installation image): through wimboot it failed with `0xc0000017 not enough memory to create a ramdisk` even with 4 GB, because the
  1.8 GB `boot.wim` is held twice (wimboot's copy and Windows' RAM disk). Started as an iSCSI CD-ROM it boots to the desktop with 4 GB. Bootlace now does that for a Windows PE
  with no `install.wim` when the iSCSI server is on (wimboot remains the fallback).
- **Windows installer through iSCSI** (tried with Server 2025): the ISO's boot sector asks "Press any key to boot from CD or DVD" (an ISO without that prompt, such as our image builder's, skips it);
  Setup started from the CD but then offered only "Install driver to show hardware", with no drives and no installation image. Windows cannot use an iPXE SAN CD once it has taken over from the firmware,
  so Windows installers stay on the file share.
- **memtest / grub-memtest** start through iSCSI in a second (read block by block, no RAM disk). Not a substitute for a proper test: the iPXE program that started it and the
  firmware's reserved memory stay untested, so use a USB boot for a thorough or intermittent-fault test. It stays in the menu as a quick check.
- **Fedora 44 Workstation Live** (new KIWI layout: kernel in `/boot/x86_64/loader`, `/LiveOS/squashfs.img`): boots to the GNOME desktop with 4 GB. Its dracut initramfs has an iSCSI
  client, so the kernel gets `netroot=iscsi:<host>::3260::<iqn> root=live:/dev/disk/by-path/ip-<host>:3260-iscsi-<iqn>-lun-0` and reads the system from the iSCSI CD a piece at a time.
  This needed the iSCSI target to answer a discovery session (`SendTargets`), which dracut's `iscsiadm` does first. Before that Fedora was started as a plain CD and never found its media.
- TrueNAS SCALE 25.10.7 (Debian live): installer menu appears. pfSense 2.7.2: stops at the FreeBSD loader (issue #9, as before).
- **openSUSE Tumbleweed (DVD and NET)**: the kernel starts but `linuxrc`'s DHCP gets "no/incomplete answer" from the lab server, which gives out 169.254.x.x addresses (Windows' automatic
  address on the lab switch); suspected lab artefact (link-local offers are probably refused). To confirm on a normal address range.
- **Proxmox test VM (real LAN, router DHCP)**: openSUSE Tumbleweed NET starts YaST and the DVD reaches the installer's Welcome page, with the repository and RPMs served from Bootlace
  over HTTP, so the lab failure was the 169.254.x.x addresses. Proxmox's OVMF has no network boot (no PXE entry for any card), so for UEFI the VM boots Bootlace's iPXE from a 64 MB EFI disk
  (EFI/BOOT/BOOTX64.EFI) and the rest is the normal path. pfSense on BIOS used to restart the PC for ever (its firmware hand-off has no boot file for BIOS): a BIOS PC now gets a message and goes back.
- **More results (2026-10-22, Hyper-V UEFI lab, Secure Boot off)**: Ubuntu 22.04.5 desktop (3 GB, desktop), Ubuntu 25.04 desktop (3 GB, installer; an apport crash dialog shows, harmless),
  TrueNAS SCALE 25.04.2.5, 25.10.3.1 and 25.10.7 (installer menu), Kali full installer, Windows Server 2025, a custom-built Windows 11 ISO and FW11HEN (Setup language page over the SMB share), Proxmox VE 9.1
  (installer EULA on the Proxmox test VM with 8 GB; it still loads the whole ISO into memory, streaming it is planned).
- **Proxmox VE streaming (2026-10-22)**: the installer now reads its ISO over the network disk instead of holding the whole 1.7 GB ISO in memory (it needed 8 GB before; the installer reaches
  its EULA page on a 4 GB VM). `internal/pve` builds an extra initramfs archive per ISO: a copy of the installer's own `/init` with lines added that bring up the network (`udhcpc`) and attach the ISO as
  `/dev/nbd0` (and the script's own search for the ISO also looks at `nbd0`), the network card drivers the start-up disk lacks (the common wired cards, taken from the kernel modules in
  `pve-installer.squashfs` on the ISO, matched to the kernel's version) and a small `insmod` (a subcommand of `bootlace-nbd`, the start-up disk has none). The installer's files are untouched; if the script's
  form is not the expected one the old way (whole ISO) is used. Not yet checked: an install run through to the end (the installer's own network setup comes late), BIOS vs UEFI beyond the BIOS VM.

## Disk images (2026-10-22)

`.img` and `.raw` files in the ISO folder, and those packed as `.gz`, `.xz`, `.zst` or `.bz2`, are listed like ISOs ("Disk image") and started as a **hard disk** over iSCSI: the iSCSI target shows
them with 512-byte blocks and device type "direct access" (an ISO is a CD-ROM, 2048-byte blocks) and iPXE's `sanboot` boots from that disk, so the image's own boot loader (MBR or EFI system partition) starts.
A packed image is unpacked once, in the background, into `<data folder>/cache/images` (the menu line says "unpacking 43 %" until it is ready; it needs as much free space as the unpacked image, e.g. 8 GB for a Batocera image).
Tested in the lab with a small UEFI disk image, plain and gzipped: the firmware booted its loader from the SAN disk (`Booting from SAN device 0x80`).
Limit, as for every "as a disk" start: once the operating system's own kernel runs, the firmware's SAN disk is gone, so an image whose system looks for its disk again after the loader (most Linux images
that mount their root from a partition) works only if its initramfs can log in to iSCSI or has been given a network disk, which is per-image work like the ISO families. Not handled: VHD, VHDX, qcow2 and VMDK.
- **BIOS (legacy) on the Proxmox test server, 2026-10-22** (`tools/proxmox/pve_run.sh`: throw-away SeaBIOS VMs with an e1000e card on the real LAN, the Bootlace container hands out the boot; much faster than Hyper-V's BIOS VMs):
  Ubuntu 22.04.5 desktop (3 GB, desktop), Debian 13.7 live GNOME (3 GB, desktop), Fedora 44 Workstation Live (4 GB, over iSCSI, desktop), CachyOS (3 GB, desktop), Linux Mint 22.3 (3 GB, desktop), Kali netinst (2 GB, installer).

## 2026-10-22 night: a real LAN showed what the lab hid (Proxmox test server, router DHCP)

A live system that starts its own network manager after the boot asks the DHCP server again, as a new client, and a real router may give it another address than the one the
start-up stage had. The open connection to the network disk (NBD) then goes nowhere and every read fails with EIO. The lab's own DHCP server always gave the same address per MAC,
so none of this showed in Hyper-V. Measured: the VM's MAC answered at 192.168.1.229 while the connection was still from .228.
- **Ubuntu Server 26.04** (netplan + systemd-networkd): fixed in two parts. The start-up stand-in `nfsmount` rewrites the netplan file that casper hands to cloud-init into a fixed address
  (the file already had routes and name servers: adding them twice made it invalid, and netplan then ignored it), and `cloud-init=disabled` is no longer passed for server ISOs
  (with cloud-init off the live system got a plain "DHCP on every card" and took another address). Reaches the installer's language screen.
- **Arch Linux (official ISO)**: the start-up disk's own `nbd-client -persist` has no request timeout, so a lost connection was noticed after many minutes. `bootlace-nbd` now speaks the
  kernel's netlink interface (request timeout 30 s, requests wait up to an hour, a lost connection is made again and handed to the kernel) and is also shipped as `usr/bin/nbd-client`,
  which replaces the start-up disk's client for archiso ISOs. Reaches the root prompt. Ubuntu/Mint/Debian and Proxmox use the same helper and still work.
- **Fedora 44, Nobara 44, Bazzite** (dracut live ISOs): Nobara and Bazzite were started with the whole ISO downloaded into memory (7.6 and 8.4 GB). All three now read the ISO as a network block disk: the
  kernel gets `netroot=nbd:<host>:<port>:<id> root=live:/dev/nbd0` and the extra archive carries a dracut handler (`sbin/nbdroot`) that attaches it with `bootlace-nbd` (which reconnects if the live system
  takes another address). An iSCSI login was tried first and works for Fedora 44 and Nobara, but Bazzite's start-up disk has no iSCSI client (`No handler for netroot type 'iscsi'`, then dracut powers the PC off).
  Bazzite also needs `enforcing=0` (its own boot menu passes it; with SELinux enforcing its systemd stops at the first label). Result: Fedora 44 desktop, Nobara installer ("no partitions": the VM has no disk),
  Bazzite desktop (its portal says it does not support legacy BIOS: the ISO is UEFI-only and the test VM was BIOS). One trap met on the way: an archive entry for `usr/sbin` replaced Fedora's `/usr/sbin` link with
  an empty folder and NetworkManager could not start; the handler is now `sbin/nbdroot` without a directory entry.
- **Extra kernel arguments per ISO** (web interface, ISO list; API field `args`): added for this kind of fault finding (serial console, `rd.debug`, a setting one ISO needs). The same arguments and switches can also be set for one PC (button *Kernel options* on the PCs page), for example `nomodeset` for a PC whose graphics card the live system's open-source driver does not handle; they follow the ISO's own. *Verbose start-up* adds the debug words of the kind of system (`rd.debug` for dracut, `systemd.show_status=1` for systemd live systems, `debug_init` for Alpine). The separate *debug shell* switch stops the start-up at a shell, with a word that was checked on a real VM for each kind: `proxdebug` for Proxmox VE (the installer's own debug shell, `exit` goes on), `rd.break=pre-mount` for dracut systems (Fedora 44 live: "Break before pre-mount", dracut emergency shell) and `break=premount` for initramfs-tools systems (Debian 13 live and Ubuntu 22.04: a BusyBox `(initramfs)` prompt); other kinds have no such word and ignore the switch.
- **AlmaLinux 10 and Bazzite** do not start on the Proxmox test host: its CPU (Core i7-2700K) has no AVX2, which AlmaLinux 10 (x86-64-v3) needs; tested on Hyper-V instead.
- Alpine standard (BIOS, e1000e card): the initramfs found no network card (`ERROR: IP requested but no network interface was found`): its card drivers come from the modloop, which needs the network.
- Packed disk images are no longer unpacked when the list is read but when someone starts them, one at a time (three at once, with a 512 MB container, got the server killed by the out-of-memory killer
  and wiped the rules); the container now has 4 GB and 4 cores.

## 2026-10-22 night, second batch (Proxmox test server, BIOS unless said)

Recognised from their own files and tested:
- **Pop!_OS 24.04**: casper, but its live system is in a folder called `casper_pop-os_24.04_..._debug_681` (kernel `vmlinuz.efi`), not `/casper`. The Ubuntu detector now looks for any `casper*` folder and tells casper
  with `live-media-path=`. Reaches the installer's language screen.
- **Gentoo minimal install CD and LiveGUI**: dracut live ISOs (`/boot/gentoo`, `/boot/gentoo.igz`, `/image.squashfs`), started like Fedora's: network block disk, `root=live:/dev/nbd0`, plus their own `rd.live.dir=/
  rd.live.squashimg=image.squashfs cdroot`. Root prompt and a KDE desktop.
- **Tiny Core (Core)**: kernel and a 20 MB initramfs started directly (`/boot/vmlinuz`, `/boot/core.gz`); prompt.
- **VyOS rolling**: live-boot, but its start-up disk has no `nbd` driver (the network disk could not be attached: "open /dev/nbd0: no such file"). It now downloads only its system image (`filesystem.squashfs`,
  a few hundred MB, not the whole ISO) with `fetch=`, and passes the options its own boot menu does (`noautologin ... union=overlay`): without them the console logged in as a user that does not exist, in a loop.
  Login prompt.
- **Super Grub2 Disk** (CD), **netboot.xyz** (ISO and disk image): need nothing, start as a CD/disk and show their menus.
- **OpenBSD 8.0 install image** (`install80.img`, disk image): reaches the installer ("(I)nstall, (U)pgrade, (A)utoinstall or (S)hell?"): its boot loader and ramdisk kernel run from the firmware disk, the sets come over HTTP.

Not working, and why (each is per-distribution work, none is a Bootlace fault):
- **memtest.iso in BIOS mode**: iPXE refuses the CD ("Boot from SAN device 0x80 failed: Operation not supported"): the image uses an El Torito emulation mode that iPXE does not support. It works in UEFI (Hyper-V).
- **Recalbox and the SteamOS recovery image**: see "Disk images with a Linux on them" below.
- **Garuda, NixOS, Haiku, Qubes**: see "The last of the unusual ISOs" below.
- Not offered: qcow2 (Rocky GenericCloud: a cloud image that needs cloud-init and a converter) and `.tar.gz` (Alpine netboot: a kernel, initramfs and modloop that fetch their packages from a mirror on the Internet). Both need a decision on whether they are worth it.

## Disk images with a Linux on them, and kernels inside an ISO's EFI image (2026-10-22, late)

Bootlace now reads partition tables (MBR with logical partitions, GPT) and FAT12/16/32 file systems (long names), so the kernel and initramfs can be fetched from *inside* a disk image, and the partition
is offered as a network block disk (named `<id>@<partition>`, 512-byte blocks: a FAT file system cannot be mounted on the 2048-byte blocks of an ISO). Tested on the Proxmox test server (BIOS):

| Image | What was wrong | How it is fixed | Result |
|---|---|---|---|
| **Tails 7.15** (disk image) | It was started "as a disk": the kernel could not find the stick it came from. Then, started from the files, its initramfs has **no network card drivers at all** (it is made for a USB stick): "Waiting for ethernet card(s) up". | Kernel and initramfs come from the FAT partition; live-boot is told `netboot=nfs`, which Bootlace's `nfsmount` stand-in answers by attaching the partition (and mounting it as vfat). The common network drivers are read out of Tails's own system image (`filesystem.squashfs`), unpacked, and loaded by a script added to the initramfs's `init-top` (its `ORDER` file is rewritten to run it first). | Reaches "Welcome to Tails!" |
| **Lakka 6.1** (disk image) | Started "as a disk": "Could not mount UUID=2102-5121". | LibreELEC's init takes `boot=NBD=host:port`, and the kernel has the network disk, the network cards and DHCP built in. Its start-up shell runs BusyBox's own `nbd-client` applet (an old client that knows no export names) even when another file of that name exists, so an `rdinit=` script rewrites that one call to name Bootlace's helper, which looks up the export name in a list in the archive. Storage is in memory (no `disk=`). | System starts ("Lakka (official): 6.1", its splash). RetroArch fails to start in the VM: the same happens when the image is booted from a local disk in the same VM, so it is the VM's missing GPU, not the network start. Needs a real PC to be seen. |
| **ChimeraOS 2025.02** (ISO) | The kernel is not in the ISO's files but in the EFI image inside it, and its initramfs has the archiso hooks for a disc but none for the network, and no network drivers. | The ISO's own partition table is read to find the EFI image and the kernel in it; the extra archive adds an initramfs hook (before archiso's) that loads the network drivers (read from `airootfs.sfs`), gets an address with `udhcpc` and attaches the ISO as `/dev/nbd0`, named by `archisodevice=`. Its list of hooks (`/config`) is rewritten to include it; mkinitcpio runs only executable hooks. | Reaches ChimeraOS's installer (it complains that `nbd0` is too small to install onto: the VM has no disk). |
| **Recalbox** (disk image) | Its kernel has neither the network disk (`nbd`) nor an iSCSI client, and its init needs a writable boot partition. | Nothing can be done from the network side: the kernel cannot read a disk from the network. Write it to a USB stick or disk. | Not supported. |
| **SteamOS recovery** (Steam Deck image) | A UEFI-only image for the Deck: its own boot loader (`steamcl.efi`) starts a kernel whose root is on the local disk (btrfs). | Same limit as Recalbox. | Not supported. |

Also changed: the Proxmox installer's and Tails's initramfs reading and driver collection is one package (`initread`), which reads xz, gzip and zstd modules; the archives that take seconds to make are kept in memory
(the last three).

Regression run after these changes (BIOS, Proxmox test server): Debian live (GNOME), Linux Mint, Arch Linux, Fedora Workstation, Proxmox VE and Ubuntu Server 26.04 all start as before.

## Packed files, zip archives and Alpine's network card (2026-10-23)

An ISO or disk image can now be packed (`.iso.bz2`, `.img.xz` ...) or inside a `.zip` archive (the largest `.iso`/`.img`/`.raw` in it is offered). It shows in the menu as "[ISO, packed, unpacks when started]";
choosing it asks the server to unpack it once into the cache folder (one at a time; the menu shows the progress); after that it is an ordinary ISO or disk image and starts like any other. (The Windows
set-up share does not unpack: a packed Windows ISO must be unpacked by hand.)

| File | What was done | Result (Proxmox test server) |
|---|---|---|
| **FD14-LiveCD.zip** (FreeDOS) | ISO inside a zip: unpacked, offered as a CD | Reaches FreeDOS's prompt (`D:\>`) |
| **FD14-FullUSB.zip** (FreeDOS) | disk image inside a zip: unpacked, offered as a hard disk | Reaches the installer's language screen |
| **supergrub2 USB `.img.zip`** | same | Super Grub2's menu |
| **ReactOS 0.4.16 zip** | ISO inside a zip: unpacked and offered as a CD | **Does not start** in BIOS mode: ReactOS's CD boot sector stops with "Unable to load second stage loader". The same ISO boots from a virtual CD in the same VM, and the message is the same over iSCSI (as drive 0x80 or 0xE0) and over HTTP (whole ISO in memory), so it is the way its boot sector reads the disc that iPXE's emulated CD does not satisfy. Would need a memdisk-style loader. |
| **OPNsense 26.7 `.iso.bz2`** | unpacked (2.3 GB) and recognised as FreeBSD-based | Same state as pfSense: the PC is restarted so that its firmware loads the FreeBSD loader over the network, which the test server's virtual firmware cannot do (issue #9: needs a real PC) |
| **Alpine standard 3.24** | Its start-up disk had no driver for the network card ("IP requested but no network interface was found"): the drivers are in the modloop, which needs the network. The common wired drivers are read out of the modloop and brought in the extra archive; an `rdinit=` script loads them with Bootlace's helper and then runs Alpine's own start-up script. (Adding them to the initramfs's `modules.dep` does not work: its `modprobe` reads a binary index.) | Reaches the login prompt, with the 293 MB modloop fetched over the network |

Also fixed: the line that tells a BIOS PC that FreeBSD-based ISOs need UEFI (`iseq ${platform} pcbios && ... && exit`) was made a failing chain on UEFI PCs by the menu's added `|| goto failed`, which
sent every UEFI PC to the failure screen for pfSense and OPNsense; it now ends with `|| echo`. A test now covers it.

## The last of the unusual ISOs (2026-10-23)

| ISO | What was done | Result (Proxmox test server) |
|---|---|---|
| **Porteus KDE 5.1** | Its start-up script finds its files on a disk, over NFS (a fixed layout of its own) or in an ISO file named by `from=`. The ISO is delivered as an initrd file at `/mnt/bootlace/porteus.iso` (the script looks in the folders under `/mnt`) and started with `from=/porteus.iso nomagic base_only norootcopy`. iPXE does not create folders in the initrd, and a file whose folder is missing is dropped without a message, so a tiny extra archive makes `/mnt/bootlace`. | Reaches the KDE desktop. The PC needs memory for the whole ISO twice over (about 1.3 GB plus the system): this is the one family here that is not read piece by piece. |
| **Haiku R1/beta6 anyboot** | Nothing works yet. In BIOS mode iPXE stops with "Operation not supported": the ISO boots through an *emulated floppy* (El Torito), which iPXE cannot do. Offered as a hard disk instead (it has a partition table) the same message comes back, and the cause was not found in iPXE's source (the real MBR error is hidden behind the El Torito one); memtest.iso behaves the same, so that experiment was removed again. | BIOS: does not start. UEFI: the PC reads the disc and then stays black (the loader runs, but nothing could be seen in the VM). Needs a real PC to judge. |
| **Garuda Sway** | Manjaro's `miso`, with a systemd initramfs: no BusyBox, no network tools, no hook system like archiso's. It would need systemd units in the initramfs and a DHCP client of its own (the kernel's `ip=` only works for built-in card drivers). | Not started. |
| **NixOS 26.05 graphical** | Kernel and initrd are in `/boot/nix/store/<hash>-.../` (found from the boot menu text) and its stage 1 looks for the ISO by label; there is no network card driver or network disk in it, and the squashfs `/nix-store.squashfs` holds the modules. Same kind of work as Garuda. | Not started. |
| **Qubes R4.3.2** | Needs Xen. | Not started. |

A DHCP client inside the helper (so that a start-up disk without BusyBox or `ip=` support can get an address) would be the shared piece for Garuda and NixOS: say if they are worth it.

## Systems that are not known by name: the generic recognition (2026-10-23)

An ISO that no family is known by is recognised from its own boot menu and from what its start-up disk is made of (see [architecture.md](architecture.md)). It was tried the only way
possible with the ISOs at hand, which are all known by name: with the recognition by name switched off for them (`BOOTLACE_GENERIC_ONLY`), so that the generic path alone chose how to
start each one. All eleven were recognised as the right make **and started** (BIOS, Proxmox test server):

| ISO | Recognised as | Result |
|---|---|---|
| Linux Mint 22.3 | Ubuntu-style (casper) | desktop |
| Ubuntu Server 26.04 | Ubuntu-style (casper) | installer's language page |
| Pop!_OS 24.04 | Ubuntu-style (casper), its live folder read from the menu | installer's language page |
| Debian live 13 (GNOME) | Debian-style live | welcome screen |
| Tails 7.15 (disk image) | Debian-style live, its own arguments from its GRUB menu, network card drivers brought | greeter |
| Clonezilla Live | Debian-style live | language page |
| VyOS | Debian-style live (no `nbd` driver in its start-up disk: only the system image is fetched, its own options kept) | login prompt |
| TrueNAS SCALE 25.10.7 | Debian-style live (kernel at the top of the ISO: system image fetched) | console setup menu |
| Arch Linux | Arch-style (archiso, with the network hooks) | root prompt |
| ChimeraOS | Arch-style (archiso without network hooks: a hook and drivers of its own) | installer |
| Fedora Workstation 44 | dracut live (network disk handler present) | welcome screen |

Along the way the generic reading of menus found and fixed: tabs between `linux` and its path (Ubuntu's GRUB menu), a kernel called `vmlinuz.efi` (Pop!_OS), menus in `/EFI/<name>/grub.cfg` (Tails), and
`filesystem.packages` taken for a system image (VyOS). Not covered: start-up disks of other makes (Manjaro's and Garuda's `miso`, NixOS, Void, Slax and the like): they stay "as a disk".

After the reorganisation of the code, Tails, Linux Mint, Debian live, Arch Linux, Fedora Workstation, Ubuntu Server and VyOS were started again by name (BIOS, Proxmox test server) and all reach the screens in
the tables above; Alpine, Proxmox VE, ChimeraOS and Lakka were started again after the first stage of the reorganisation, with the same results as before.


## Windows Setup in a Proxmox VM (2026-10-24)

Windows 11 (a custom-built English ISO, build 26300.9711, 7.9 GB) started from the network in a UEFI Proxmox VM (OVMF, 6 GB, a 64 MB EFI disk with Bootlace's `ipxe.efi`) and **reached the first page of Windows Setup** (language, time and keyboard) with the **Intel e1000 network card** (`PVE_NIC=e1000` for `tools/proxmox/pve_run.sh`). The card matters: Setup's own environment has no driver for QEMU's e1000e (the earlier "installation files could not be reached" in these VMs), and Proxmox's virtio card needs a driver that is not in Setup either. Not run to the end of an installation here (the Hyper-V runs above did that). A new UEFI VM also waits at its firmware's boot manager for a key: `pve_run.sh` now presses Enter twice, 80 and 120 seconds after the start.

## Unattended installs (2026-10-25, Proxmox VM, UEFI)

- **Ubuntu Server 26.04.1 live server**, autoinstall file attached to the ISO (`autoinstall ds=nocloud-net;s=http://server/answers/<id>/<MAC>/`): cloud-init fetched `meta-data`, `user-data` and `vendor-data`,
  subiquity read the file and installed onto the second disk by itself, and the installed system booted as `pc-980101` (the `hostname` word of the file). Two things were found on the way and fixed: the file's
  user name must not be one Ubuntu reserves (`admin`: the template now says `ubuntu`), and **over the network disk subiquity's disk probe fails** ("Invalid dep_id (disk-nbd0) not in storage config"), so Ubuntu
  Server ISOs are now downloaded whole into memory (the VM had 9 GB for the 2.7 GB ISO). With the network disk on and a server ISO, the menu now picks that way by itself.
- **Narrowed access to the file (#60, same VM test, 2026-10-25):** with the gate on (a file only to a MAC that asked for the menu in the last two hours, twelve files per menu request) the autoinstall ran to the end again
  and the system booted as `pc-600001`. The first attempt required the same network address as the menu request and **was refused to the PC it was meant for**: iPXE had 192.168.8.154, the installer (which identifies
  itself to DHCP with a DUID, iPXE with the MAC) was given 192.168.8.155 by the router. That condition is therefore a switch ("Stricter" on the Unattended page), off by default. A fetch with a MAC that never asked for a
  menu is refused (404), the thirteenth file after one menu request is refused, and a new menu request opens the address again (checked with curl against the running server).
- **Debian 13.7 netinst**, preseed attached: stopped at the installer's first step (it needs a CD), see the next section: fixed 2026-10-25 ([#59](https://github.com/ghazichaieb/gola-bootlace/issues/59)), and the preseed is then read like any other.
- Kickstart (`inst.ks=`) and AutoYaST (`autoyast=`): the arguments and the served files were checked by the tests only.

## Debian and Kali netinst over the network (2026-10-25, Proxmox VMs; #59)

**What was wrong.** The installer of a netinst ISO (`install.amd/gtk/initrd.gz`) is the CD flavour: its start-up disk holds `cdrom-detect` but no network card drivers and no iSCSI client, and it
loads even `netcfg` from the CD, so it cannot take a step without a device the kernel calls a CD. Bootlace handed the ISO over as a network block disk, which it does not look at.

**What was built.** Every ISO is already offered as an iSCSI CD-ROM. For a Debian installer the start-up archive (`/x/<id>/extra.cpio`, made on first request and kept, about 10 MB) adds, taken from the
ISO's own pool (`dists/*/main/debian-installer/binary-amd64/Packages.gz`, then the `.udeb` files): the network card drivers (`nic-modules`, `nic-shared-modules`, `nic-usb-modules` of the **kernel version read
from the start-up disk**), `iscsistart` and `libopeniscsiusr` (libraries it needs that the start-up disk lacks are looked for in the packages `open-iscsi-udeb` depends on; none was missing), and a script
`S12bootlace-iscsi` in the installer's start-up folder. The script loads the drivers, gets an address with the start-up disk's `udhcpc`, logs in to the CD-ROM target and waits for `/dev/sr0`; then the
installer's own "Detect and mount installation media" mounts it like a disc. Nothing in the code names a Debian version; the layouts of an old (`/lib`, `/sbin`) and a usr-merged start-up disk are both handled
(unit tests with fake ISOs of three kernel versions, among them one that does not exist yet).

**What was run (UEFI VMs on the Proxmox test server, the private test instance of the code in this change):**

| ISO | Installer kernel | How | Result |
|---|---|---|---|
| Debian 13.7.0 netinst | 6.12.107+deb13 | by hand: language, network, partitioning, base system from the CD, mirror, GRUB | "Installation complete"; the installed system booted to its login prompt |
| Debian 13.7.0 netinst | 6.12.107+deb13 | preseed file attached (Unattended page), 20 GB disk | no question asked; installed and booted to its login prompt |
| Debian 12.15.0 netinst (an older installer) | 6.1.0-50 | preseed file attached | installed; the disk holds the system (user from the file, GRUB on the ESP, kernels 6.1.0-50 and -54) |
| Debian 13.7.0 netinst, **BIOS** (SeaBIOS VM) | 6.12.107+deb13 | preseed file attached, started from an iPXE CD | installed; the installed system booted to its login prompt |
| Kali 2026.2 installer netinst | 7.1.5+kali | preseed file with Kali's mirror | installed; booted to `kali login:` |

The installer took the answer file from `preseed/url` as for any installer, so the answer files of the Unattended page work with these ISOs. Two things the runs showed: the template's user name `admin` is
reserved by the installer ([#63](https://github.com/ghazichaieb/gola-bootlace/issues/63), fixed: `localadmin`), and the `hostname` word has no effect on a netinst install because the network is configured before
the file is read ([#64](https://github.com/ghazichaieb/gola-bootlace/issues/64)).

**Memory.** The graphical installer ran with 3 GB and 1 GB. With 768 MB it switched to its "Low memory mode" (text) and reached partitioning. With 512 MB the screen filled with
`touch: /var/run/brltty-Xorg: No such file or directory` and it did not get further (not compared with an installer on a real CD). The ISO is not held in memory: it is read over iSCSI a piece at a time.

**What does not work or was not tried.** The iSCSI server must be on (it is by default): without it the menu entry says that the installer cannot install, and the installer stops at the first step as before.
A server restart while an install runs cuts the CD (the initiator gives up after 120 s). The network cards are the ones in the ISO's `nic-*-modules` packages (virtio-net and e1000e were used here); a PC with a
card those packages do not carry cannot reach the CD (the log of the start-up script is `/tmp/bootlace-iscsi.log`, shown on the installer's second console). BIOS was run to the end for Debian 13.7 (the VM started iPXE from a CD image; that CD had to be taken out once the kernel was loaded, because the installer then also looks at it as a second CD and its "configure apt from the media" step stops at a disc that is not a Debian disc: a property of that test setup).

## Windows 11 Setup checks, answer files and drivers (2026-10-25, Hyper-V gen 2, Windows 11 26H2 build 26300.9457)

Run on the vanilla `Windows11_Client_x64_en-us_26300_9457.iso` through a lab copy of the server, in a Hyper-V generation 2 VM with **no virtual TPM, 2 GB of memory and a 40 GB disk** (Secure Boot off,
because iPXE is not signed). Each row is one start of Setup with the Menu page > Tuning ticks as listed.

| Ticks | What Setup showed |
|---|---|
| none | right after the edition page: "This PC can't run Windows 11 - This PC doesn't meet the minimum system requirements" |
| all five | went on (key page / licence terms / disk page) |
| all but TPM | the same "This PC can't run Windows 11" |
| all but memory | went on: **2 GB did not stop Setup even without the memory tick** |
| all but storage | went on; on the disk page a note says "To install this version of Windows, the system drive needs to be 52 GB or larger" **with the tick as well as without it**, and Next worked on the 40 GB disk (the install ran through to the first-start screens with the tick off) |

So on this build only the TPM tick changed anything the person could see. The Secure Boot and processor ticks could not be tried (the VM is UEFI and its processor is fine); they write the same registry values
as the others and are kept. The memory and storage ticks are harmless, and their absence did not matter on this machine; Setup may enforce them on hardware that is further below the limits.

### Answer file per PC (Hyper-V, same VM, 64 GB disk)

A PC with a Windows answer file attached (an edition by name, the disk wiped and partitioned, a local account, a first-logon command) and a name given on the PCs page ran Setup with no question from the language page to
the end of the first start, got the computer name `SHOP-PC-7` and ran the first-logon command ({{computername}} cut and upper-cased by Windows). The same VM without a file asked every page.

### Local account at the first start (Hyper-V, no network at all)

Windows 11 26H2's first start showed "Let's connect you to a network" with Next greyed out and no link to go on without a connection. Tried, each as an answer file on a fresh install:

| What the answer file held | The network page |
|---|---|
| `HideOnlineAccountScreens` only | still shown |
| `HideOnlineAccountScreens` and the `BypassNRO` value written before the first start (specialize pass) | still shown |
| `HideOnlineAccountScreens` and a local account made by the file, no `HideWirelessSetupInOOBE` | still shown |
| `HideOnlineAccountScreens` and `HideWirelessSetupInOOBE` | **skipped**: "Who's going to use this device?", a name, "Create a super memorable password" (blank accepted), the privacy page |
| `HideWirelessSetupInOOBE` alone | **skipped**, the same pages |
| an account in the file with a password, automatic sign-in, `HideEULAPage`, `HideWirelessSetupInOOBE` and `ProtectYourPC` | skipped; the account was made and the desktop came up |

So `HideWirelessSetupInOOBE` is what works on this build; the `OOBE\BYPASSNRO` route is gone. (A partial answer file also needs the product key page to be shown, or Setup stops with "Windows cannot read the
&lt;ProductKey&gt; setting".) Not tried: Windows 11 builds other than 26300, the Home edition against the Pro edition (the runs used Home, the first edition of the list, and Pro), a localised first start.

### Drivers (Hyper-V and Proxmox)

- Proxmox UEFI VM (e1000 network card, 40 GB disk on a virtio-scsi controller; the `vioscsi` package of virtio-win 0.1.302, Microsoft-signed, in `drivers/all/vioscsi`): without the drivers Setup shows "We couldn't find any drives. To get a
  storage driver, click Load driver."; with the tick on, Setup sees the disk and starts installing onto it. The boot.wim copy held the folder `\$WinPEDriver$\all\vioscsi`.
- Hyper-V (the same package, no matching hardware): after an install the driver store of the new Windows (`C:\Windows\System32\DriverStore\FileRepository`) holds `vioscsi.inf_amd64_...`, shown by a first-logon command: the drivers do
  reach the installed system.
- `setup.exe /installdrivers <folder>` was tried first and does not exist in the set-up image's Setup ("An unknown command-line option [/installdrivers] was specified"): it is not used.
- **Not seen:** the installed Windows starting from the virtio-scsi disk. On the Proxmox test host (an i7-2700K without AVX2) the VM's QEMU exited by itself at about 75 % of "Getting files ready for installation" in two runs, so the install never finished there
  (the cause is not known; the same ISO ran to the end on Hyper-V on a newer processor). Without the unfinished install nothing is claimed about booting from that controller.

## Secure Boot on, through the signed-iPXE route (prototype, 2026-10-26, Hyper-V UEFI, template "Microsoft UEFI Certificate Authority")

Run with the own-key route (`-secure-boot-dir`; see [secure-boot.md](secure-boot.md) for the method, the two ways, the screens and everything that was not tried). Each ISO was started from the menu through its own shim.

On a real PC (MSI, Secure Boot on, proxy DHCP, the iPXE project's shim) the menu, Ubuntu Server and Ubuntu 25.04 worked (table in secure-boot.md); the table below is the Hyper-V lab.

| ISO | Result |
| --- | --- |
| Ubuntu 22.04.5 desktop | **works**: desktop (signed-iPXE route and the iPXE project's shim) |
| Linux Mint 22.3 Cinnamon | **works**: desktop |
| Debian 13.7 live GNOME | **works**: desktop (both ways) |
| Debian 13.7 netinst | **works**: installer's first page |
| Fedora Workstation Live 44 | **works**: GNOME overview |
| openSUSE Tumbleweed NET 20261020 | kernel starts; the installer then stops at DHCP (the link-local lab artefact above), not tried further |
| Ubuntu 26.04.1 live server | kernel starts; the 4 GB VM cannot hold the downloaded ISO (as without Secure Boot) |
| CachyOS 260426 | **does not start**: `Could not boot: Error 0x7f04818f` (unsigned kernel) |
| Windows 11 (FW11HEN) through wimboot | **does not start** in this lab: `Verification failed: Security Policy Violation` for the boot manager (the lab template does not trust Windows' CA); use the `native:` rule |
