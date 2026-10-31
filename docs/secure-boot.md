# UEFI Secure Boot

Bootlace's usual way in is iPXE, and the iPXE that Bootlace builds itself is not signed by anyone a PC's firmware trusts (wimboot, which loads Windows' set-up environment, is signed by Microsoft, but it is started
by iPXE, which comes first). With Secure Boot **on** the firmware refuses it and the PC stops with `Security Violation`, so it never reaches the menu. There are four ways round that:

1. **Built in: the iPXE project's signed shim and iPXE, which are part of the program. The default of a new installation; nothing to download, no key to enrol in any PC.** Works for any Linux ISO whose kernel is signed. Next section.
2. **Your own key**: a folder with a distribution's shim and an iPXE you signed yourself; the key is enrolled once in each PC (further down). Chosen with one switch in the web interface.
3. A rule that starts **Windows** with the firmware's own signed boot manager (below).
4. The same for **Ubuntu** (below).

## Built in: the iPXE project's signed shim (the default; a real PC: works)

The iPXE project publishes a **shim signed by Microsoft** and an `ipxe.efi` signed by its own CA, which that shim trusts. Bootlace carries the two x86-64 files inside the program (`internal/secureboot/assets`,
release v2.0.0 of the project for `ipxe.efi` and the project's shim release `ipxe-16.1` for the shim (re-signed with both Microsoft CAs, see below), unchanged, with a `MANIFEST.json` that gives the source, the release, the checksum of the release archive and of each file; the program checks its own bytes against that manifest when it starts, and serves
nothing if they do not match). A PC with Secure Boot on starts the shim (the firmware trusts Microsoft), the shim starts `ipxe.efi`, and the usual Bootlace menu comes up: nothing has to be enrolled in the PC. Bootlace serves the two files by TFTP
(under `sb/`, only these two names) and, for a PC that is to use them, names the shim instead of its own `ipxe.efi` in the DHCP answer. Tested on a real MSI PC with Secure Boot on, in proxy-DHCP mode (the table below).

### What you do

- **A new installation:** nothing. It starts with the route **built in** and **for every 64-bit UEFI PC**: it works with Secure Boot on (measured on a real PC) and with it off (the shim then just starts iPXE; measured in a lab). The log says so at the start, and the Server page shows it.
- **An installation made by an older version:** nothing changes when the program is updated. Its settings file has no Secure Boot setting, which means **off**, and which PCs are sent is the marked ones only, as before. The Dashboard and the Server page show a banner, *Secure Boot support is built in: turn it on*, with a button;
  the log says it at the start. Turn it on there (or choose another way), and the banner goes. An installation that already used `-secure-boot-dir` keeps doing so: the option selects the own-key way, as before.
- **Switching** is done on the **Server** page, block **Secure Boot route**: *Way* (Built in, Own key, Off) and *Which PCs* (every 64-bit UEFI PC, or only the PCs marked on the PCs page). It takes effect for the next DHCP answer: no restart. The page asks before switching to your own key (enrolment) and before sending every PC through the shim.

### The options (they win over what the web interface saved)

| Option | Environment variable | What it does |
| --- | --- | --- |
| `-secure-boot builtin\|own\|off` | `BOOTLACE_SECURE_BOOT` | The way. Without it the choice made in the web interface counts. |
| `-secure-boot-scope all\|marked` | `BOOTLACE_SECURE_BOOT_SCOPE` | Which PCs go through the shim. |
| `-secure-boot-dir <folder>` | `BOOTLACE_SECURE_BOOT_DIR` | The folder of the own-key way. Giving it selects *own* unless `-secure-boot` says otherwise. Without it, *own* uses the folder `secure-boot` next to the settings file (`/var/lib/bootlace/secure-boot` in a Proxmox container, `/data/secure-boot` in the Docker image). |
| `-secure-boot-shim <file>` | `BOOTLACE_SECURE_BOOT_SHIM` | Own key: the file in that folder PCs are told to boot (default `shimx64.efi`). |
| `-secure-boot-all` | `BOOTLACE_SECURE_BOOT_ALL` | The same as `-secure-boot-scope all`. |

