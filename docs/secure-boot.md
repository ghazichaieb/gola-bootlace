# UEFI Secure Boot

Bootlace's usual way in is iPXE, and the iPXE that Bootlace ships is not signed by anyone a PC's firmware trusts (wimboot, which loads Windows' set-up environment, is signed by Microsoft, but it is started
by iPXE, which comes first). With Secure Boot **on** the firmware refuses it and the PC stops with `Security Violation`, so it never reaches the menu. There are three ways round that:

1. **The iPXE project's signed shim: the recommended way, for any Linux ISO whose kernel is signed.** Off until a folder is given; no key to enrol in any PC. Next section.
2. A rule that starts **Windows** with the firmware's own signed boot manager (below).
3. The same for **Ubuntu** (below).

## Recommended: the iPXE project's signed shim (a real PC: works)

The iPXE project publishes a **shim signed by Microsoft** and an `ipxe.efi` signed by its own CA, which that shim trusts. A PC with Secure Boot on starts the shim (the firmware
trusts Microsoft), the shim starts `ipxe.efi`, and the usual Bootlace menu comes up: nothing has to be enrolled in the PC. Bootlace serves the two files by TFTP and, for a PC that
is to use them, names the shim instead of its own `ipxe.efi` in the DHCP answer. Tested on a real MSI PC with Secure Boot on, in proxy-DHCP mode (the table below).

### Set it up (a Proxmox container made with the project's script, as root in the container: `pct enter <number>`)

```sh
# 1. the iPXE project's signed files (release v2.0.0), checked before use
cd /tmp
wget https://github.com/ipxe/ipxe/releases/download/v2.0.0/ipxeboot.tar.gz
echo "01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1  ipxeboot.tar.gz" | sha256sum -c -     # must say: ipxeboot.tar.gz: OK
tar xzf ipxeboot.tar.gz ipxeboot/x86_64-sb

# 2. a folder for them; the shim is renamed so that its name says it is the iPXE project's
mkdir -p /var/lib/bootlace/secure-boot
cp ipxeboot/x86_64-sb/shimx64.efi /var/lib/bootlace/secure-boot/ipxe-shimx64.efi
cp ipxeboot/x86_64-sb/ipxe.efi    /var/lib/bootlace/secure-boot/ipxe.efi

# 3. tell the server (two lines in /etc/default/bootlace; /etc/bootlace.env if the container was made by the community-scripts script)
cat >> /etc/default/bootlace <<'EOF'
BOOTLACE_SECURE_BOOT_DIR=/var/lib/bootlace/secure-boot
BOOTLACE_SECURE_BOOT_SHIM=ipxe-shimx64.efi
EOF
systemctl restart bootlace
```

(If `tar` does not find `ipxeboot/x86_64-sb`, list the archive with `tar tzf ipxeboot.tar.gz`; the folder is the one for x86-64 with Secure Boot, `x86_64-sb`.) The container needs
no extra package: the files are only copied and served. Then check it:

- The log (`journalctl -u bootlace -e`) says `Secure Boot route: ON for ...` at the start. If it says `OFF`, the two lines were not read. If it says `warning: ... has no ipxe.efi`, that file is not in the folder.
- The web interface, **Server** page, block **Secure Boot route**: it says on, the folder, and marks each file `present` or `missing` (it looks in the folder every few seconds, so a file copied in
  later shows at once, no restart). The same facts are in the API: `GET /api/secureboot` (and the `secureBoot` field of `GET /api/status`).

### Which PCs are sent through the shim

- **Per PC (the default).** On the **PCs** page press **Secure Boot route** on the row of a PC that has Secure Boot on. A PC appears in that list as soon as it has asked for a boot file, even
  if it never reached the menu (its row then says `Asked for a boot file; Secure Boot may be refusing iPXE`), so: boot the PC from the network once, it fails with `Security Violation`, refresh the
  PCs page, press the button, boot it again.
