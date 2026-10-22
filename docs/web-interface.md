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
| ISOs | rename an ISO in the menu, hide it, choose which one starts by itself, look inside the ISOs again |
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

The menu on the PC is shown a page at a time when there are more ISOs than fit: 12 per page on a UEFI PC (the picture leaves 24 text lines) and 10 on a BIOS PC (80 x 25), with "Previous page" and "Next page" lines and "page 2 of 3" in the heading. Flipping pages is not counted as a new start in the PCs list, a timeout never fires while someone is looking through the pages, and the menu opens on the page that holds the entry that starts by itself. A BIOS PC also gets a small ASCII lace mark above the list (iPXE's BIOS build cannot draw a picture or change colours; the blanks that line the mark up are the byte 0xFF, because iPXE joins words with single spaces).