A setting that an option decides is shown in the web interface as such ("Set by the command line or the environment") and cannot be changed there; the API answers 409 for it. The saved choice is not overwritten by an option. A server started with no option and no settings file is the new-installation default.

The same facts are in the API (the session login and the `X-Bootlace: 1` header apply, as for the other pages):

- `GET /api/secureboot` (also the `secureBoot` field of `GET /api/status`): `on`, `mode` (`builtin`, `own`, `off`), `scope` (`all`, `marked`), `modeSource` and `scopeSource` (`option`, `dashboard`, `default`), `modeLocked`, `scopeLocked`, `unset` (nobody has chosen yet: the banner), `dir`, `shim`, `marked`, `files` (`name`, `role`, `required`, `present`, and for built-in files `size` and `sha256`), `ready`, `problem`, `message`, `builtin` (`ok`, `error`, `release`, `released`, `added`, `source`, `tarballSha256`, `files`) and `own` (`dir`, `dirFromOption`, `shim`, `files`, `ready`, `problem`).
- `PUT /api/secureboot` with `{"mode": "builtin"}`, `{"scope": "all"}` or both. 400 for a value that is not one of the choices, 409 for what an option decides, for *builtin* when the built-in files do not match their manifest, and for *own* when the shim is not in the folder. It answers with the new status.

### What each way means

