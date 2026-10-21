# Tested ISOs

An ISO is listed as **works** only after it booted to its installer or live desktop through Bootlace in our test rig (a Hyper-V generation 2 VM on a private
switch, UEFI with Secure Boot off, started by `tools/lab.ps1`). Nothing here is a promise that another ISO of the same distribution boots: each line is one
file we tried. BIOS (legacy) boot has not been tested yet.

| ISO | Version | BIOS | UEFI | Result | Date | Notes |
|---|---|---|---|---|---|---|
| Alpine Linux virt | 3.24.2 | not tried | yes | **works** | 2026-10-21 | Kernel, modules disk and packages are read out of the ISO over HTTP; reaches the login prompt. |
| Ubuntu Desktop | 22.04.5 | not tried | yes | **works** | 2026-10-21 | The initramfs downloads the whole ISO into memory (`url=`): the PC needs RAM for the ISO and then some (12 GB for the 4.4 GB ISO; 10 GB was not enough). Reaches the desktop and the installer icon. |
| CachyOS Desktop | 260426 | not tried | yes | **works** | 2026-10-21 | archiso network boot (`archiso_http_srv`), image copied to memory. Needs `ip=:::::eth0:dhcp`: with plain `ip=dhcp` the start-up script ran before the network card existed. Reaches the KDE desktop. |
| Proxmox VE | 9.1-1 | not tried | yes | **works** | 2026-10-21 | The ISO is given to the kernel as a second start-up disk named `/proxmox.iso`. The graphical installer starts (it warns that the VM has no hardware virtualisation: a property of the test VM). |
| TrueNAS SCALE | 25.10.7 | not tried | yes | **works** | 2026-10-21 | Debian live style (`fetch=` the squashfs). The installer menu appears. |
| pfSense CE | 2.7.2 | not tried | yes | **fails** | 2026-10-21 | Handed to the PC as a disk: FreeBSD's loader and kernel start, then the kernel cannot see the disk (the firmware's virtual disk is gone once the kernel runs). Needs the BSD method (issue #9). |
| Windows Server 2025 and Windows 11 24H2 ISOs | | | | **untested** | | Windows ISOs keep their files in UDF, and need the Windows method (issue #5). |

Result values: **works**, **partial** (boots but a step needs help; see notes), **fails**, **untested**.
