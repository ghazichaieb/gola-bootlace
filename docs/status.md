# Project status

Kept short on purpose. Last checked against the code, `git log` and the issues on 2026-10-29 (main at the commit that added this page). The history is in
[CHANGELOG.md](../CHANGELOG.md); the reasoning is in [architecture.md](architecture.md); what was tested is in [compatibility.md](compatibility.md).

## Where things stand

- **Latest release:** 0.5.1 (2026-10-30): the update command lists the changes for an installation that ran a pre-release (and drops a trailing comma from the lines), the Server page no longer reads like an error about the own-key way, Windows set-up names the devices without a driver when it cannot reach the share, and the library's drivers are loaded again after `wpeinit` and logged. Before it: 0.5.0 (2026-10-29), the Secure Boot route built into the program and boot entries that survive a restart (#75, @omar-akermi).
- **Latest pre-release:** 0.5.2-rc.1 (2026-10-30): loads the library's drivers in Windows PE (the folder test that skipped them is gone); to be confirmed on the ASUS ExpertBook before 0.5.2.
- **Not yet done for that work:** a run of the *built-in* route on a real PC (the same files were measured from a folder) and a look at the Server page in a browser. A release is made by the maintainer only (see CONTRIBUTING.md).
- **Branches:** `main` is the only line that matters. `sb10` is the Secure Boot prototype of 0.5.0-rc.1, kept for reference, not to be merged.
- **Pull requests:** none open. #75 (restore configured boot entries immediately after restart, from @omar-akermi) was reviewed, changed by its author as asked, and merged (item 2 of #70).

## Open issues, grouped

| Group | Issues | State |
|---|---|---|
| Secure Boot | #10, #73 | #10: ARM64 and a real-PC run of the built-in files are open; whether to wait for an iPXE release signed with the 2023 Microsoft certificate is undecided. #73: could not be reproduced in-process; waiting for the next real-PC run |
| Real-PC faults | #51 (ASUS P8Z77-V LX UEFI freeze), #57 (Pop!_OS 24.04 on an ASUS X299) | waiting for a packet capture (#51) and a test with a newer Pop!_OS image (#57) |
| Boot menu | #35, #70, #72 | #35: entries are offered and manageable per ISO; the rest is items 1, 3, 4 and 5 of #70 (item 2, entries right after a restart, is done by #75) and entries of systems with their own rules. #72: a button to reopen the first-start guide |
| Unattended and Windows | #17, #18, #16 | #17: groups of PCs and end-to-end kickstart/AutoYaST runs are missing. #18: drivers and check bypass done and verified; kept open for the parts listed in its last comment. #16: low priority by decision |
| Network boot variants | #19, #29, #30, #9 | #19: UEFI HTTP boot answered, untested on real firmware. #29: only stage 1 (dual-stack web port). #30: assessed, recommended not to build. #9: FreeBSD hand-off, OpenBSD, real-PC runs |
| Features with a first part done | #22, #23, #24, #25 | groups/model rules, Wake button (no schedules), per-ISO boot password (no whole-menu password), per-PC history (no "did it work" report) |
| Later | #26, #33, #49, #50, #34 | persistence (deferred), Windows service and installer, signed releases and more package formats, community-scripts submission, the pre-release audit |

## Building and testing

```
go build ./cmd/bootlace
go vet ./... && go test ./...
node --test tools/admin-views.test.cjs tools/admin-secureboot.test.cjs
```

`tools/build.sh` builds with the version read from the git tag; `tools/release.sh` prints the release commands and publishes nothing. Real-file tests need
`BOOTLACE_TEST_ISODIR` (see the README). The lab scripts (`tools/lab.ps1` and `tools/pxe_vm.ps1` for Hyper-V, `tools/proxmox/pve_run.sh` and `pve_uefi_vm.sh` for a Proxmox
host) boot an ISO in a virtual machine, BIOS or UEFI, with or without Secure Boot, and keep a screenshot. Real hardware results go in compatibility.md with the date.

## Rules that apply to every change

- A change a user would notice gets a line under "Unreleased" in the changelog in the same commit, and the documentation page it affects is updated with it.
- A test PC result or an ISO result is written down only after it was run to the end; say what was not run.
- Text that comes from outside (file names, ISO menu titles, kernel arguments) goes through `internal/ipxesafe` before it reaches an iPXE script.
- Credit people who contributed by their GitHub login in the changelog and in issue and pull request comments.