- **Built in.** Works with the iPXE project's own shim (signed by Microsoft) and signed iPXE: no key to manage, nothing to enrol, nothing to download. It is the project's *reduced-feature* build: the project says that `ipxe.efi` is "missing support for several features that are forbidden in a UEFI Secure Boot build" ([ipxe.org/secboot](https://ipxe.org/secboot)), so Bootlace's own iPXE options (for instance the USB-driver switch in `tools/ipxe/config/usb.h`) are not in it. The Bootlace menu came up in the lab (with its picture) and on the real PC; a Linux ISO starts only if its distribution signed its kernel.
- **Own key.** Your own signed iPXE (`tools/ipxe/build-sb.sh`: built with iPXE's Secure Boot safe set, which Bootlace's picture, console and colour options pass) and a distribution's shim, from a folder. The key has to be enrolled once in every PC through MokManager (8 screens, below). You look after the key and the build's SBAT line.
- **Off.** Nothing under `sb/` is served and no PC is sent through a shim.

### Which PCs are sent through the shim

- **Every 64-bit UEFI PC** (*scope all*; the default of a new installation). Nothing has to be marked. It adds one boot stage (the shim) to every 64-bit UEFI PC, with Secure Boot on or off (the shim starts iPXE either way); that was fine in the tests below. BIOS PCs and ARM64 PCs are never sent to the shim, nor is a PC that already runs iPXE (it is given its script), nor a PC with a Windows or Ubuntu `native:` start in progress.
- **Only marked PCs** (*scope marked*; what an older installation has). On the **PCs** page press **Secure Boot route** on the row of a PC that has Secure Boot on. A PC appears in that list as soon as it has asked for a boot file, even
  if it never reached the menu (its row then says `Asked for a boot file; Secure Boot may be refusing iPXE`), so: boot the PC from the network once, it fails with `Security Violation`, refresh the
  PCs page, press the button, boot it again.

### What the log shows when it works

```
12:00:01  Secure Boot route: ON, built in (the iPXE project's signed shim and iPXE, release v2.0.0; the default of a new installation; change it on the Server page) for every 64-bit UEFI PC, which are told to start ipxe-shimx64.efi; the shim then asks for ipxe.efi
...
12:31:40  dhcp: discover d8:43:ae:d6:2f:05 from 0.0.0.0 answered with "sb/ipxe-shimx64.efi" to 255.255.255.255
12:31:41  tftp: 192.168.1.50 gets ipxe-shimx64.efi (Secure Boot route, built in)
12:31:41  tftp: 192.168.1.50:2074 asks for sb/ipxe-shimx64.efi (options map[...]), 1038920 bytes
12:31:42  tftp: 192.168.1.50 gets ipxe.efi (Secure Boot route, built in)
12:31:42  tftp: 192.168.1.50:2075 asks for sb/ipxe.efi (options map[...]), 397160 bytes
          (then the request for /boot.ipxe, and the PC's row on the PCs page shows the menu)
```

Both files are fetched, in that order, and then `boot.ipxe`. A DHCP line that names plain `ipxe.efi` instead means the PC was not sent through the shim (first row of the table below). The other start lines are
`Secure Boot route: OFF ...` (with why: not chosen yet on an older installation, chosen in the web interface, or set by an option), `Secure Boot route: ON, own key ...` and `Secure Boot route: ERROR, the built-in Secure Boot files do not match their manifest and are NOT served ...`.

### If it does not work

| What the log and the PC show | What it means | What to do |
| --- | --- | --- |
| The DHCP answer names plain `ipxe.efi` and the PC shows `Security Violation` | The route is off (`Secure Boot route: OFF` at the start; an older installation that has not chosen yet), or the scope is *marked* and this PC is not marked | Turn the route on in the Server page (Built in), and choose every 64-bit UEFI PC or mark the PC on the PCs page |
| The Server page shows the choices greyed out, *Set by the command line or the environment* | An option or environment variable decides | Change `-secure-boot...` / `BOOTLACE_SECURE_BOOT...` and restart, or remove it |
| `Secure Boot route: ERROR, the built-in Secure Boot files do not match their manifest` | The program file is damaged or was changed: the built-in files are not served | Install the program again from a release (checksums in `SHA256SUMS`, [verifying.md](verifying.md)) |
| The DHCP answer names `sb/ipxe-shimx64.efi`, the shim is fetched by TFTP, but `ipxe.efi` is never requested; the PC shows an error or goes on to the next boot device | The firmware refuses the shim | Check the board's Secure Boot mode (standard/"Windows UEFI mode", not custom with empty keys) and that **Microsoft's third-party UEFI CA** is not disabled (some boards have an option for it, "Allow Microsoft 3rd party UEFI CA"). To tell a firmware problem from a Bootlace problem, start a Linux live USB with Secure Boot on: if it does not start, the firmware does not trust the third-party CA. The shim carries both signatures (2011 and 2023), so what is missing is the CA itself: on the PC, in an administrator PowerShell, `$db = [Text.Encoding]::ASCII.GetString((Get-SecureBootUEFI db).Bytes)`, then `$db -match 'Microsoft Corporation UEFI CA 2011'` and `$db -match 'Microsoft UEFI CA 2023'`. When both say `False`, the PC trusts no third-party shim at all (an ASUS ExpertBook B3405CCA does not, measured 2026-10-31, and answers "Invalid signature"): allow the CA in the firmware if it has the option, use the own-key way, or the rule "Install <ISO>, once (works with Secure Boot)", which starts Windows' own boot manager |
| Both files are fetched, then `Security Violation` or `Verification failed` | The shim or `ipxe.efi` was revoked: the PC's revocation list (`dbx`, SBAT) refuses this build | See *Keeping the built-in files current*; try the own-key way, or switch Secure Boot off in that PC |
| Switching to *Own key* answers that the shim is not in the folder, or the Server page says a file is missing | The folder of the own-key way is empty or incomplete | Put the shim (and the signed iPXE) in the folder shown; the block turns green (it looks every few seconds) |
| The PC's row says `Asked for a boot file; Secure Boot may be refusing iPXE` and nothing else | The PC never reached the menu | One of the lines above; the row's History button shows when it asked |
| The PC's row says `This PC took the Secure Boot shim but never started iPXE` | The PC fetched the whole shim by TFTP and did not ask for `ipxe.efi` within 30 seconds: its firmware trusts only Windows' own boot code (no Microsoft third-party UEFI CA), so no shim can start. The row clears by itself when the PC reaches the menu | Give the PC the rule *Install <ISO>, once (works with Secure Boot)* (it starts Windows' own boot manager), or switch Secure Boot off for the session; there is no menu and no Linux on such a PC while Secure Boot is on |
| The menu comes up, an entry ends with `Could not boot` / `Security Policy Violation` | The shim works, the ISO's kernel is not signed (for example CachyOS) or it is Windows through wimboot | Use the Windows rule below for Windows; a distribution with an unsigned kernel needs Secure Boot off |

### Limits

Only the iPXE project's reduced-feature signed binaries are used in the built-in way, so Bootlace's own iPXE options are not in them; the project's signing policy for other parties is not stated in its pages (not asked). A Linux ISO starts if its distribution signed its kernel (the measured list is in the table below);
Windows keeps its own way (next section), although a Windows 11 ISO picked from the menu also installed on the real PC (table below). The files are for x86-64 UEFI only:

- **ARM64 is not included.** The same release has an `arm64-sb` folder (`shimaa64.efi`, 1,050,192 bytes, `ipxe.efi`, 438,120 bytes, `snponly.efi`). It is not built in because ARM64 PCs are not started by Bootlace yet: it has no ARM64 iPXE of its own to hand out (an ARM64 PC, architecture codes 11 and 19, gets no boot file), the shim route is chosen only for the x86-64 UEFI codes and an ARM64 PC is never sent the x86-64 shim (tested), and no ARM64 PC has been tried. Adding it needs the PC architecture to be checked on every answer, a boot file per architecture and a test on real hardware; it stays with issue #10.
- UEFI HTTP boot (architecture code 16) does not go through the shim; it was not tried.
- BIOS PCs have no Secure Boot and keep their file; nothing about Windows `native:` starts changes.

## Keeping the built-in files current

The files are one release of the iPXE project (`MANIFEST.json` and the Server page say which: release, date, and the checksum of the release archive). They are not renewed by themselves, and a program that is kept for a long time carries them as they were. **Why they have to be renewed from time to time:** PCs refuse programs that were revoked. The firmware has a revocation list (`dbx`, by hash and certificate), and the shim has an **SBAT** level (a generation number per component: this shim says `shim,4` and `shim.ipxe,1`, this `ipxe.efi` says `ipxe,1`) that operating system updates, Windows Update and the Linux distributions' shim updates raise on the PC. When a security fix in iPXE or in the shim raises the required generation, a PC that has taken that update refuses older builds ("Security Violation", "Verification failed") while PCs that have not yet taken it still start them: the failure appears PC by PC, over months. A newer shim release can also change the Microsoft signatures it carries.

To check whether a newer release exists and whether it matters:

1. Read the release notes of the iPXE project ([github.com/ipxe/ipxe/releases](https://github.com/ipxe/ipxe/releases)) and of its shim ([github.com/ipxe/shim/releases](https://github.com/ipxe/shim/releases)): look for security fixes, "SBAT", "revocation" and the signatures ("dual-signed with the 2011 and 2023 certificates"). The generations in the files shipped here are in their `.sbat` sections: the shim says `shim,4` and `shim.ipxe,1`, `ipxe.efi` says `ipxe,1`.
2. If there is a new iPXE release, run `tools/update-secure-boot-assets.sh <release> <sha256>` (for example `v2.0.0` and the checksum of `ipxeboot.tar.gz`). **Get the checksum from somewhere other than the download itself** (the release page, a second computer): the script prints the checksum it computed, and stops when it is not the one given. Without a checksum it refuses unless `--trust-printed-hash` is given, which accepts whatever was downloaded and writes its checksum into the manifest: use that only when you have looked at the release yourself. `--from <file>` uses an archive already on the computer; `--out <folder>` writes somewhere else to look first.
3. The script takes only the two x86-64 files out of the archive (`x86_64-sb/shimx64.efi`, written as `ipxe-shimx64.efi`, and `x86_64-sb/ipxe.efi`). **The shim of an iPXE archive can be older than the project's shim release**: the project replaced its `ipxe-16.1` release file on 2026-05-27 with a shim signed by both Microsoft CAs (2011 and 2023), while the v2.0.0 archive carries the build signed with 2011 only. `--shim-release ipxe-16.1 <sha256 of ipxe-shimx64.efi>` takes the shim from that release instead (the checksum is on the release page next to the file); the manifest then records the shim's release, address and the CAs found in its signature data (`signedWith`), and the Server page shows them. The script rewrites `MANIFEST.json`, and prints which checksums changed. Then: `go test ./internal/secureboot` (the manifest and the bytes agree), update `NOTICE` and `third_party/SOURCES` (release, checksums, shim version) and this page's measurements note, try it on a PC with Secure Boot on, and note the change in `CHANGELOG.md`.

The script has no PowerShell twin: the repository's tools are shell scripts, and the Windows tools in it only drive Hyper-V.

## Your own key (the folder way)

For when you want your own signed iPXE, a different shim, or the iPXE project's files of a release newer than the built-in ones. Switch to **Own key** on the Server page, or start with `-secure-boot-dir <folder>` (which also selects it). The folder has the shim and what it fetches; every file directly in it is served by TFTP under `sb/` by its own name, nothing else (no sub-folder, no path).

- **Your own key:** `tools/ipxe/build-sb.sh` builds iPXE with `SECUREBOOT=1` and signs it with your key; put the result in the folder as `grubx64.efi` next to a distribution's `shimx64.efi` and `mmx64.efi` (MokManager), and enrol the certificate in each PC once (the 8 screens in *Way 1* below). Server options: just the folder.
- **Another release of the iPXE project's files, by hand** (a Proxmox container made with the project's script, as root in the container: `pct enter <number>`; here `v2.0.0`, the version that is built in, as the example):

```sh
# 1. the iPXE project's signed files, checked before use
cd /tmp
wget https://github.com/ipxe/ipxe/releases/download/v2.0.0/ipxeboot.tar.gz
echo "01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1  ipxeboot.tar.gz" | sha256sum -c -     # must say: ipxeboot.tar.gz: OK
tar xzf ipxeboot.tar.gz ipxeboot/x86_64-sb

# 2. a folder for them; the shim is renamed so that its name says it is the iPXE project's
mkdir -p /var/lib/bootlace/secure-boot
cp ipxeboot/x86_64-sb/shimx64.efi /var/lib/bootlace/secure-boot/ipxe-shimx64.efi
cp ipxeboot/x86_64-sb/ipxe.efi    /var/lib/bootlace/secure-boot/ipxe.efi

# 3. tell the server (lines in /etc/default/bootlace; /etc/bootlace.env if the container was made by the community-scripts script)
cat >> /etc/default/bootlace <<'EOF'
BOOTLACE_SECURE_BOOT_DIR=/var/lib/bootlace/secure-boot
BOOTLACE_SECURE_BOOT_SHIM=ipxe-shimx64.efi
EOF
systemctl restart bootlace
```

(For another release replace `v2.0.0` and the checksum; if `tar` does not find `ipxeboot/x86_64-sb`, list the archive with `tar tzf ipxeboot.tar.gz`. Without the `BOOTLACE_SECURE_BOOT_DIR` line, the folder `/var/lib/bootlace/secure-boot` next to the settings file is the one used when *Own key* is chosen in the web interface; the shim's name is still given by `BOOTLACE_SECURE_BOOT_SHIM`.) The log says `Secure Boot route: ON, own key (...)`, and warns (`warning: ... has no ipxe.efi`) when the folder has the shim but not the file the shim asks for next; the start is not refused for that, because the folder is read at every request and the file can be copied in while the server runs. The Server page marks each file `present` or `missing`.

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
iPXE project's shim) a Windows 11 ISO was picked from the menu and its installation was run to the end; Windows started. Not tested: other firmware.

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

Status: the route has three modes (built in, own key, off) and two scopes (every 64-bit UEFI PC, marked PCs), described at the top of this page; a new installation has the built-in way for every PC, an older one is off until a person chooses (a server started with `-secure-boot-dir` is in the own-key way, as in 0.5.0-rc.1). Measured in a Hyper-V lab (Way 1 and Way 2) and on one real PC (Way 2 only, see the table). Issue #10.

The idea: with Secure Boot on the firmware only starts files signed by a key it knows, and iPXE is not one. A **shim** is: Microsoft signs the shims of the Linux
distributions, and a shim starts a second program (by default `grubx64.efi`, fetched by TFTP from the same folder) after checking its signature against its own keys and the
keys a person enrolled in the PC (the "MOK" list). So the DHCP answer names a shim, the shim fetches iPXE as its second stage, and the menu works as usual. What differs between
the two ways measured below is only whose signature the shim accepts on iPXE.

### How it is set up

1. **Files.** Built in: the two files of the program. Own key: **a folder** with the shim and what it fetches (`-secure-boot-dir <folder>`, also `BOOTLACE_SECURE_BOOT_DIR`; `-secure-boot-shim <file>` names the boot file, default `shimx64.efi`).
   Every file directly in the folder is served by TFTP under `sb/` by its own name, nothing else (no sub-folder, no path; unit tests). A folder given with the option must hold the shim, or the server refuses to start; one chosen in the web interface must hold it too, or the switch is refused.
   When the shim is there but the file it asks for next is not (`ipxe.efi` for a shim whose name contains `ipxe`, `grubx64.efi` for any other), the server **starts and warns** in the log and on
   the Server page: the folder is read at every request, so the file can be copied in while the server runs, and refusing to start would take the whole menu down for a folder that is only half filled.
2. **Which PCs.** The server cannot see a PC's Secure Boot state (the DHCP architecture code is the same with it on and off), so a person says it: the PCs page has a
   **Secure Boot route** button per PC (saved in the PC's options, `secureBoot`; scope *marked*), or the scope *all* sends every 64-bit UEFI PC through the shim. The shim also starts with
   Secure Boot off (measured: nothing had to be pressed), so "all" is possible, at the price of an extra boot stage for PCs that did not need it. BIOS PCs, iPXE itself (once it runs it is
   given its script, not the shim again), ARM64 PCs and a start in progress for Windows or FreeBSD (the `native:` rule) are never sent to the shim (unit tests).
3. **Kernels.** With the route on (built in or own key), every menu entry that starts a Linux kernel gets one more iPXE line before `boot`: `shim <ISO>/EFI/boot/bootx64.efi`, the ISO's own shim, read
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

### Way 2: the iPXE project's own signed shim and binaries (no enrolment; now built in)

The iPXE project (v2.0.0, 2026-03-06, release file `ipxeboot.tar.gz`, folder `x86_64-sb`) publishes a shim signed by Microsoft that trusts the "iPXE Secure Boot CA", and `ipxe.efi`
signed by that CA ([ipxe.org/secboot](https://ipxe.org/secboot)). Folder: the shim as `ipxe-shimx64.efi` (the shim takes the name of its second stage from its own name, minus
`shim`: it asks for `ipxe.efi`) and `ipxe.efi`. A fresh VM, nothing enrolled, no disk, **reached the
menu with Secure Boot on** and started Linux the same way. The price: only the project's reduced-feature binaries are signed ("you can't build your own iPXE with the features you need
in Secure Boot mode, and the page says that is intentional"), so Bootlace's own iPXE options (for instance the USB-driver switch in `tools/ipxe/config/usb.h`) are not in them, and the
project's signing policy for other parties is not stated in its pages (not asked). These are the files that are now built into the program.

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
(This was the folder route, with the files from the project's v2.0.0 archive, whose checksum is the one in the steps above; the built-in route serves the same two files, byte for byte, under the same names. The built-in route itself has not been run on a real PC yet.)

| What | Secure Boot | Outcome |
| --- | --- | --- |
| Plain `ipxe.efi` offered (route off) | on | stops: `Security Violation` |
| Way 2, nothing enrolled: the Bootlace menu | on | **works** |
| Ubuntu Server (chosen from the menu) | on | **booted** |
| Ubuntu 25.04 (chosen from the menu) | on | **booted** |
| Windows 11 ISO chosen from the menu (Setup to the end) | on | **worked**: Setup ran, the installation finished and Windows started (reported by the person who ran it; the entry was picked from the menu, not started with the `native:` rule) |

These were the first runs of the route on a real PC; one PC and one firmware say nothing about others (see the troubleshooting table above for what a different firmware may do).

Notes on what the table does and does not prove:

- The Linux ISOs started with the **distribution's own kernel, signed by that distribution**, through **that ISO's own shim**: nothing of theirs had to be enrolled. Where it works,
  the person sees only the extra screens above (Way 1, once) or nothing (Way 2). Which distributions work is therefore "those whose ISO has a shim and a signed kernel": measured
  Ubuntu, Linux Mint, Debian, Fedora, openSUSE (kernel reached); not measured: others. A distribution with an unsigned kernel (CachyOS, measured) does not start; the only way is
  to start that PC with Secure Boot off (or enrol that distribution's own key, not tried).
- **Windows through the lab is the open point, and the real PC answered it for one firmware.** The Windows boot manager of that ISO was refused ("Security Policy Violation") probably because this lab template trusts only Microsoft's third-party CA
  (the one that signs shims and wimboot) and not the Windows CA (the template with the Windows CA was not tried; the one other template tried trusted neither). The real PC, whose firmware has both, ran a Windows 11 ISO picked from the menu to the end (table above). The refusal of Way 1 (a crash of the shim instead of an error) differs from Way 2 (an error message); the cause was not
  investigated (a shim that regains control when the program it started exits is one guess, not checked). A `native:` rule (above), which the DHCP answer serves before the shim, stays the way that does not depend on the shim.
- `wimboot` itself **is signed by Microsoft** (the UEFI CA 2011; checked with `sbverify` on `internal/assets/ipxe/wimboot`), so the sentence "wimboot is not signed" that this page used to
  carry was wrong. What is unsigned is iPXE, which has to run first.
- **Not measured:** the built-in route on a real PC (the files are the ones measured, the code that serves them is new); a real PC with Way 1 or with another make of firmware (Way 2 was measured on one real PC, in proxy DHCP mode, above); a network whose own DHCP server names a boot file (from the shim's source (read, not run), the boot file and server are taken from the DHCP acknowledgement, and the proxy offer is used only when the acknowledgement names
  no boot file, so a network whose own DHCP server names one would send the shim there), HTTP boot, ARM64, MokManager when no key is pressed,
  "Enroll hash from disk", firmware-level enrolment, Windows with both Microsoft CAs trusted in a lab, Fedora's and Ubuntu 26.04's shims as first stage, the "as a disk" starts and
  the "other ways" menu under Secure Boot (their entries do not get the `shim` line yet), a distribution's kernel that needs its own key enrolled, a PC that trusts only Microsoft's 2023 UEFI CA (the built-in shim carries both signatures since 0.5.2; untested on such a PC: the one PC that refused it, the ExpertBook, trusts neither third-party CA), any speed or reliability figure.

### Risks and open questions

- **Revocation.** Shims and firmware refuse programs by hash (the `dbx` list) and by SBAT generation; the shims measured carry `ipxe,1` as the accepted generation of iPXE. A security
  fix in iPXE raises that number upstream, and then every older build, ours included, would be refused by updated PCs: builds must follow upstream, and our own line
  (`ipxe.gola`) is the lever for the cases upstream does not cover. For the built-in files, *Keeping the built-in files current* above is the procedure. A shim that Microsoft later revokes stops the whole route on updated PCs. The built-in shim is the project's `ipxe-16.1` release file of 2026-05-27, signed with both Microsoft UEFI CAs, **2011 and 2023** (the v2.0.0 archive carries the earlier build with 2011 only, which a firmware that trusts only the 2023 CA refuses; the ASUS ExpertBook B3405CCA refused the 2011-only shim, probably for that reason, which was not confirmed by reading its key database). `go test ./internal/secureboot` checks that the bytes carry both certificates. **Not yet measured on a PC that trusts the 2023 CA alone**, and not measured that the 2023 signature is what that PC accepts: the lab's firmware trusts the 2011 CA, so a lab run only shows that the new shim still starts `ipxe.efi`.
- **Key custody (Way 1).** The signing key is a Secure Boot key for every PC that enrolled it: anyone who has it can sign code that those PCs run. It must not be in the repository, in CI
  or on the server; a hardware token or an offline machine. If it leaks, every PC has to remove it (MokManager again). This is the owner's decision; the lab key was thrown away.
- **Which shim to ship.** The built-in shim and iPXE are the iPXE project's own files, unchanged; their licences and source are in `NOTICE` and `third_party/SOURCES` (iPXE: GPL, mostly GPL-2.0-or-later or the project's Unmodified Binary Distribution Licence; the shim: BSD-2-Clause with OpenSSL 1.0.2k and gnu-efi parts; the exact commit the shim was built from is not recorded by the project, see NOTICE). The distributions' shims are
  not shipped: the own-key folder is filled by hand from an ISO.
- **A signed iPXE is a trusted program.** With `SECUREBOOT=1` its build refuses the features that can start unsigned code; a build without that flag must never be signed.
