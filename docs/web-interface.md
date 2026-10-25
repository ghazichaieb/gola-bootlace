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
| ISOs | (also lists disk images: .img and .raw, packed or not) rename an ISO in the menu, hide it, choose what starts by itself (one of the ISOs, the PC's own disk or nothing), extra kernel arguments, *verbose start-up* and *serial console* switches, the exact commands a PC gets, a **password** the menu asks for before the ISO starts, look inside the ISOs again |
| PCs | every PC that asked for a menu (name from its firmware, MAC, address, UEFI or BIOS, what it did last) and a name of your choice for each (kept with the MAC address; when the PC offers a host name in its DHCP request, or an asset tag in its SMBIOS, one click takes it as the name), and a rule for each: show the menu, boot from its own disk, or start one ISO without a menu; a **Wake** button (Wake-on-LAN) and a **History** of what the server saw (also as a CSV for every PC). Rules can also be added by MAC address for a PC that has not started yet |
| Menu | heading, seconds before the default choice starts, what starts by itself (nothing, the PC's own disk, an ISO), with a preview; **Tuning**: patterns that keep ISOs out of the menu, the colour of the highlighted line, the largest TFTP block and the blocks sent ahead |
| Log | the last 2000 lines of the server log, live, with a filter, and a button that downloads the whole log (see `-log-file` for a log that survives a restart) |
| Account | change the password |

Everything applies at the next start of a PC: the menu is made again for each one.

Verbose start-up also removes valued splash options such as `splash=silent` and replaces other kernel log levels with `loglevel=7`, for both ISO and PC options. Proxmox's verbose mode opens debug shells: type `exit` or press Ctrl+D to continue installation.

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

- It is kept in `pcs.json` next to the settings file, so a restart does not empty it.
- A PC's address is the one its request came from.
- The page is paged (10, 25, 50 or 100 per page; the choice is remembered in the browser, and the first guess comes from the window's height, because the number that fits cannot be known) and has a search box. `/api/pcs` takes `limit`, `offset` and `q`.
- "Forget" removes one PC, "Forget them now" removes the PCs not seen for N days, and a PC is forgotten by itself after `pcDays` days without a start (default 90, 0 keeps all). Rules are never removed by this.
- **Kernel options** (button on each PC; `PUT /api/pcs/{mac}/options` with `{"args": "...", "debug": {"verbose": true, "serial": false}}`): extra kernel arguments and the *verbose start-up* and *serial console* switches for that PC only. They come after the arguments of the ISO itself, on the kernel line of every ISO that PC starts from its own files (not Windows, not a disk image; the pages of the menu carry the PC's address, so they apply on every page and to a PC's rule too), from the PC's next start. The settings file keeps them under `pcOptions` by MAC address (they are in backups), and a PC that has them is listed even when it has not asked since the server started. Empty arguments and both switches off remove the entry. *Verbose start-up* adds, besides `loglevel=7 ignore_loglevel debug`, the words that the start-up of the kind of system honours: `rd.debug` for dracut (Fedora and what is built on it, Red Hat's installers, openSUSE, Gentoo), `systemd.show_status=1` for the systemd live systems (Fedora, openSUSE, Arch, SystemRescue, Debian, Ubuntu and their relatives), `debug_init` for Alpine and `proxdebug` for Proxmox VE (its debug shell opens before the installer starts: leave it with `exit`). A kind without a word of its own (Tiny Core, ShredOS, Tails) gets only the general ones.

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
  given while it is attached to an ISO or a PC, but **an installer cannot log in, so the file is not secret from the local network**: anyone who can reach the server and asks the menu for that ISO sees
  the address in its kernel line and can read the file (with any MAC address in the path). Like the rest of the server it is only given to the networks that may use it (Server > Access).
- **One file for many PCs.** In the text, the words `hostname`, `name`, `mac`, `mac-dash`, `ip` and `serial` between double curly braces are replaced for the PC that asks: `hostname` is the name given
  on the PCs page made into a host name, or `pc-` and the last six digits of its address.
- **Passwords.** Put password *hashes* in a file, not passwords (the templates ask for hashes: `openssl passwd -6` or `mkpasswd -m sha-512`, or an SSH key), because of the point above. A file can hold secrets. They are shown as `********` on the page and in the API unless "Show the passwords" is ticked; a file saved with the stars left as they were keeps its
  values, and a save that would turn a value into the stars is refused. The files are kept in the settings file (so not in a backup made without secrets, nor in the copy beside the library), and
  the log only says which file was given to which PC, never its text.
- **What was checked.** Ubuntu Server 26.04.1 installed by itself in a UEFI VM from an autoinstall file (the disk was partitioned, the system installed and it booted under the PC's `hostname`).
  The kernel arguments of the other kinds are right for their installers but were not run to the end: the Debian *netinst* installer cannot install when it is started this way (it has no CD, see
  [#59](https://github.com/ghazichaieb/gola-bootlace/issues/59)), and kickstart and AutoYaST were not tried. A Windows `autounattend.xml` is not part of this: Windows Setup still takes the file that
  sits in the ISO's own folder.
- **Ubuntu Server** ISOs are now downloaded whole into the PC's memory (the PC needs about the size of the ISO and 2 GB more) instead of being read from the network disk: Ubuntu's installer stops on
  the network disk with "Invalid dep_id (disk-nbd0)".

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

While unpacking a compressed image, the server checks free space on the disk that holds the chosen cache folder before each write. It stops and removes the partial copy if the write would use the disk's reserve (1 % of the disk, at least 512 MB and at most 5 GB), or if free space cannot be checked. The error is shown in the menu and the server log. This protects the disk even when a packed image expands far beyond its compressed size; it does not enforce the container cache's configured size limit during unpacking or move an unfinished copy to another cache folder.

What the server makes once and keeps: the unpacked copies of packed disk images, and the copies of Windows' `boot.wim` with Bootlace's files in them. Under **Server > Cache**, the container's own folder is used while it has room (2 GB by default); after that new entries go to `<library>/.bootlace/cache`. An entry is looked for in both places. The size of the container's folder bounds how many different installs can be prepared at once; the library is slower (it may be a network drive) but roomy. A copy of `boot.wim` contains the share's password, so anyone who can read the library's folder can read it.
