# The web interface

Bootlace serves a management page at `http://<server>:8080/admin/` (the address of the server itself, `http://<server>:8080/`, leads there). PCs that
start from the network never need it; they only use the boot menu.

## Password

The first time the server starts without a password it makes one and writes it once in its log:

```
journalctl -u bootlace | grep -i password
```

It is kept only as a salted PBKDF2 hash in the settings file. Change it under **Account**, or set it from the command line (`-admin-password`, or the
environment variable `BOOTLACE_ADMIN_PASSWORD`; the one given at the start wins). Five wrong passwords from one address lock that address out for five
minutes. Anyone on the network can reach the login page, so use a password you would not mind defending; `-no-admin` switches the interface off.

## Pages

| Page | What it does |
|---|---|
| Dashboard | ISOs, PCs seen, how long the server has run, the addresses PCs use, recent activity |
| Server | the library (the folder with the ISOs): choose it with a folder picker, see its state and free space; the services PCs talk to (boot menu, Windows file share, iSCSI, network disk, TFTP, PXE) with their state and any error, start and stop them (with a warning when PCs are using the server), and whether they start by themselves when the program starts |
| Library | the files of the library: upload, add from a web address (with a checksum), copy-in detection, rename, move, new folder, delete, who is using what, the checksums |
| ISOs | (also lists disk images: .img and .raw, packed or not) rename an ISO in the menu, hide it, choose what starts by itself (one of the ISOs, the PC's own disk or nothing), look inside the ISOs again |
| PCs | every PC that asked for a menu (name from its firmware, MAC, address, UEFI or BIOS, what it did last) and a rule for each: show the menu, boot from its own disk, or start one ISO without a menu. Rules can also be added by MAC address for a PC that has not started yet |
| Menu | heading, seconds before the default choice starts, what starts by itself (nothing, the PC's own disk, an ISO), with a preview |
| Log | the last 2000 lines of the server log, live, with a filter |
| Account | change the password |

Everything applies at the next start of a PC: the menu is made again for each one.

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

What the server makes once and keeps: the unpacked copies of packed disk images, and the copies of Windows' `boot.wim` with Bootlace's files in them. Under **Server > Cache**, the container's own folder is used while it has room (2 GB by default); after that new entries go to `<library>/.bootlace/cache`. An entry is looked for in both places. The size of the container's folder bounds how many different installs can be prepared at once; the library is slower (it may be a network drive) but roomy. A copy of `boot.wim` contains the share's password, so anyone who can read the library's folder can read it.
