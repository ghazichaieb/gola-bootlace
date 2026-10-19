<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/bootlace-logo-dark.svg">
    <img alt="Gola Bootlace" src="branding/bootlace-logo.svg" width="460">
  </picture>
</p>

# Gola Bootlace

A network boot server for ISO files: put your ISOs in a folder, and any PC on the network can boot and install from them over PXE.
No USB sticks. Think of [Ventoy](https://github.com/ventoy/Ventoy), but over the network.

**Status: early design. Nothing here boots anything yet.** The goals below are goals, not features.

## Goals

- Boot unmodified Windows and Linux ISOs (and BSD where possible), straight from the ISO file. The ISO is never changed.
- One small server program that runs the same way in a Proxmox LXC container and in Docker Compose.
- Leave the network's existing DHCP server alone (proxyDHCP), with an optional full DHCP mode.
- BIOS and UEFI clients; ARM64 and Secure Boot later.
- A public, tested compatibility list: an ISO is listed as working only after it has actually booted in our test rig.

## How it will work (short version)

The server speaks proxyDHCP, TFTP and HTTP and hands clients an iPXE boot menu built from the ISO folder. Linux ISOs start their own kernel
with a small helper that downloads the ISO into RAM and makes it look like a disc; Windows ISOs start their setup environment with a helper that
attaches the ISO from a network share. Read [docs/start-plan.md](docs/start-plan.md) for the plan and the phases, and
[docs/compatibility.md](docs/compatibility.md) for what has been tested.

## Building

You need Go (see `go.mod` for the version).

```
go build ./cmd/bootlace
go test ./...
```

## Licence

Gola Bootlace is free software under the **GNU General Public License, version 3 or (at your option) any later version**; see
[LICENSE](LICENSE). Additional terms about attribution and the project's name are in [ADDITIONAL-TERMS.md](ADDITIONAL-TERMS.md) (draft).
Third-party code and credits are in [NOTICE](NOTICE).

"Gola" and "Gola Bootlace" are names of Gola Services SUARL. The licence gives you the code, not the right to use the names for a modified version.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
