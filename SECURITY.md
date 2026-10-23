# Security

## Reporting a problem

Please report a security problem **privately**, not in a public issue: use the repository's "Report a vulnerability" button (Security tab, private advisory), or write to
Gola Services through [gola.tn](https://gola.tn). Say what you found, which version (`bootlace --version`) and how to reproduce it. You will get an answer, and a fix with
credit (if you want it) in the changelog.

## What it is designed for

Gola Bootlace is for a **local network** you control. By default every service answers only local addresses (private ranges, loopback, link-local), and it should never be
reachable from the internet; use a VPN to reach the network from outside. Within the local network, the network disk and iSCSI services have no login, the web interface
uses plain HTTP with a password, and the Windows share is a small server written for this project. These are known design limits, listed in
[docs/what-bootlace-changes.md](docs/what-bootlace-changes.md) and in the README, not vulnerabilities by themselves; a way around the local-network rule, the library's
folder limits, the checks on web addresses, or the login, is.

## Checking a download

Releases are signed; see [docs/verifying.md](docs/verifying.md).

## Supported versions

The latest release. The project is young (0.x): fixes go into the next release.
