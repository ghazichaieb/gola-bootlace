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
3b. **Ubuntu Server ISOs are loaded into memory.** Their installer (subiquity) stops when it finds the network disk that carries the ISO, so Bootlace downloads these ISOs whole into the PC's memory
   (about the size of the ISO and 2 GB more is needed). Ubuntu desktop ISOs still use the network disk (**checked**: an autoinstall of Ubuntu Server 26.04.1 completed this way in a UEFI VM).
4. **The kernel gets different arguments from the ones in its own boot menu.** Bootlace takes out the ones that say where the medium is (`findiso=`, `toram`,
   `live-media=`, `root=LABEL...`, `archisolabel=`, ...) and adds the ones that point at the network disk (`ip=dhcp`, `netboot=nfs`, `console=tty0`, ...). The rest of the menu's own
   arguments are kept. Whatever follows a `---` is left to the distribution: Debian's and Ubuntu's installers copy those arguments into the installed system's kernel command
   line, so Bootlace puts nothing after it (**checked**: a test pins this).
   Two systems get one more on purpose: Fedora-family live ISOs get `enforcing=0` (SELinux permissive, for this live session only: Bazzite's live system stops at the first
   thing it labels otherwise), and Ubuntu desktop ISOs get `cloud-init=disabled`. Neither is a setting of the installed system (**expected**: an installer does not copy
   them; not checked on a complete install of each).
5. **The distribution's own boot loader and menu are not used.** iPXE loads the kernel, so what that menu would have offered (language and "safe graphics" choices, OEM
   install, memtest, persistence) is not shown as such. Instead, the entries of the ISO's own menu that can be started the same way (by the same strategy as the usual start, with that entry's kernel, start-up disk and arguments
   minus the ones that say where the medium is) are offered on the menu line "Other ways to start an ISO" (**checked** on a UEFI VM and on a BIOS VM: Clonezilla's VGA 800x600 entry reaches its language page).
   Entries that depend on the medium (persistence, `toram`, a chain to another disk, memtest images) are left out; "check the disc" is offered but reads the ISO over the network. Extra arguments can be added per ISO (and per PC) in the web interface. Some systems start
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

## Managing an ISO's alternative entries

On the **ISOs** page, expand an ISO's other ways to start it, then choose **Manage entries**.
Each compatible alternative can be hidden, renamed or given extra kernel arguments. Empty names keep the ISO's own title.
Select **Default entry** to use that alternative whenever this ISO starts from the main menu, the timeout, or a PC rule.
The default cannot be hidden. **The usual start** remains the first choice in the other-ways menu; choose it as the default to reset the selection.
ISO arguments apply to every start; an alternative's extra arguments follow them, then the PC's options. Passwords, debug options and attached answer files still apply.

Choices are saved with the settings and included in backups. Renaming an ISO through the library page keeps its choices; deleting it removes them.
Entries are identified by their boot payload and volume, rather than their title, position or the server address. An ISO update that changes a payload creates a new entry: old choices are retained,
but an unavailable default temporarily uses the usual start. After recognition finishes, reopen the dialog to choose a replacement.
This manages only the compatible entries already offered by Bootlace; it does not expand family support or change disc-check and persistence behavior.
The management paths and generated UEFI/BIOS scripts are tested in software; these new settings have not been walked through on a booting VM or physical PC.
### Boot entries after a server restart

Bootlace keeps inspected initramfs facts in the server's local cache under `boot-findings/` (one bounded file per ISO path).
When the ISO, server address and program version still match, the first request after a restart rebuilds its usual start and alternative entries from the ISO's current menus and those facts.
A configured alternative can therefore be the default immediately, without waiting for background initramfs inspection. Boot commands, passwords and custom archive-building functions are not serialized.
The archive builder is reconstructed from the current ISO tree; this cache does not store a bootable initramfs archive itself.

The cache is disposable. A missing, corrupt, oversized or incompatible file falls back to background recognition; the usual start remains the temporary fallback in that case.
Changing the ISO path, size or modification time, server address, or program version causes a miss. The cache does not detect a replacement that preserves both size and modification time.
**Look inside the ISOs again** discards the disk facts and re-runs inspection. Failure to write the cache is logged and does not prevent booting.
Servers embedded without a cache directory retain the previous in-memory behavior. The restart path is tested with a small ISO fixture and concurrent requests; no new VM or hardware boot was performed.
