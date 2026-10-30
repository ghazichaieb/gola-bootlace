# The web interface

Bootlace serves a management page at `http://<server>:8080/admin/` (the address of the server itself, `http://<server>:8080/`, leads there). PCs that
start from the network never need it; they only use the boot menu.

## Password

The first time the server starts without a password it makes one and writes it once in its log, in a line `web interface: the password was made up: ...`. Where to look:

| Installed as | Where the password is |
|---|---|
| Proxmox container (the community-scripts script or `tools/proxmox/bootlace-lxc.sh`) | shown at the end of the installation; later `pct exec <number> -- journalctl -u bootlace \| grep -i password` |
| Docker | `docker logs bootlace 2>&1 \| grep -i password` |
| A service on a Linux machine | `journalctl -u bootlace \| grep -i password` |
| Started by hand | printed in the terminal |

Change it under **Account** as soon as you are in. **If it is lost**, give a new one at the start: set `BOOTLACE_ADMIN_PASSWORD` (or start with `-admin-password`) and restart the server; a password given at the start replaces the saved one. In a Proxmox container, add the line `BOOTLACE_ADMIN_PASSWORD=<new password>` to `/etc/default/bootlace` (`/etc/bootlace.env` if it was made by the community-scripts script) and run `systemctl restart bootlace`.

It is kept only as a salted PBKDF2 hash in the settings file. Change it under **Account**, or set it from the command line (`-admin-password`, or the
environment variable `BOOTLACE_ADMIN_PASSWORD`; the one given at the start wins). Five wrong passwords from one address lock that address out for five
minutes. Anyone on the network can reach the login page, so use a password you would not mind defending; `-no-admin` switches the interface off.

## Pages

