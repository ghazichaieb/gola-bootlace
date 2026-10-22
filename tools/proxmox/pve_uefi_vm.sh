#!/bin/bash
# Runs on the Proxmox host (copied there by pve_run.sh): makes a UEFI test VM that starts Bootlace's iPXE from a small EFI disk (/root/espdisk.img), because Proxmox's OVMF has no PXE.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#   pve_uefi_vm.sh VMID MEMORY_GB MAC [NAME]
set -eu
vmid=$1; mem=$2; mac=$3; name=${4:-bl-t$1}
qm stop "$vmid" >/dev/null 2>&1 || true
qm destroy "$vmid" --purge >/dev/null 2>&1 || true
qm create "$vmid" --name "$name" --memory $((mem * 1024)) --cores 2 --cpu host --net0 "virtio=$mac,bridge=vmbr0" --bios ovmf \
   --efidisk0 local-lvm:1,efitype=4m,pre-enrolled-keys=0 --vga std --ostype l26 --scsihw virtio-scsi-pci >/dev/null
disk=$(qm importdisk "$vmid" /root/espdisk.img local-lvm 2>&1 | sed -n "s/.*'local-lvm:\(vm-[0-9]*-disk-[0-9]*\)'.*/\1/p")
[ -n "$disk" ] || { echo "the EFI disk was not imported" >&2; exit 1; }
qm set "$vmid" --scsi0 "local-lvm:$disk" --boot order=scsi0 >/dev/null
