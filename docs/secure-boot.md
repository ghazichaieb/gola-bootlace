# UEFI Secure Boot

Bootlace's usual way in is iPXE, and iPXE is not signed by anyone a PC's firmware trusts (wimboot, which loads Windows' set-up environment, is signed by Microsoft, but it is started
by iPXE, which comes first). With Secure Boot **on** the firmware refuses it, so the PC never reaches the menu. There are three ways round that. Two are built in and work
today (Windows and Ubuntu, below). The third, for everything else, is a prototype that is off by default (the last section).

## Windows, with Secure Boot on (works)

Windows' own boot manager is signed by Microsoft, so the firmware accepts it. A rule in the web interface (PCs page, "Install ..., once (works with Secure Boot)", or a
`native:<id>` rule in the settings file) makes the DHCP answer for that PC's next network boot name `win/<id>/bootmgfw.efi`. Then:

1. The firmware fetches `bootmgfw.efi` from the ISO by TFTP (this also tells the server which PC is after which ISO).
2. The boot manager fetches the ISO's BCD, fonts, `boot.sdi` and **`boot.wim`** from the same server by TFTP (about 600 MB; roughly 1.5 minutes on a gigabit link).
3. That `boot.wim` is a **copy of the ISO's with two files added** (`winpeshl.ini`, `bootlace.cmd`), made once with `wimlib-imagex` (install `wimtools`; the Proxmox script does)
   and kept in the cache folder next to the settings file. It is made again when the ISO, the server's address or the share's password changes (the password is kept in the
   settings file for this reason). Making it takes a few seconds.
4. WinPE starts, `bootlace.cmd` connects the share and starts Setup from it, exactly as in the iPXE way.
5. The rule is **used up** when the DHCP answer is given, so that the PC that has just installed Windows does not install it again at its next network boot.

Things found out on the way:

