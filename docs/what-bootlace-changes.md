# What Bootlace changes about the system it starts

Short answer: **nothing is written to the ISO or to the PC's disks by Bootlace, and the system that an installer puts on the disk is the installer's own.** But the
system that *runs* from the network is not started quite the way its own boot menu would start it, and a few features that depend on the boot medium behave differently.
This page lists what, and says for each point whether it was checked on a real run (**checked**) or follows from how the system works (**expected**).

## What is never changed

- **The ISO files.** The web server, the network disk server, the iSCSI target and the Windows share are all read-only. The network disk is flagged read-only to the PC's
  kernel, and the iSCSI target answers a write with "write protected" (**checked**: code and tests; the kernel then treats the disk as read-only).
- **The PC's disks and firmware settings.** Bootlace only starts a system; it never partitions, formats or writes a disk, and it does not touch the UEFI boot entries. A
  Linux started this way sees the full EFI interface (**checked**: `/sys/firmware/efi/runtime` and `efivars` are there and `efibootmgr` lists the firmware's boot entries on
  a UEFI PC), so an installer can create its own boot entry as it would from a disc.
- **The installed system.** An installer copies its own files. The extra files Bootlace adds (the network disk client, drivers, small scripts) are in the memory disk the
  kernel starts from, which is thrown away when the real system takes over: they are in no file the installer copies (**expected**; **checked** for Proxmox VE, which was installed to the end
  this way).
- **Windows.** Setup is the ISO's own `setup.exe`, run from a read-only share, and installs the ISO's own `install.wim` (**checked**, installation run to the end with
  Windows 11 in the lab). The only things added are two small files in a *copy* of `boot.wim` (kept in the cache folder; the ISO is not changed): a command file that replaces
  Setup's launcher and connects the share, and a one-line deletion of the registry key that makes Setup think it was started by the retired WDS service. An
  `autounattend.xml` in the ISO's folder is passed to Setup.

## What is different while the system runs

1. **The network must stay up.** The system reads its ISO over the LAN for as long as it runs. If the cable is pulled or the server stops, the system freezes or reports
   read errors until it is back (the connection is retried for up to an hour). An installer is as fast as the LAN, and "remove the installation medium" at the end means
   nothing (**checked**: behaviour seen on every ISO tested).
2. **The ISO shows up as a disk.** A live system sees `/dev/nbd0` (a read-only disk the size of the ISO) or an iSCSI CD. Installers and disk tools may list it
   (**checked**: ChimeraOS's installer offers `nbd0` and refuses it as "too small"). Choosing it as a target fails; nothing is lost.
3. **The network is up before the system starts, and its address is kept.** The PC takes the address its DHCP server gave it, and for Ubuntu and its relatives the live session
   keeps exactly that address (the live system would otherwise ask again, get another one, and lose the ISO). So the live session has a fixed address instead of a leased one
   (**checked** that the session works; **expected**: Ubuntu Server's installer may show the network card as set by hand: look at that screen before accepting it).
4. **The kernel gets different arguments from the ones in its own boot menu.** Bootlace takes out the ones that say where the medium is (`findiso=`, `toram`,
   `live-media=`, `root=LABEL...`, `archisolabel=`, ...) and adds the ones that point at the network disk (`ip=dhcp`, `netboot=nfs`, `console=tty0`, ...). The rest of the menu's own
   arguments are kept. Whatever follows a `---` is left to the distribution: Debian's and Ubuntu's installers copy those arguments into the installed system's kernel command
   line, so Bootlace puts nothing after it (**checked**: a test pins this).
   Two systems get one more on purpose: Fedora-family live ISOs get `enforcing=0` (SELinux permissive, for this live session only: Bazzite's live system stops at the first
   thing it labels otherwise), and Ubuntu desktop ISOs get `cloud-init=disabled`. Neither is a setting of the installed system (**expected**: an installer does not copy
   them; not checked on a complete install of each).
5. **The distribution's own boot loader and menu are not used.** iPXE loads the kernel, so what that menu would have offered (language and "safe graphics" choices, OEM
   install, memtest, persistence) is not offered (planned: [#35](https://github.com/ghazichaieb/gola-bootlace/issues/35)). Extra arguments can be added per ISO in the web interface. Some systems start
   in another video mode than from their own boot loader (**expected**).
6. **Network card drivers may be loaded earlier than usual.** A few start-up disks (Tails, Alpine, Proxmox VE, ChimeraOS) have no network drivers because they were made for
   a USB stick or disc. Bootlace brings the common ethernet drivers from the ISO's *own* kernel modules (same kernel version) and loads them first (**checked**).
7. **Time and logs.** The server notes which PC (MAC and address, and its model if the firmware says) chose what, and keeps that list for 90 days by default (Dashboard,
   PCs). Nothing about what is done inside the installed system is seen.

## Systems that need a word of warning

| System | What differs | Verified |
|---|---|---|
| **Tails** | The network card is used with its **real MAC address before Tails starts** (it has to be, to read the system), and the network cannot be off. Tails normally randomises the MAC and keeps the network off until its greeter. So the protections that depend on that are weakened: **do not use Bootlace to start Tails when anonymity matters.** Persistent Storage and "install to USB" need the USB stick it was started from, and are not available. | checked (it starts to the greeter); the MAC point follows from the method |
| **Lakka** | Storage is in memory: settings, saves and downloads are gone at the next start. | checked |
| **Porteus** | Started "always fresh" (`base_only nomagic norootcopy`); nothing is saved. The whole ISO is held in memory, twice over (it needs about two times the ISO's size in RAM). | checked |
| **openSUSE** (installer) | The installation source is the Bootlace server (`install=http://...`). YaST may leave that address as a package repository in the installed system; it will fail to refresh once the server is gone (`zypper rr` removes it). | expected, not run to the end |
| **Fedora, CentOS, Rocky, Alma installers** | `inst.repo=` points at the server for the installation. | expected: the installed system keeps the distribution's own repositories; not run to the end |
| **Disk images** (`.img`) started "as a disk" | The disk is read-only: a system that writes to its own disk (updates, settings, a journal) fails or runs read-only. | checked on a few images |
| **FreeBSD, pfSense, OPNsense** | Started by the PC's own firmware loading FreeBSD's loader from the server, with the ISO as a memory disk. | experimental; needs a real PC |
| **Secure Boot** | iPXE and wimboot are not signed. With Secure Boot on, only Windows and Ubuntu start (their own signed boot managers). A measured boot (TPM) records different values than a start from a disc. | checked in the lab |

## What is worth doing before relying on it

- Run one complete installation of each distribution you care about, in a virtual machine, before doing it on a PC that matters. Only Proxmox VE, and Windows through the
  iPXE way, have been taken to the end so far; the others have been taken to their installer or desktop.
- Keep the PC on a wired connection with a fixed or reserved address while it runs from the network.
- For an installer, check the network page and the package sources it shows before accepting them.
