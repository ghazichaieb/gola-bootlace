# What Gola Bootlace needs

Measured, not guessed: a Debian 13 container on a Proxmox VE 9.2 host with an Intel Core i7-2700K (2011, one core given to the container), the ISOs on the host's disk,
and 9 real ISOs of 21 GB as the library (Alpine, Arch, Fedora Workstation, Proxmox VE, SystemRescue, TrueNAS SCALE, Tails, Ubuntu Server and Windows Server 2025).
Eight PCs were stood in for by eight parallel readers; the real network disk and iSCSI paths read the same files the same way.

## In short

| | Minimum | Recommended |
|---|---|---|
| **Cores** | 1 | 2 (installs, checksums and imports share the CPU) |
| **Memory** | 256 MB | 512 MB (1 GB with Windows on Secure Boot machines and big imports) |
| **Disk of the container** | 2 GB | 8 GB (the system 0.7 GB, a 2 GB cache, room to grow) |
| **Library** | the ISOs you offer; anywhere that is readable and writable: a folder, a disk, a network share | on a disk or share that can feed several PCs at once |
| **Network** | 100 Mbit wired | gigabit wired; Wi-Fi cannot network-boot |

The ISOs do not have to be in the container: choose the library folder under Server in the web interface (a host folder, or a network share mounted on the host).

## What was measured

- **Idle:** 15 to 20 MB of memory.
- **Building the start-up archive of an ISO** (done once per ISO, when a PC first asks for it): up to **216 MB** for the biggest (Alpine and Proxmox VE, 30 MB archives, read out of squashfs and initramfs files), 5 to 10 MB for most. This is the peak of the whole program. At **256 MB** the container ran the whole set without trouble; at **192 MB** the server was killed while building Proxmox VE's archive. 512 MB leaves room for the system and the page cache.
- **Serving:** eight PCs reading 600 MB each at once, 4.5 GB in 40 s (113 MB/s) from a 1-core container, with the library on a hard disk: the disk was the limit, not memory (the process stayed at 190 MB, most of it the archives above). Reading an ISO over the network disk or iSCSI costs almost no memory: it is read a piece at a time.
- **Checksums:** the 21 GB were hashed in the background in about 100 s of CPU on that old processor, one file at a time, and paused while PCs are using the server.
- **Windows with UEFI Secure Boot:** the copy of `boot.wim` (518 MB, 2 images) is patched by `wimlib-imagex` in 6 to 18 s depending on the disk, with 39 MB of memory for the whole container. The copy lives in the cache folder (disk), not in memory.
- **Disk of the container:** 716 MB after the installation of Debian, `wimtools` and Bootlace. The cache (unpacked disk images and the Windows `boot.wim` copies) is the only thing that grows, up to 2 GB by default and then into the library (Server > Cache).

## Things to know

- The library's speed decides how fast an install is, and how many PCs can install at once: ten PCs reading from one hard disk, or from a share over a slow link, share that speed. Use a disk or share that can feed them.
- In a Debian 13 container `/tmp` is a RAM-backed `tmpfs`. Bootlace keeps only small files there; do not put big files in it.
- A container whose memory limit is lowered while it is busy writing can be killed by the system before the writes are on disk: change the limit when the server is quiet, or restart the container after.
- Not measured: a long run with many PCs and the real network disk on a gigabit network (the lab has virtual machines, and a handful of real PCs are still to try).

## Docker and PCs

The Docker image uses about the same: 42 MB on disk, 20 MB idle. On a PC or laptop the program is a single file of 16 MB; Windows needs the program allowed through its firewall (on private networks) and, when its own file sharing holds port 445, another port for the share (`-smb-port`).
