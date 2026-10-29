# How Bootlace starts a system

This page explains how a file in the ISO folder becomes a menu entry that starts, and how to teach Bootlace a system it does not know yet. What has been tested
on which ISO is in [compatibility.md](compatibility.md).

## The path of one boot

1. **The menu.** A PC asks `/menu`. For every file of the ISO folder (`httpd.entryInfos`) the server asks *what is this* (`recognise`) and writes the iPXE commands
   that start it.
2. **Recognising.** The package `family` looks at the files and answers with a `Boot`: a family name and the iPXE commands (kernel, start-up disk, boot). A file it
   does not know is handed to the PC as a CD or disk (iSCSI) instead.
3. **The files.** iPXE fetches the kernel and the start-up disk from `/fs/<id>/...`, which serves them out of the ISO (or out of a partition of a disk image,
   `/fs/<id>@<n>/...`), without unpacking anything.
4. **The extra archive.** Most systems also get `/x/<id>/extra.cpio`: a second initramfs archive that the kernel unpacks over the ISO's own. It adds what the
   start-up disk lacks, and nothing of the ISO is changed.
5. **The ISO itself** is offered to the running start-up disk as a network block disk (NBD, port 10809), read a piece at a time, so that it needs no memory.

## The model: locator, strategy, extra archive

Every system is the same three things (package `family`, one file per group of systems):

| Part | What it does | Examples |
|---|---|---|
| **Locator** | finds the kernel and the start-up disk, and in which volume they are | `/casper/vmlinuz`; the EFI image inside ChimeraOS's ISO; a partition of Tails's disk image; an entry of the ISO's own boot menu |
| **Strategy** | tells the start-up disk where the ISO is, in the best way both the server and the start-up disk can do (`Transport`) | `casper`, `liveBoot`, `archiso`, `archisoHook`, `dracutLive` |
| **Extra archive** | adds what the start-up disk lacks | the network disk client; network card drivers; a hook or a script that loads them |

The four ways the ISO can be reached (`transport.go`), from the best:

| Transport | How the system reads the ISO | Needs |
|---|---|---|
| `NBD` | a network block disk that the extra archive attaches | the network disk server and the extra archive |
| `ISCSI` | a network CD that the start-up disk logs in to itself (dracut only) | the iSCSI server |
| `HTTP` | only the system image (a few hundred MB) is downloaded | the web server |
| `Memory` | the whole ISO is downloaded into the PC's memory | the web server, and RAM for all of it, twice over |

A strategy takes `u.pick(NBD, ISCSI, Memory)`: the first of its list that the server can offer.

**Volumes.** A file can be more than one set of files: an ISO has a file system, and also partitions (the EFI image) that hold more; a disk image has partitions.
`Context.FS` is the ISO's own, `Context.Volumes` finds the others when asked, and every file is fetched from the address of the volume it was found in.

## Injecting something into a start-up disk

The extra archive is the only way in. What has to be added, and where the start-up disk can be made to run it, depends on how the start-up disk is made:

| Technique | Used for | Why this one |
|---|---|---|
| a command the start-up disk already calls (`nfsmount`) | casper, live-boot | they mount the ISO with it when told `netboot=nfs` |
| a handler the start-up disk already calls (`sbin/nbdroot`) | dracut | `netroot=nbd:` is handed to `<scheme>root` |
| a client program under the name it calls (`nbd-client`) | archiso with its network hooks | the hook runs `nbd-client`; this one connects again when the connection is lost |
| a script that is made to run first (`scripts/init-top/ORDER`) | Tails, unknown casper/live-boot with no card drivers | initramfs-tools runs the scripts that ORDER names |
| a hook, and the hook list (`hooks/bootlace`, `config`) | archiso without network hooks (ChimeraOS) | mkinitcpio runs the hooks of `HOOKS=`; **only executable ones** |
| a changed copy of the start-up script (`init`) | Proxmox VE | its script looks for the ISO in its own way; lines are added before it |
| `rdinit=/bootlace-init` | Alpine, LibreELEC (Lakka) | the kernel runs this before `/init`: it loads drivers or fixes one line, then `exec /init` |
| a start-up script of the installer (`lib/debian-installer-startup.d/S12...`) | the Debian installer of a netinst ISO (Debian, Kali) | it can only go on with a CD: the script brings up the network, logs in to the ISO's iSCSI CD-ROM and waits for `/dev/sr0`, and the installer's own CD detection mounts it. The drivers and `iscsistart` come out of the ISO's own pool (`udeb`, `debinst`) |
| a file in a folder the archive makes | Porteus | the script looks in `/mnt/*`; the ISO is an initrd file there |

Things that were learnt the hard way, and are why the code looks as it does:
- iPXE's `initrd --name` is the path in the archive, and **it does not make folders**: a file in a folder that is not there is dropped without a word. The archive
  makes the folder (`initrd.Builder.Dir`).