| Page | What it does |
|---|---|
| Start here | (only on a new installation) four short steps: your own password, the library, who may use the server, and the checks |
| Dashboard | ISOs, PCs seen, how long the server has run, the addresses PCs use, recent activity |
| Server | the library (the folder with the ISOs): choose it with a folder picker, see its state and free space; the services PCs talk to (boot menu, Windows file share, iSCSI, network disk, TFTP, PXE) with their state and any error, start and stop them (with a warning when PCs are using the server), and whether they start by themselves when the program starts; the cache (with its entries, oldest first, and what is kept); who may use the server; a **self-test** that asks the server's own address for what a PC asks for, a **look for other PXE/DHCP servers** on the network, a **library speed test**, and a **backup of the settings** (download, restore, and the copy kept in the library) |
| Library | the files of the library: upload, add from a web address (with a checksum), copy-in detection, rename, move, new folder, delete, who is using what, the checksums |
| ISOs | (also lists disk images: .img and .raw, packed or not) rename an ISO in the menu, hide it, choose what starts by itself (one of the ISOs, the PC's own disk or nothing), extra kernel arguments, *verbose start-up*, *serial console* and *debug shell* switches, the exact commands a PC gets, a **password** the menu asks for before the ISO starts, look inside the ISOs again |
| PCs | every PC that asked for a menu (name from its firmware, MAC, address, UEFI or BIOS, what it did last) and a name of your choice for each (kept with the MAC address; when the PC offers a host name in its DHCP request, or an asset tag in its SMBIOS, one click takes it as the name), and a rule for each: show the menu, boot from its own disk, or start one ISO without a menu; a **Wake** button (Wake-on-LAN) and a **History** of what the server saw (also as a CSV for every PC). Rules can also be added by MAC address for a PC that has not started yet |
| Menu | heading, seconds before the default choice starts, what starts by itself (nothing, the PC's own disk, an ISO), with a preview; **Tuning**: patterns that keep ISOs out of the menu, the colour of the highlighted line, the largest TFTP block and the blocks sent ahead |
| Log | the last 2000 lines of the server log, live, with a filter, and a button that downloads the whole log (see `-log-file` for a log that survives a restart) |
| Account | change the password |

Everything applies at the next start of a PC: the menu is made again for each one.

Verbose start-up also removes valued splash options such as `splash=silent` and replaces other kernel log levels with `loglevel=7`, for both ISO and PC options; it opens no shell. The **debug shell** switch is separate from verbose start-up, which never opens a shell. It adds the word that makes the start-up of the kind of system stop at a shell before it goes on, so that a person can look around (network, disks, logs); each word was checked on a real virtual machine started through Bootlace: `proxdebug` for Proxmox VE ("Debugging mode": type `exit` or press Ctrl+D and the installer goes on), `rd.break=pre-mount` for dracut systems (Fedora, Red Hat's installers, openSUSE: dracut's emergency shell, leave it with `exit` or Ctrl+D), `break=premount` for Debian's live ISOs and Ubuntu (an `(initramfs)` prompt before the live system is mounted: type `exit` to go on). Systems without such a word (Alpine, Arch, SystemRescue, Debian's installer, Tails, Windows, FreeBSD) ignore the switch. It works alone or together with the other two, for an ISO (ISOs page) and for a PC (Kernel options).

## Settings file

Settings made in the interface are kept in the file given by `-config` (default `bootlace.json`; `/var/lib/bootlace/config.json` in the Proxmox
container). The first time, the file is made from the command-line menu options (`-menu-timeout`, `-menu-default`, `-menu-local`, `-menu-pick`); after
that the file wins and those options are ignored. Addresses, ports and the DHCP mode stay on the command line.

## How it works

DHCP still points PCs at `/boot.ipxe`, which only tells iPXE to ask for `/menu?mac=...&ip=...` with what it knows about itself (MAC, address,
UEFI or BIOS, maker and model from SMBIOS). `/menu` writes the menu for that PC from the ISO folder, the settings and the PC's rule. The API under
`/api/` is JSON; every request except the login needs the session cookie, and every change also needs the header `X-Bootlace: 1`, which a page on
another site cannot add. The pages are one file built into the program (`internal/admin/web/index.html`).

## The boot screen

A UEFI PC shows the Gola Bootlace logo with the menu on top of it (iPXE's graphical console, a 1024 x 768 picture made by the server at `/bg.png`). A BIOS PC, or any PC whose screen cannot do that, gets the same list as plain text under the heading "Gola Bootlace". The heading is not a setting: the logo is always the heading. Notes from testing: iPXE refused PNG files written with Go's default compression level but accepted the best-compression level, and it shows a gradient as bands, so the picture is flat.

## The list of PCs

The list records at most 200 PCs. At capacity, a new MAC address replaces the least recently seen PC without an administrator-assigned name, per-MAC rule, or non-empty kernel/answer options. Firmware host names and asset tags do not protect a record. If all 200 records are protected, new PCs still receive their menu and rules but are not added to the list; existing PCs continue updating. Removing those settings makes a record eligible for capacity eviction again. This protects configured history from random-MAC floods, but does not authenticate a MAC address. Explicit Forget actions and the age-based cleanup below still apply.

- It is kept in `pcs.json` next to the settings file, so a restart does not empty it.
- A PC's address is the one its request came from.
- A PC is listed as soon as it asks for a boot file (the DHCP answer), even if it never loads the menu. One that did not (`count` 0) carries a `hint` in `/api/pcs`, shown under its name: `Asked for a boot file; Secure Boot may be refusing iPXE.`, with what to do (turn the Secure Boot route on, or press "Secure Boot route" on its row). Its History shows each time it asked.
- The page refreshes itself every 5 seconds, except while a choice inside the table (a rule's drop-down) has the focus: redrawing it would close it. Typing in the search box or changing the page size does not stop it.
- The page is paged (10, 25, 50 or 100 per page; the choice is remembered in the browser, and the first guess comes from the window's height, because the number that fits cannot be known) and has a search box. `/api/pcs` takes `limit`, `offset` and `q`.
- "Forget" removes one PC, "Forget them now" removes the PCs not seen for N days, and a PC is forgotten by itself after `pcDays` days without a start (default 90, 0 keeps all). Rules are never removed by this.
- **Kernel options** (button on each PC; `PUT /api/pcs/{mac}/options` with `{"args": "...", "debug": {"verbose": true, "serial": false, "shell": false}}`): extra kernel arguments and the *verbose start-up*, *serial console* and *debug shell* switches for that PC only. They come after the arguments of the ISO itself, on the kernel line of every ISO that PC starts from its own files (not Windows, not a disk image; the pages of the menu carry the PC's address, so they apply on every page and to a PC's rule too), from the PC's next start. The settings file keeps them under `pcOptions` by MAC address (they are in backups), and a PC that has them is listed even when it has not asked since the server started. Empty arguments and all switches off remove the entry. *Verbose start-up* adds, besides `loglevel=7 ignore_loglevel debug`, the words that the start-up of the kind of system honours: `rd.debug` for dracut (Fedora and what is built on it, Red Hat's installers, openSUSE, Gentoo), `systemd.show_status=1` for the systemd live systems (Fedora, openSUSE, Arch, SystemRescue, Debian, Ubuntu and their relatives), `debug_init` for Alpine (none of them opens a shell: that is the *debug shell* switch). A kind without a word of its own (Tiny Core, ShredOS, Tails) gets only the general ones.

## Other ways to start an ISO

When the boot menu of an ISO has entries besides the usual start that Bootlace can start the same way, the PC's menu gets the line "Other ways to start an ISO >>" (UEFI and BIOS). It opens
the list of those ISOs, and an ISO opens its entries: "The usual start" first, then the ISO's own entries in the order its menu gives them, the ordinary ones before safe graphics, a
check of the disc and such. The ISO's boot password, its extra arguments and the PC's own options apply to every one. The ISOs page lists the entries of an ISO under "other ways to start
it". They are looked for in the background after the server starts or the library changes ("Look again" on the ISOs page makes it forget them), so a new ISO shows its ways after a minute.

## Unattended installs

The **Unattended** page keeps *answer files*, the files an installer reads so that it asks nothing: Debian's preseed, Ubuntu Server's autoinstall (cloud-init `user-data`), Red Hat's kickstart and
openSUSE's AutoYaST. "New answer file" starts from a template for the kind; each template says at the top that **it erases the disk** and has a password to change.

- **Attaching.** Nothing is attached by itself. Choose a file for an ISO on the ISOs page ("Unattended install"), or for one PC with *Kernel options* on the PCs page ("Follow the ISO's setting",
  "No answer file for this PC" or a file); a PC's own choice wins over its ISO's. Attaching asks for a confirmation, because every PC that starts that ISO from the menu (or by a rule) then
  installs without asking.
- **How the installer gets it.** The kernel line of the ISO gets the argument of the kind (`auto=true priority=critical cdrom-detect/retry=false url=...` for preseed, `autoinstall ds=nocloud-net;s=...`
  for autoinstall, `inst.ks=...`, `autoyast=...`), placed before a `---` where the line has one. The address is `/answers/<file id>/<MAC>/<name>` on the server: the id cannot be guessed and the file is only
  given while it is attached to an ISO or a PC, but **an installer cannot log in, so the file is not secret from the local network** (see the next point for what narrows that, and what it does not).
  Like the rest of the server it is only given to the networks that may use it (Server > Access).
- **Who can fetch it (issue #60).** The address has no password, so Bootlace narrows it: a file is given only to a MAC address that **asked for the menu in the last two hours** (an ISO's start
  fetching files keeps the two hours open), and only **twelve files after each time that PC asked for the menu** (Ubuntu fetches three files and may ask for each again, Debian reads its preseed
  twice, the others once); after that the address answers "not found" until the PC asks for the menu again. A refusal says why in the Log ("that PC never asked for the menu", "asked for the menu too
  long ago", "was already given 12 files") and the requester only sees "not found".
  **The network address is not part of the default check**, although it was the first idea: in a real run (Ubuntu Server 26.04.1 in a UEFI VM on a home router) iPXE had one address and the installer,
  which identifies itself to DHCP differently, was given the next one, so a file that required the same address was refused to the PC it was meant for. The switch at the bottom of the Unattended page
  ("Stricter") adds that condition for networks where a PC keeps its address; a file fetched from another address than the menu's is noted in the Log either way.
  **What this stops:** a computer that never asked for a menu cannot read a file, and a file does not stay readable for ever. **What it does not stop:** any computer of the local network can ask
  the menu with the MAC address of a PC and then read the file (once for that MAC and menu request, a dozen times at most); the log shows a menu request for one MAC from two addresses when it happens.
  This is a limit of an installer that cannot log in, not something a setting removes: keep a password hash in the file, and the server on a network you trust.
- **A warning for plain passwords.** Saving a file, listing it and attaching it say so when a line holds a password in plain text for its kind (`d-i passwd/user-password password ...`, autoinstall
  `password:` or `plain_text_passwd:` or a `chpasswd` list entry, kickstart `rootpw`/`user --password=` without `--iscrypted`, AutoYaST `user_password` of a user that is not marked encrypted); a hash,
  a locked password, a placeholder of a template or a `{{word}}` is not one. The warning names the line, never repeats the password, and refuses nothing.
- **One file for many PCs.** In the text, the words `hostname`, `name`, `mac`, `mac-dash`, `ip` and `serial` between double curly braces are replaced for the PC that asks: `hostname` is the name given
  on the PCs page made into a host name, or `pc-` and the last six digits of its address.
- **Passwords.** Put password *hashes* in a file, not passwords (the templates ask for hashes: `openssl passwd -6` or `mkpasswd -m sha-512`, or an SSH key), because of the point above. A file can hold secrets. They are shown as `********` on the page and in the API unless "Show the passwords" is ticked; a file saved with the stars left as they were keeps its
  values, and a save that would turn a value into the stars is refused. The files are kept in the settings file (so not in a backup made without secrets, nor in the copy beside the library), and
  the log only says which file was given to which PC, never its text.
- **What was checked.** Ubuntu Server 26.04.1 installed by itself in a UEFI VM from an autoinstall file (the disk was partitioned, the system installed and it booted under the PC's `hostname`).
  Debian's preseed was run to the end with the Debian 13.7 and 12.15 netinst ISOs and Kali 2026.2 (UEFI VMs; the installer finds its CD over iSCSI, see [compatibility](compatibility.md)): installed and booted without a question. The iSCSI server must be on. For an attached preseed, Bootlace now passes `hostname=<name> netcfg/hostname=<name>` before `---`, using the same sanitized PC name or MAC-based fallback as `{{hostname}}`; this also overrides hostnames from DHCP. An explicit `hostname=`, `netcfg/get_hostname=` or `netcfg/hostname=` in the ISO or PC kernel options takes precedence. Other network answers inside a network-fetched preseed still arrive too late: put them on the kernel line. This hostname fix is covered by command-generation tests, not a new full installer run. The arguments of the other kinds are right for their installers but were not run to the end (kickstart and AutoYaST were not tried). A Windows `autounattend.xml` is not part of this: Windows Setup still takes the file that
  sits in the ISO's own folder.
- **Ubuntu Server** ISOs are now downloaded whole into the PC's memory (the PC needs about the size of the ISO and 2 GB more) instead of being read from the network disk: Ubuntu's installer stops on
  the network disk with "Invalid dep_id (disk-nbd0)".
- **Windows (autounattend.xml, #17).** The "Windows autounattend.xml" kind works for Windows 10, Windows 11 and Windows Server ISOs. Windows Setup has no web client, so the file is read from the share: while
  a Windows answer file is attached to the PC or to its ISO, the ISO's folder on the share shows `_bootlace\<MAC>\autounattend.xml` for that PC (made for it when it is read, with the same words
  as the other kinds plus `{{computername}}`, the host name cut to Windows' 15 characters), and the start-up script of the Windows set-up environment (`bootlace.cmd`) finds its own MAC addresses with
  `ipconfig`, copies the file and starts Setup with `/unattend:`. The same rules as for the web address apply (it is given only to a PC that asked for the menu, or was started by a "native" rule, in the
  last two hours, and twelve times at most; the share cannot tell which address a PC reads from, so the "Stricter" switch does not apply to it). A file attached here wins over an `autounattend.xml`
  that sits in the ISO's own folder, which keeps working when nothing is attached. Windows keeps a password in this kind of file as plain text (the base64 form it can hold is no protection), so the page
  warns about any `<Password><Value>` and the template creates no account. Words put into the XML are escaped. **Checked in a real Windows 11 26H2 Setup** (Hyper-V, UEFI, 2 GB, no TPM): a PC
  with a file attached got it through the share, Setup ran without a question (disk partitioned, edition chosen, account made, computer name `SHOP-PC-7` from the name given on the PCs page, first-logon
  command ran). Two things the run showed: Windows reads the folder that holds a file before it copies it, so the share shows the folders `_bootlace` and `_bootlace\<MAC>` too; and the set-up image of
  Windows has no `curl`, `getmac`, `wmic`, `powershell`, `certutil` or `findstr`, which is why the file travels over the share.

## Windows Setup options

On the Menu page, under Tuning, three groups of ticks change how a Windows ISO is installed through Bootlace (they apply at the next start of the PC; the ISO is never changed).

- **Skip these checks (Windows 11).** `TPM 2.0`, `Secure Boot`, `4 GB of memory`, `Supported processor` and `64 GB of storage` write the matching `HKLM\SYSTEM\Setup\LabConfig` `Bypass...Check` value from the start-up script before Setup starts. Seen on Windows 11
  26H2 (build 26300) in a VM without a TPM, with 2 GB and a 40 GB disk ([compatibility](compatibility.md#windows-11-setup-checks-answer-files-and-drivers-2026-10-25-hyper-v-gen-2-windows-11-26h2-build-26300-9457)):
  without the TPM tick Setup stops with "This PC can't run Windows 11"; with it Setup goes on. The memory and storage ticks changed nothing visible there, and the Secure Boot and processor ticks could not be tried on that machine.
- **Local account at the first start.** Windows 11 26H2 asks for a network at its first start and gives no way past that page (no "I don't have internet" link; the old `OOBE\BYPASSNRO` command is gone, and its registry value, written before the first start,
  is ignored). What works is `HideWirelessSetupInOOBE` in an answer file: the first start then goes straight to "Who's going to use this device?" and asks for a name and a password for a local account. With this tick on, Bootlace gives Setup
  a small answer file with that setting (and `HideOnlineAccountScreens`), only when no Windows answer file is attached to the PC or its ISO and the PC was not set to "No answer file"; every page of Setup is still asked as before (the answer file
  also says the product key page is to be shown, because Setup stops with "Windows cannot read the <ProductKey> setting" when an answer file has neither a key nor that). In your own Windows answer file put
  `<HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>` (and, to leave out the Microsoft account pages, `<HideOnlineAccountScreens>true</HideOnlineAccountScreens>`) inside `<OOBE>` of the `oobeSystem` pass.
  Checked in a real 26H2 first start with no network: the page was skipped and a local account could be made.
- **Drivers.** With the tick on, driver packages kept in the library are added to the copy of Windows' start-up image that the server hands out, and loaded before Setup starts, so that Setup sees a disk or a network card that Windows does not know. Put each driver
  package (the folder with its `.inf`, `.sys` and `.cat`) in `drivers/all/` of the library for every Windows ISO, or in `drivers/<the ISO's file name without .iso>/` for one ISO; the Menu page lists what it finds. The packages are copied into the image at `\$WinPEDriver$`,
  which Windows Setup also takes into the installation it makes (seen: after an install the driver store of the new Windows held the package, on a machine that has no such hardware). The start-up image is held in the PC's memory, so at most 1024 MB of drivers are added. The ISO itself is never changed (the copy is made with wimlib and kept in the cache; a
  change in the folder makes a new copy). A copy is made on first use, which takes a few seconds for the 600 MB image. The same copy serves the Secure Boot start.
  A network card is the usual case on a recent PC: Windows 11's set-up image does not carry the driver of every chip, and then Setup cannot reach the share (the screen says so and lists the devices without a driver, with their `PCI\VEN_` and `DEV_` numbers). Take the maker's driver pack (for Intel cards the "Ethernet Adapter Complete Driver Pack"), keep only the 64-bit Windows 10/11 folders, and put them in `drivers/all/<name>/` (or in the folder of one ISO). Seen on a real PC: the newer "W11" driver of the Intel pack loaded but passed no traffic, the older one in `Winx64/NDIS68` worked ([compatibility](compatibility.md#real-pcs)).

## Long menus

The menu on the PC is shown a page at a time when there are more ISOs than fit: 12 per page on a UEFI PC (the picture leaves 24 text lines) and 8 on a BIOS PC (80 x 25), with "Previous page" and "Next page" lines and "page 2 of 3" in the heading. Flipping pages is not counted as a new start in the PCs list, a timeout never fires while someone is looking through the pages, and the menu opens on the page that holds the entry that starts by itself. A BIOS PC gets a minimal list on black instead: the ASCII lace mark with the name beside it, a line "ISO Boot Menu", one row per ISO (size, name, kind of start), a line "Tools" and the three fixed lines, with Gola's red for the logo, the lines and the chosen row. 8 ISOs fit a page. iPXE's BIOS build cannot draw a picture in the VMs tried (the VESA picture path failed in Hyper-V's BIOS VM), block characters and large letters showed badly on a BIOS text screen, and the blanks that line things up are the byte 0xFF, because iPXE joins words with single spaces.

## The library and the services

The ISOs live in the **library**: one folder that has to be readable and writable (a folder on the computer, or a network drive mounted on it). Choose it under
**Server**. A folder that is not a library yet becomes one when you say so: Bootlace puts a hidden folder `.bootlace` in it, with a small marker file, and
writes nothing else without being asked. `-iso-dir <folder>` on the command line adds the library the first time the program starts; after that the one chosen
in the web interface is used.

The marker is what makes the folder *the* library. When the folder is not there, or has no marker (a network drive that is not mounted is just an empty folder
on the computer's own disk), the library counts as not available: nothing is served and nothing is written there. The server stops after 30 seconds of that (a
network drive that stalls for a moment does not end the installs that are running) and starts again by itself when the library is back.

The server is the group of services the PCs talk to. **Stop the server** closes them all (the web page stays up); the page lists who is connected and asks
before it interrupts anyone. A service that cannot start (another program holds its port) is shown with the reason and the others keep running.

The **Secure Boot route** block (Server page, next to the services; `GET /api/secureboot`, also the `secureBoot` field of `/api/status`) is where the route for PCs with UEFI Secure Boot on is chosen and checked. *Way*: **Built in** (the iPXE project's signed shim and iPXE, part of the program), **Own key** (a folder with a distribution's shim and an iPXE signed with your key) or **Off**; *Which PCs*: every 64-bit UEFI PC, or only the PCs marked on the PCs page (with their number). A short explanation of each way is under the control, and the page asks before it switches to your own key (the key has to be enrolled in every PC) or to every PC. A change counts from the next DHCP answer, with no restart. Under the controls: which files are served (the built-in ones with size and checksum and the iPXE release they come from; for the own key the folder and whether the shim and the file it asks for next are present, looked at every few seconds) and one sentence about what is missing. A setting that the command line or the environment decides is greyed out and says so. An installation from before the built-in files existed shows the banner *Secure Boot support is built in: turn it on* here and on the Dashboard until a way is chosen.
The API: `GET` gives `on`, `mode` (`builtin`, `own`, `off`), `scope` (`all`, `marked`), `modeSource`/`scopeSource` (`option`, `dashboard`, `default`), `modeLocked`, `scopeLocked`, `unset`, `dir`, `shim`, `marked`, `files` (`name`, `role`, `required`, `present`, `size`, `sha256`), `ready`, `problem`, `message`, `builtin` (`ok`, `error`, `release`, `released`, `added`, `source`, `tarballSha256`, `files`) and `own` (`dir`, `dirFromOption`, `shim`, `files`, `ready`, `problem`). `PUT` takes `{"mode": ..., "scope": ...}` (either or both): 400 for a value that is not a choice, 409 for what an option decides, for the built-in files when they do not match their manifest, and for *own* when the shim is not in the folder. The PCs page's `secureBoot` field is `""` (route off), `pcs` or `all`. See [secure-boot.md](secure-boot.md).

## Who may use the server

Every service answers the local network only: private addresses (10.x.x.x, 172.16-31.x.x, 192.168.x.x and the IPv6 equivalents), the computer itself and
link-local addresses. A request from any other address is refused (the web page answers 403, the other services close the connection or stay silent), counted
under **Server > Who may use the server**, and written to the log once an hour for each address. Extra networks (a VPN's, say) are added there, one per line;
a network that is most of the internet is refused, and "answer addresses from anywhere" is a separate switch that asks for a confirmation. The server shows a
warning when its own address is a public one.

This is a guard against a port forwarded by mistake, not a security boundary: inside the local network there is no login on the network disk and iSCSI, and
the web page is plain HTTP. Do not expose the server to the internet; use a VPN to reach the local network instead.

## The Library page

The files of the library, managed from the web page. Everything here works on the library folder only.

- **Look and move around.** Folders and files, with size, date, the SHA-256 and, for a file that was imported, where it came from. A file a PC is using right now is marked "in use".
- **Add files.** *Upload files* (or drop files on the list) sends them from the browser; a file is written to disk as it arrives, whatever its size. *Add from a web address* makes the server download the file itself, with progress, a speed limit while PCs are installing, and a retry from where it stopped when the connection breaks. Both can be given a checksum (SHA-256, or SHA-512 for an address; for an address also the link to a `SHA256SUMS` file), which is checked at the end.
- **Copy files in by hand.** Files put in the library's folder with any other tool appear by themselves; a file that is still being copied (changed in the last 15 seconds) is left alone until it is finished, and its checksum is worked out in the background while no PC is installing.
- **Rename, move, new folder, delete.** Renaming or moving an ISO keeps its name in the menu, its hidden flag, its extra arguments, the rules that start it and the default. A file that a PC is using asks first. Delete is never recursive: it removes a file that is an ISO or a disk image, or an empty folder, and nothing else (a file such as `notes.txt` is shown but not offered for deletion).

**What is refused.** A file name that does not end in `.iso .img .raw .xz .gz .zst .bz2 .zip`; a file whose first bytes are not what its name says (a web page saved as `x.iso` is refused as soon as its start has arrived, not after the whole download); a name that is already taken (nothing is ever replaced); a file that would leave less than a reserve of free space (1 % of the disk, between 512 MB and 5 GB); a path that leaves the library or reaches its hidden `.bootlace` folder, or goes through a link.

**Repeats.** Before a download or an upload, and again when it ends, the library says when the file looks like one that is there: the same name, the same address, the same size under the same name, or the same content (checksum). The person decides; a file with the same *name* is never replaced.

**Web addresses.** Only `http` and `https`, without a user name or password in them. The server never connects to an address on a local network (private ranges, loopback, link-local such as the cloud metadata address, carrier-grade NAT, multicast and the IPv6 equivalents): the check is made when the connection is made, on the address actually used, so a redirect or a name with several addresses cannot get around it. Files on a local network are copied into the library's folder instead.

## The cache

Zstandard (`.zst`) image extraction accepts frames with a decoding window up to 128 MiB and uses one decoder worker, and XZ (`.xz`) extraction refuses a file whose first block declares a dictionary above 128 MiB (the XZ tools write 8 to 64 MiB; `xz -9` writes 64 MiB). Images may unpack to much more than 128 MiB: the bound applies to the decoder history or dictionary, not the output file. A file asking for more fails with an unpacking error that says so, and its partial output is removed; repack with a smaller window (`xz --lzma2=dict=64MiB`) or decompress it outside Bootlace. For XZ only the first block's header is read: a file that declares a small dictionary first and a huge one in a later block is not caught (the XZ tools write the same size in every block). This does not bound every allocation.

While unpacking a compressed image, the server checks free space on the disk that holds the chosen cache folder before each write. It stops and removes the partial copy if the write would use the disk's reserve (1 % of the disk, at least 512 MB and at most 5 GB), or if free space cannot be checked. The error is shown in the menu and the server log. This protects the disk even when a packed image expands far beyond its compressed size; it does not enforce the container cache's configured size limit during unpacking or move an unfinished copy to another cache folder.

What the server makes once and keeps: the unpacked copies of packed disk images, and the copies of Windows' `boot.wim` with Bootlace's files in them. Under **Server > Cache**, the container's own folder is used while it has room (2 GB by default); after that new entries go to `<library>/.bootlace/cache`. An entry is looked for in both places. The size of the container's folder bounds how many different installs can be prepared at once; the library is slower (it may be a network drive) but roomy. A copy of `boot.wim` contains the share's password, so anyone who can read the library's folder can read it.
