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
| pfSense CE | 2.7.2 | not tried | yes | **fails** | 2026-10-21 | Handed to the PC as a disk, the FreeBSD kernel cannot see the disk. Second method (menu entry "FreeBSD, experimental"): iPXE tells the server which ISO was chosen and restarts the PC; the PC's own firmware then loads the ISO's loader.efi from this server by TFTP (this part works, see internal/handoff). The loader starts and does its own DHCP, then stops: it sends no TFTP or NFS request, so it never reaches the kernel. Issue #9. |
| Windows 11 24H2 (FW11HEN, an a Windows image trimming tool build, Pro, English) | 24H2 | not tried | yes | **works** | 2026-10-21 | wimboot loads the ISO's WinPE, which maps Bootlace's built-in SMB share and runs `X:\sources\setup.exe /installfrom:Z:\<id>\sources\install.wim /unattend:...` from the ISO's folder. Installed to an 80 GB disk, rebooted from the disk into the new Windows. Setup went through the whole copy over the network (about 25 minutes in the lab). |
| Windows 11, custom-built (EN, Home+Pro, 9 GB ISO) | 26300.9711 | not tried | yes | **works** | 2026-10-21 | Same method. Setup showed both editions read over the share; Pro installed to an 80 GB disk (the whole 9 GB image copied over the network in about 12 minutes) and the new Windows reached the first-boot computer-name screen. The first attempt failed with 0x8007003B because the SMB server lost a half-read frame under the heavy copy (fixed in 046cc31). |
| Windows 11, custom-built (FR, Home+Pro) | 26300.9711 | not tried | yes | **partial** | 2026-10-21 | Same method. French Setup starts from the share; not installed to the end (the English build of the same program was). |
| Windows 11 FW11PFR (custom-trimmed build, Pro, French) | 24H2 | not tried | yes | **partial** | 2026-10-21 | Same method. French Setup starts and reaches the disk page; the install itself was not run to the end (it is the same path as FW11HEN). |
| Windows Server 2025 (Standard/Datacenter, Jan 2025 media) | 2025 | not tried | yes | **partial** | 2026-10-21 | Same method. Windows Server Setup starts, and lists its four editions read out of install.wim over the share; not installed to the end. |
| Windows 11 FW11HFR / FW11PEN, custom-built Windows 11 -sb2023 builds on Secure Boot hardware | | | | **untested** | | Same method; not run yet (the lab has Secure Boot off). |

Result values: **works**, **partial** (boots but a step needs help; see notes), **fails**, **untested**.

## BIOS (legacy) boot, 2026-10-22

Tested in a Proxmox VM (SeaBIOS, 6 GB) started from the network; the menu, with the logo picture, comes up through iPXE's VESA console.

- **Windows Setup environment (wimboot) now starts on BIOS** (Windows Server 2025 and FW11HEN: WinPE reaches the Bootlace command window). It failed with `Bad CPIO magic`
  because on a BIOS PC wimboot reads each file's name from the image's command line, and the script only gave `--name` (which UEFI uses). Every `initrd` line now has both:
  `initrd --name boot.wim <address> boot.wim`.
- A QEMU VM must not use the default `kvm64` CPU: bootmgr restarts the VM the moment wimboot hands over. Use CPU type `host` (or `x86-64-v2-AES`).
- The PC needs a network card WinPE has a driver for (the VM's `virtio`, `e1000` and `e1000e` cards have none: "installation files could not be reached", ipconfig lists no adapter). So
  the install itself was not run through on BIOS; the network and share part is the same as on UEFI, where it works.
- iPXE's old BIOS file (from boot.ipxe.org, 2019) has no `console`, `cpair` or PNG support. Bootlace now ships its own build (see NOTICE and tools/ipxe).
