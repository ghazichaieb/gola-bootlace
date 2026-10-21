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