- `Windows Deployment Services client functionality is being partly deprecated` (and, a moment later, `WdsClient: There was a problem initializing WDS Mode`): Microsoft is
  retiring WDS, and since Windows 11 Setup started from the ISO's own `boot.wim` after a PXE boot runs in "WDS mode", which is blocked
  ([Microsoft's page](https://learn.microsoft.com/en-us/windows/deployment/wds-boot-support)). What is still supported is a custom boot image that runs Setup from a network
  share, which is what Bootlace does. Setup decides it is in WDS mode from the registry key `HKLM\SYSTEM\CurrentControlSet\Control\PXE`, which the boot manager leaves behind;
  `bootlace.cmd` deletes it before starting Setup, and the usual Windows 11 Setup window comes up.
- The boot manager asks for its files with backslashes and in any case, and for `\Boot\BCD`, which the ISO keeps as `efi/microsoft/boot/bcd`: `windowsName` maps these.
- A BIOS PC cannot use this way yet (and does not need it: there is no Secure Boot there).

Tested in the Hyper-V lab with Secure Boot on (Microsoft Windows template): boot manager, BCD, `boot.wim`, WinPE, the share, Windows 11 Setup. Not tested: a real PC, and an
installation run through to the end this way (it is the same Setup, started the same way, as in the iPXE way, where it was).

## Ubuntu, with Secure Boot on (works, on any amount of RAM)

Microsoft signs Ubuntu's shim, the shim carries Canonical's key, GRUB and the kernel are signed by Canonical, so the whole chain passes with Secure Boot on. The rule
(`native:<id>` on an ISO that is recognised as "Ubuntu (casper)") names `ubuntu/<id>/bootx64.efi` as the boot file. Then:

1. The firmware fetches the **shim** from the ISO (TFTP). It asks for `grubx64.efi` in the same folder.
2. The GRUB on an Ubuntu ISO is a media-only build: it has no `tftp` or `http` and Secure Boot forbids loading them (`Secure Boot forbids loading module from (tftp,...)`).
   So the answer to `grubx64.efi` is **Ubuntu's own signed network GRUB** (`grubnetx64.efi.signed`, shipped unchanged in internal/assets/ubuntu; see NOTICE).
   (An older one, from `jammy-updates`, was refused by the shim: `Verification failed: (0x1A) Security Violation`; the one from `noble-updates` is accepted.)
3. That GRUB asks for `grub/grub.cfg`; the server makes one that starts the ISO's own `casper/vmlinuz` and `casper/initrd` over HTTP, plus a second initramfs archive (below).
4. The kernel (signed by Canonical) starts and the live system comes up. Tested in the Hyper-V lab with Secure Boot on (the "Microsoft UEFI Certificate Authority" template, which
   Linux shims need): **Ubuntu 22.04.5 desktop on a VM with 2 GB of RAM** reached the desktop.

### Low RAM: the ISO is a network disk

casper's usual network boot downloads the whole ISO into a memory file system that may take half the RAM (a 4.4 GB ISO needed about 12 GB; a 10 GB VM ended with
`wget: short write: No space left on device`). Bootlace instead offers the ISO as a **network block disk** and the live system reads it a piece at a time:

- The server has a small read-only **NBD server** (`internal/nbd`, port 10809, `-nbd-port`; 0 turns it off). The export name is the ISO's id.
- A second initramfs archive, made on the fly at `/x/<id>/extra.cpio` and loaded after the ISO's own (the kernel lets later files replace earlier ones), carries two files:
  `bootlace-nbd` (`cmd/bootlace-nbd`, a static Go program that hands the connection to the kernel's `nbd` driver; the `nbd` and `isofs` modules are in Ubuntu's initramfs)
  and a `nfsmount` script. casper calls `nfsmount` when told `netboot=nfs nfsroot=...`; this one attaches `/dev/nbd0` and mounts the ISO from it where casper wanted the share.
  If that fails it says so and stops: the whole ISO is never copied into the PC's memory.
- Why through `nfsmount`: casper has no hook that does not depend on a generated file list (`ORDER`), but it runs `nfsmount` by name, and the file can simply be replaced.
  (Also tried: klibc's real `nfsmount` hangs without a portmapper and has no `mountport=` option; `netboot=cifs` would need the share to accept `root` with no password and to speak
  SMB 3, and casper does not read `nfsopts=` from the kernel command line.)
- The same archive is used on the iPXE way (`initrd` twice), so it applies with Secure Boot off too.

The helper is rebuilt with `tools/build_helper.sh` (x86-64 Linux only for now).

## Everything else, with Secure Boot on: the signed-iPXE route (prototype)

Status: a prototype, **off unless the server is started with `-secure-boot-dir`**, measured in a Hyper-V lab only (no real PC, issue #10). Nothing changes for a server started
without that option.

The idea: with Secure Boot on the firmware only starts files signed by a key it knows, and iPXE is not one. A **shim** is: Microsoft signs the shims of the Linux
distributions, and a shim starts a second program (by default `grubx64.efi`, fetched by TFTP from the same folder) after checking its signature against its own keys and the
keys a person enrolled in the PC (the "MOK" list). So the DHCP answer names a shim, the shim fetches iPXE as its second stage, and the menu works as usual. What differs between
the two ways measured below is only whose signature the shim accepts on iPXE.

### How it is set up

1. **A folder** with the shim and what it fetches. `-secure-boot-dir <folder>` (also `BOOTLACE_SECURE_BOOT_DIR`); `-secure-boot-shim <file>` names the boot file (default `shimx64.efi`).
   Every file directly in the folder is served by TFTP under `sb/` by its own name, nothing else (no sub-folder, no path; unit tests). The server refuses to start when the shim is missing.
2. **Which PCs.** The server cannot see a PC's Secure Boot state (the DHCP architecture code is the same with it on and off), so a person says it: the PCs page has a
   **Secure Boot route** button per PC (saved in the PC's options, `secureBoot`), or `-secure-boot-all` sends every 64-bit UEFI PC through the shim. The shim also starts with
   Secure Boot off (measured: nothing had to be pressed), so "all" is possible, at the price of an extra boot stage for PCs that did not need it. BIOS PCs, iPXE itself (once it runs it is
   given its script, not the shim again), ARM64 PCs and a start in progress for Windows or FreeBSD (the `native:` rule) are never sent to the shim (unit tests).
3. **Kernels.** With the route on, every menu entry that starts a Linux kernel gets one more iPXE line before `boot`: `shim <ISO>/EFI/boot/bootx64.efi`, the ISO's own shim, read
   from inside the ISO over HTTP. iPXE then starts the kernel *through that shim*, which checks the kernel with the keys the distribution put into it. With Secure Boot off,
   or on a BIOS PC, the command does nothing. Entries that are not kernels (a disk image, Windows) are left as they are.

### Way 1: iPXE signed with our own key (`tools/ipxe/build-sb.sh`)

`docker run --rm -v "$PWD/tools/ipxe:/work:ro" -v "$PWD/out:/out" -v "/path/to/keys:/keys:ro" debian:12 sh /work/build-sb.sh` builds iPXE from the pinned commit with
`SECUREBOOT=1` (iPXE's own safe set: its build refuses features that can start unsigned code; Bootlace's picture, console and colour options pass) and signs it (`sbsign`) with
`db.key` and `db.crt` from the folder given. The key is never in the repository or the image. The build carries its own SBAT line (`ipxe.gola,1`,
`tools/ipxe/config-sb/branding.h`), so that a build found unsafe can be refused by PCs without waiting for upstream: raise the number. The signed file goes into the folder as
`grubx64.efi` next to a distribution's `shimx64.efi` and `mmx64.efi` (MokManager).

The certificate has to be **enrolled in each PC once** (the shim's MOK list), through MokManager, which only reads a file from a disk:

| Step | What the person sees | Keys |
| --- | --- | --- |
| 1 | Blue box `Verification failed: (0x1A) Security Violation` (the shim refuses iPXE) | Enter on `OK` |
| 2 | `Shim UEFI key management`, `Press any key to perform MOK management`, `Booting in 10 seconds` | any key |
| 3 | `Perform MOK management`: Continue boot / **Enroll key from disk** / Enroll hash from disk | Down, Enter |
| 4 | `Select Key`: the volume (here `MOKDISK`) | Enter |
| 5 | `Select Key`: the file (`secureboot.cer`) | Enter |
| 6 | `[Enroll MOK]`: View key 0 / Continue | Down, Enter |
| 7 | `Enroll the key(s)?`: No / Yes | Down, Enter |
| 8 | `Perform MOK management`: **Reboot** | Enter |

That is 8 screens and 12 key presses, once per PC, with a FAT-formatted USB stick holding `secureboot.cer` (the DER form, which `build-sb.sh` writes next to the signed file) plugged
in. The key is kept in the PC's firmware variables (it survived a power-off and a second shim from another distribution). **It cannot be done from the network**: MokManager offers
disks only. Nobody has to be at a PC afterwards. Enrolling the certificate in the firmware's own key database instead (so that no shim is needed) is the other way; Hyper-V does
not let a VM's database be edited, so that was not tried.

### Way 2: the iPXE project's own signed shim and binaries (no enrolment)

The iPXE project (v2.0.0, 2026-03-06, release file `ipxeboot.tar.gz`, folder `x86_64-sb`) publishes a shim signed by Microsoft that trusts the "iPXE Secure Boot CA", and `ipxe.efi`
signed by that CA ([ipxe.org/secboot](https://ipxe.org/secboot)). Folder: the shim as `ipxe-shimx64.efi` (the shim takes the name of its second stage from its own name, minus
`shim`: it asks for `ipxe.efi`) and `ipxe.efi`; server options `-secure-boot-dir <folder> -secure-boot-shim ipxe-shimx64.efi`. A fresh VM, nothing enrolled, no disk, **reached the
menu with Secure Boot on** and started Linux the same way. The price: only the project's reduced-feature binaries are signed ("you can't build your own iPXE with the features you need
in Secure Boot mode, and the page says that is intentional"), so Bootlace's own iPXE options (for instance the USB-driver switch in `tools/ipxe/config/usb.h`) are not in them, and the
project's signing policy for other parties is not stated in its pages (not asked).

### Measurements (Hyper-V generation 2, 4 GB, Secure Boot on, template "Microsoft UEFI Certificate Authority", host Windows 11; 2026-10-26)

Server: this branch (`-secure-boot-all`, a lab copy on a private switch with its own ports and settings); iPXE built with `SECUREBOOT=1` from commit a5eed98 and signed by a throw-away key
made for the lab (never kept). Shim of the first stage: the one on the Ubuntu 22.04.5 desktop ISO (shim 15.8, signed by "Microsoft Corporation UEFI CA 2011") unless said.

| What | Secure Boot | Outcome | What the screen showed |
| --- | --- | --- | --- |
| Shim from the server, Way 1, key not enrolled | on | stops | `Verification failed: (0x1A) Security Violation` ... then MokManager (steps 1 and 2 above) |
| Way 1, enrolment from a FAT disk | on | works | steps 3 to 8, then the PC restarts |
| Way 1, after enrolment: iPXE and the Bootlace menu | on | **works** | the usual picture menu; iPXE's own `autoexec.ipxe` is asked for and not found (harmless) |
| Way 1 with Debian 13's shim (16.1) as first stage, same PC | on | works | menu, no new screen: the MOK list is shared between shims |
| Way 1, shim and iPXE with no key enrolled | **off** | works | the menu came by itself, no key pressed: the shim just starts iPXE |
| Way 2 (project's shim and signed iPXE), nothing enrolled | on | **works** | the menu (the picture menu on the first start; a plain text menu after a failed Windows start) |
| A kernel started by the ISO's own shim (`shim` line): Ubuntu 22.04.5 desktop | on | works, Way 1 and Way 2 | desktop |
| Linux Mint 22.3 Cinnamon | on | works (Way 1) | desktop |
| Debian 13.7 live GNOME | on | works, Way 1 and Way 2 | desktop |
| Debian 13.7 netinst | on | works (Way 1) | the installer's first page |
| Fedora Workstation Live 44 | on | works (Way 1) | the GNOME overview |
| openSUSE Tumbleweed NET (20261020) | on | kernel starts (Way 1) | `linuxrc` runs, then `Sending DHCP request to eth0... no/incomplete answer`: the known lab cause (link-local addresses, see compatibility.md), not a Secure Boot failure |
| Ubuntu 26.04.1 live server | on | kernel starts (Way 1) | downloads the ISO into memory, `wget: short write: No space left on device` on 4 GB (the known memory limit of that start, not Secure Boot) |
| The same Ubuntu 22.04.5 kernel **without** the `shim` line | on | fails (Way 1) | `Could not boot: Error 0x7f04818f (https://ipxe.org/7f04818f)`, "Could not load image" |
| CachyOS 260426 (Arch, unsigned kernel) | on | **fails** | Way 1: `Could not boot: Error 0x7f04818f`; Way 2: three lines `Verification failed: Security Policy Violation`, then `Could not boot: Error 0x7f04819a`. Nothing starts |
| Windows 11 Setup through wimboot (FW11HEN), Way 1 | on | fails | blue box `Could not install security protocol: (0x2) Invalid Parameter`, then `Something has gone seriously wrong: Invalid Parameter. shim cannot continue, sorry.`; the VM turns itself off. Same box when wimboot is started with no files |
| Windows 11 Setup through wimboot, Way 2 | on | fails, cleanly | wimboot runs (`Secure Boot is enabled`, files patched), then `Verification failed: Security Policy Violation`, `Could not load bootmgfw.efi: 0x800000000000001a`, `Could not boot: Error 0x7f048281`; back to the menu |
| A VM with the template "Open Source Shielded VM" | on | the shim is refused by the firmware | Hyper-V's own screen: `The signed image's hash is not allowed (DB)` (this template cannot be used) |

Notes on what the table does and does not prove:

- The Linux ISOs started with the **distribution's own kernel, signed by that distribution**, through **that ISO's own shim**: nothing of theirs had to be enrolled. Where it works,
  the person sees only the extra screens above (Way 1, once) or nothing (Way 2). Which distributions work is therefore "those whose ISO has a shim and a signed kernel": measured
  Ubuntu, Linux Mint, Debian, Fedora, openSUSE (kernel reached); not measured: others. A distribution with an unsigned kernel (CachyOS, measured) does not start; the only way is
  to start that PC with Secure Boot off (or enrol that distribution's own key, not tried).
- **Windows is the open point.** The Windows boot manager of that ISO was refused ("Security Policy Violation") probably because this lab template trusts only Microsoft's third-party CA
  (the one that signs shims and wimboot) and not the Windows CA (the template with the Windows CA was not tried; the one other template tried trusted neither), so the lab cannot say
  whether wimboot followed by Windows' boot manager works on a real PC, whose firmware normally has both. The refusal of Way 1 (a crash of the shim instead of an error) differs from Way 2 (an error message); the cause was not
  investigated (a shim that regains control when the program it started exits is one guess, not checked). Until measured on a real PC, Windows should keep its existing way: a `native:`
  rule (above), which the DHCP answer serves before the shim.
- `wimboot` itself **is signed by Microsoft** (the UEFI CA 2011; checked with `sbverify` on `internal/assets/ipxe/wimboot`), so the sentence "wimboot is not signed" that this page used to
  carry was wrong. What is unsigned is iPXE, which has to run first.
- **Not measured:** a real PC (any), proxy DHCP mode (the lab server hands out addresses itself; from the shim's source (read, not run), the boot file and server are taken from the DHCP acknowledgement, and the proxy offer is used only when the acknowledgement names
  no boot file, so a network whose own DHCP server names one would send the shim there), HTTP boot, ARM64, MokManager when no key is pressed,
  "Enroll hash from disk", firmware-level enrolment, Windows with both Microsoft CAs trusted, Fedora's and Ubuntu 26.04's shims as first stage, the "as a disk" starts and
  the "other ways" menu under Secure Boot (their entries do not get the `shim` line yet), a distribution's kernel that needs its own key enrolled, any speed or reliability figure.

### Risks and open questions

- **Revocation.** Shims and firmware refuse programs by hash (the `dbx` list) and by SBAT generation; the shims measured carry `ipxe,1` as the accepted generation of iPXE. A security
  fix in iPXE raises that number upstream, and then every older build, ours included, would be refused by updated PCs: builds must follow upstream, and our own line
  (`ipxe.gola`) is the lever for the cases upstream does not cover. A shim that Microsoft later revokes stops the whole route on updated PCs; the shims measured were signed with the 2011
  UEFI CA (the 2023 CA that replaces it was not looked at).
- **Key custody (Way 1).** The signing key is a Secure Boot key for every PC that enrolled it: anyone who has it can sign code that those PCs run. It must not be in the repository, in CI
  or on the server; a hardware token or an offline machine. If it leaks, every PC has to remove it (MokManager again). This is the owner's decision; the lab key was thrown away.
- **Which shim to ship.** The distributions' shims are Microsoft-signed binaries under their own licences (shim's repository says BSD-2-Clause, not re-checked here; the distributions' copyright files and the project's
  `NOTICE` have to carry them if Bootlace ever embeds one; the iPXE project's shim licence was not looked at). The prototype embeds none: the folder is filled by hand from an ISO.
- **A signed iPXE is a trusted program.** With `SECUREBOOT=1` its build refuses the features that can start unsigned code; a build without that flag must never be signed.
