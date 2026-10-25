# ISOs to test

The rule: an ISO is **streamed** (read over the network a piece at a time, never copied whole into the PC's memory). Windows already does this (Setup reads `install.wim`
from the share; only `boot.wim`, about 0.6 GB, is loaded because WinPE is a RAM disk). Ubuntu does it through a network block disk (see secure-boot.md). The rest:

| ISO | Why test it | Family / how it boots | Streaming idea |
|---|---|---|---|
| Clonezilla Live | imaging and backups | Debian live (live-boot) | live-boot also asks `nfsmount`: same stand-in as casper |
| SystemRescue | rescue, backups, partitioning | archiso | archiso has native NBD, NFS and cifs options (`archiso_pxe_nbd_srv=`) |
| Rescuezilla | backups with a GUI | Ubuntu (casper) | works as Ubuntu |
| GParted Live | partitioning | Debian live | as Clonezilla |
| ShredOS (nwipe) | wiping drives | tiny Buildroot image, runs from RAM | small enough to send whole; nothing to stream |
| Linux Mint (Cinnamon) | the most downloaded desktop after Ubuntu | casper | works as Ubuntu |
| Debian live (Standard or GNOME) | popular, different loader | live-boot | `nfsmount` stand-in, or live-boot's `httpfs=` |
| Debian netinst | installs over the network | Debian installer | kernel + initrd from the ISO, the ISO as an iSCSI CD-ROM (extra archive), the rest of the packages from the CD and a mirror |
| Fedora Workstation Live | popular, different family | dracut (`root=live:`) | dracut has an NBD client |
| Kali Linux (installer or live) | very common in IT | Debian live | as Debian |
| openSUSE Tumbleweed / Leap | different family | dracut | as Fedora |
| Ubuntu Server (live) | no desktop, subiquity | casper | as Ubuntu |
| Ubuntu 24.04 Desktop | newer casper and kernel | casper | check the stand-in still applies |
| Windows 10 22H2 | an older WinPE than Windows 11 | wimboot / signed boot manager | same as Windows 11 |
| Windows Server 2022 | server edition | same | same |
| Hiren's BootCD PE | repair toolkit, WinPE based | WinPE | needs its own way (no `install.wim`) |
| Memtest86+ | memory test | tiny, own loader | boots as it is |
| Proxmox VE, TrueNAS, pfSense, Alpine, Arch / CachyOS | already tried | see compatibility.md | |

Which of them also work with UEFI Secure Boot on depends on who signs their loader: see secure-boot.md.
