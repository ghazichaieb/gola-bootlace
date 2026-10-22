# UEFI Secure Boot

Bootlace's usual way in is iPXE, and iPXE (like wimboot, which loads Windows' set-up environment) is not signed by anyone a PC's firmware trusts. With Secure Boot **on**
the firmware refuses it, so the PC never reaches the menu. There are two ways round that; this page says what works today.

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

## Everything else, with Secure Boot on

Not yet. Options (issue #10): a Microsoft-signed shim with our key enrolled on each PC once (iPXE and all the kernels then work, with one extra screen per PC), or the same
"firmware starts a vendor-signed chain" idea for other distributions that sign their shim, GRUB and kernel (Debian, Fedora, openSUSE, ...). iVentoy offers both as its Standard and ByPass modes.