- **Every 64-bit UEFI PC: a third line `BOOTLACE_SECURE_BOOT_ALL=true`** in the same file (or `-secure-boot-all` on the command line), then `systemctl restart bootlace`. Nothing has to be marked. It adds one boot stage (the shim) to every
  64-bit UEFI PC, with Secure Boot on or off (the shim starts iPXE either way); that was fine in the tests below. BIOS PCs and ARM64 PCs are never sent to the shim.

### What the log shows when it works

```
12:00:01  Secure Boot route: ON for every 64-bit UEFI PC, which are told to start ipxe-shimx64.efi from /var/lib/bootlace/secure-boot; the shim then asks for ipxe.efi
...
12:31:40  dhcp: discover d8:43:ae:d6:2f:05 from 0.0.0.0 answered with "sb/ipxe-shimx64.efi" to 255.255.255.255
12:31:41  tftp: 192.168.1.50 gets ipxe-shimx64.efi (Secure Boot route)
12:31:41  tftp: 192.168.1.50:2074 asks for sb/ipxe-shimx64.efi (options map[...]), 966000 bytes
12:31:42  tftp: 192.168.1.50 gets ipxe.efi (Secure Boot route)
12:31:42  tftp: 192.168.1.50:2075 asks for sb/ipxe.efi (options map[...]), 1100000 bytes
          (then the request for /boot.ipxe, and the PC's row on the PCs page shows the menu)
```

(The numbers are examples.) Both files are fetched, in that order, and then `boot.ipxe`. A DHCP line that names plain `ipxe.efi` instead means the PC was not sent through the shim (first row of the table below).

### If it does not work

| What the log and the PC show | What it means | What to do |
| --- | --- | --- |
| The DHCP answer names plain `ipxe.efi` and the PC shows `Security Violation` | The route is off (`Secure Boot route: OFF` at the start) or this PC is not marked and `-secure-boot-all` is not set | Set `BOOTLACE_SECURE_BOOT_DIR` (see above), then mark the PC on the PCs page or use `-secure-boot-all` |
| The DHCP answer names `sb/ipxe-shimx64.efi`, the shim is fetched by TFTP, but `ipxe.efi` is never requested; the PC shows an error or goes on to the next boot device | The firmware refuses the shim | Check the board's Secure Boot mode (standard/"Windows UEFI mode", not custom with empty keys) and that **Microsoft's third-party UEFI CA** is not disabled (some boards have an option for it, "Allow Microsoft 3rd party UEFI CA"). To tell a firmware problem from a Bootlace problem, start a Linux live USB with Secure Boot on: if it does not start, the firmware does not trust the third-party CA |
| Both files are fetched, then `Security Violation` or `Verification failed` | The shim or `ipxe.efi` was revoked or does not match: an old file, a wrong file, or the PC's revocation list (`dbx`, SBAT) refuses it | Download the files again (the checksum above), use the folder `x86_64-sb` and not another, copy them again |
| The shim is fetched, the PC shows `Security Violation` straight away, no `ipxe.efi` request, and the Server page says a file is missing | The folder is incomplete | Copy the missing file; the block on the Server page turns green |
| The PC's row says `Asked for a boot file; Secure Boot may be refusing iPXE` and nothing else | The PC never reached the menu | One of the lines above; the row's History button shows when it asked |
| The menu comes up, an entry ends with `Could not boot` / `Security Policy Violation` | The shim works, the ISO's kernel is not signed (for example CachyOS) or it is Windows through wimboot | Use the Windows rule below for Windows; a distribution with an unsigned kernel needs Secure Boot off |

### Limits

Only the iPXE project's reduced-feature signed binaries are used in this way, so Bootlace's own iPXE options (for instance the USB-driver switch in `tools/ipxe/config/usb.h`) are not in them;
the project's signing policy for other parties is not stated in its pages (not asked). A Linux ISO starts if its distribution signed its kernel (the measured list is in the table below);
Windows keeps its own way (next section). The older way with a key of your own (needs one enrolment per PC) is described at the end.

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