- A folder entry over a link replaces the link with an empty folder (Fedora's `/usr/sbin`): folders are only made when asked for.
- BusyBox's shell runs its own applet for `nbd-client` even when a file of that name is on the path: the call has to be rewritten (LibreELEC).
- `modprobe` is kmod and reads a **binary** index: extending the text `modules.dep` does nothing. Drivers are loaded with the helper's `insmod` from plain files.
- A start-up disk keeps its modules compressed or unloaded: a driver's needs are brought along too, even when the start-up disk "has" them.
- The menu appends `|| goto failed` to every iPXE line: a line that may be false (`iseq ...`) must end with `|| echo`.

## Systems that are not known by name

`family.DetectGeneric` (`generic.go`) recognises a system by what it is made of:

1. `bootcfg` reads the ISO's own boot menus (isolinux/syslinux, GRUB incl. `source`, systemd-boot) and gives the kernel, start-up disk and arguments of each entry.
   Ordinary entries come before "safe graphics", "check" and such.
   `family.Ways` (`ways.go`) turns the other entries into *ways*: each is started by the same strategy as the usual start (the strategy is recorded as `Boot.Kind`: casper, live-boot,
   archiso, dracut-live), and only when it needs no extra archive other than the usual start's (the server offers one per ISO). The server looks for them in the background (like
   the generic recognition) and the menu line "Other ways to start an ISO" opens `menu?ways=list`, then `menu?ways=<iso id>`.
2. `initread.Inspect` lists the start-up disk, and `identify` (`flavor.go`) says what it is: casper (`scripts/casper`), live-boot (`scripts/live`), archiso
   (`hooks/archiso`, and whether it has `archiso_pxe_nbd`), dracut with a live module (`dmsquash-live-root`, and whether it has `netroot`, an iSCSI client,
   `livenetroot`), and whether it carries network card drivers and the `nbd` driver.
3. The same strategy the named systems of that make use is applied, with the menu's own arguments (minus the ones that say where the ISO is: `findiso=`,
   `root=`, `archisolabel=` ... see `dropFor...`). A start-up disk with no card drivers gets them, from whichever squashfs of the ISO has the kernel's modules.

It takes seconds (the start-up disk is read whole), so it runs **after** the named families and **in the background**: the first menu shows the ISO as a disk, the
next one shows the system once it is known (the answer is kept while the file does not change). It handles only the makes above; anything else stays a disk.

`BOOTLACE_GENERIC_ONLY=<start of ISO file names, comma separated>` skips the named recognition for those files, which is how the generic path is tried on ISOs that
are in fact known.

## Adding a system

1. Put the locator and strategy in the file of its group (`debian.go`, `arch.go`, `redhat.go`, `alpine.go`, `standalone.go`, or a new one), as
   `func name(c *Context) (Boot, bool)`, and add it to `detectors` in `family.go` (most specific first).
2. Build the command line with `newBoot(family, files, args)`, and `.withExtra(u, builder)` when it needs an archive; the builder gets `ExtraEnv` (where the
   network disk is, the helper program) and uses `initrd.Builder`. If the usual archive is enough, pass `nil`.
3. Choose the transport with `u.pick(...)`; never write `if u.NBD != ""`.
4. Test it: a table entry in the family's test file with a fake tree (`fake{"/path": true}`), and the ISO in `knownIsos` of `real_test.go`. Run the real-ISO
   tests with `BOOTLACE_TEST_ISODIR=<folder of ISOs> go test ./internal/family`.
5. Try it on a PC or VM and write the result in `compatibility.md`.

## Tools for finding faults

Meant for development and for reporting a problem; none of them changes what is served.

| Switch | What it does |
|---|---|
| `BOOTLACE_GENERIC_ONLY=<ISO names>` (environment) | skips the recognition by name for those files, to try the generic path on a known ISO |
| `-debug-smb` | logs every message of the Windows file share (a lot) |
| `-smb-forward host:port` | passes the share's connections on to another SMB server and logs them, to compare with a real Windows share |
| `bootlace inspect <iso>` | says how the server would start an ISO and prints the commands, without a network |

## Where things are

| Package | Role |
|---|---|
| `family` | recognising systems and how to start them (this page) |
| `bootcfg` | reads isolinux/GRUB/systemd-boot menus |
| `initrd` | writes the extra archive (cpio), the shell scripts that go in it |
| `initread` | reads an initramfs (any compression) and kernel modules out of a squashfs |
| `udeb`, `debinst` | read the Debian installer's own packages (the ISO's Packages index and `.udeb` files) and make the archive that lets the installer of a netinst ISO find its CD over iSCSI |
| `disc`, `iso9660`, `udf`, `fat`, `partition` | read ISOs, disk images and their partitions without mounting |
| `httpd` | the web server: menu, files, extra archives, the network disk exports; `source.go` opens what a file holds |
| `nbd`, `iscsi` | the network disk and iSCSI servers; `cmd/bootlace-nbd` is the client that runs in the PC's initramfs |
| `diskimg`, `isos` | packed files and zip archives, the folder scan |
| `winpe`, `smbshare`, `handoff`, `tftp`, `dhcp`, `menu`, `gfx` | Windows set-up, FreeBSD/Ubuntu native chains, the boot menu and the network services |
| `secureboot` | the iPXE project's signed shim and iPXE, embedded with a manifest that is checked at start (the Secure Boot route, [secure-boot.md](secure-boot.md)) |
| `admin`, `config`, `backup`, `access`, `library`, `importer`, `service`, `logbuf` | the management page and its API, answer files for unattended installs, settings backup, the local-network-only rule, the library and its uploads and downloads, starting and stopping the services, the log kept in memory |
| `ipxesafe` | keeps text that is not Bootlace's own (file names, titles read from an ISO's menu, kernel arguments) from chaining iPXE commands |
| `wol`, `pxecheck`, `selftest`, `speedtest`, `tidy` | Wake-on-LAN, a look for other PXE/DHCP servers, the server checking itself the way a PC uses it, a folder speed test, removing leftovers (cache, partial files) |
