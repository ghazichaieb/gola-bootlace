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

## Everything else, with Secure Boot on

Not yet. Options (issue #10): a Microsoft-signed shim with our key enrolled on each PC once (iPXE and all the kernels then work, with one extra screen per PC), or the same
"firmware starts a vendor-signed chain" idea for Ubuntu (its shim + GRUB + kernel are signed by Canonical). iVentoy offers both as its Standard and ByPass modes.