Tested in the Hyper-V lab with Secure Boot on (Microsoft Windows template): boot manager, BCD, `boot.wim`, WinPE, the share, Windows 11 Setup. On one real PC (an MSI board, proxy-DHCP, the
iPXE project's shim) a Windows 11 installation was run to the end and Windows started; the start method of that run (this rule or wimboot) was not recorded. Not tested: other firmware.

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

## The signed-iPXE route in detail: how it works, the two ways, measurements

Status: **off unless the server is started with `-secure-boot-dir`** (`BOOTLACE_SECURE_BOOT_DIR`); nothing changes for a server started without that option (it says so in the log, and the
Server page shows the route as off). Measured in a Hyper-V lab (Way 1 and Way 2) and on one real PC (Way 2 only, see the table). Issue #10.

The idea: with Secure Boot on the firmware only starts files signed by a key it knows, and iPXE is not one. A **shim** is: Microsoft signs the shims of the Linux
distributions, and a shim starts a second program (by default `grubx64.efi`, fetched by TFTP from the same folder) after checking its signature against its own keys and the
keys a person enrolled in the PC (the "MOK" list). So the DHCP answer names a shim, the shim fetches iPXE as its second stage, and the menu works as usual. What differs between
the two ways measured below is only whose signature the shim accepts on iPXE.

### How it is set up

1. **A folder** with the shim and what it fetches. `-secure-boot-dir <folder>` (also `BOOTLACE_SECURE_BOOT_DIR`); `-secure-boot-shim <file>` names the boot file (default `shimx64.efi`).
   Every file directly in the folder is served by TFTP under `sb/` by its own name, nothing else (no sub-folder, no path; unit tests). The server refuses to start when the shim is missing.
   When the shim is there but the file it asks for next is not (`ipxe.efi` for a shim whose name contains `ipxe`, `grubx64.efi` for any other), the server **starts and warns** in the log and on
   the Server page: the folder is read at every request, so the file can be copied in while the server runs, and refusing to start would take the whole menu down for a folder that is only half filled.
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

The copy-paste steps for a Proxmox container are at the top of this page; this section keeps the facts behind them.

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


### Measurements on a real PC (MSI, UEFI with Secure Boot on, proxy-DHCP mode, Bootlace 0.5.0-rc.1 in a Proxmox container, 2026-10-27)

Way 2 (the iPXE project's `ipxe-shimx64.efi` and `ipxe.efi`, `-secure-boot-shim ipxe-shimx64.efi`, `-secure-boot-all`). Server log for the start: DHCP answer `sb/ipxe-shimx64.efi`, TFTP `sb/ipxe-shimx64.efi`, then `sb/ipxe.efi`, then `boot.ipxe`.

| What | Secure Boot | Outcome |
| --- | --- | --- |
| Plain `ipxe.efi` offered (route off) | on | stops: `Security Violation` |
| Way 2, nothing enrolled: the Bootlace menu | on | **works** |
| Ubuntu Server (chosen from the menu) | on | **booted** |
| Ubuntu 25.04 (chosen from the menu) | on | **booted** |
| Windows 11 installed from the menu (Setup to the end) | on | **worked**: Setup ran, the installation finished and Windows started (reported by the person who ran it; the log lines were not kept, so which of the two Windows start methods was used is not recorded) |

These were the first runs of the route on a real PC; one PC and one firmware say nothing about others (see the troubleshooting table above for what a different firmware may do).

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
- **Not measured:** a real PC with Way 1 or with another make of firmware (Way 2 was measured on one real PC, in proxy DHCP mode, above); a network whose own DHCP server names a boot file (from the shim's source (read, not run), the boot file and server are taken from the DHCP acknowledgement, and the proxy offer is used only when the acknowledgement names
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
