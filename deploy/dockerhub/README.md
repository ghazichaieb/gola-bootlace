# Gola Bootlace

A network boot server for ISO files: put Windows and Linux installers in a folder and install them on any PC over the network, without USB sticks. Made by [Gola Services](https://gola.tn). Source and full documentation: <https://github.com/ghazichaieb/gola-bootlace>.

## Quick start (Linux host)

```yaml
# compose.yaml
services:
  bootlace:
    image: ghazichaieb/gola-bootlace:latest
    network_mode: host          # network boot uses broadcast traffic, which Docker's own networks do not pass
    restart: unless-stopped
    volumes:
      - bootlace-data:/data     # settings and small cache
      - ./library:/library      # your ISOs (must be writable by the container)
volumes:
  bootlace-data:
```

```
docker compose up -d
docker compose logs bootlace | grep -i password
```

Then open `http://<address of the host>:8080/admin/`.

## The password of the web interface

The first time it starts, Bootlace makes up a password and writes it **once** in the container's log (`web interface: the password was made up: ...`). Read it with `docker logs <container> 2>&1 | grep -i password` and change it under **Account**. To choose it yourself, set `BOOTLACE_ADMIN_PASSWORD` in `environment:`. If it is lost, set `BOOTLACE_ADMIN_PASSWORD` to a new password and restart the container.

## Good to know

- Linux hosts only (Docker Desktop on Windows and macOS cannot reach the network the PCs are on). On Proxmox, use the LXC script instead: see the docs.
- Every option of `bootlace serve` is an environment variable `BOOTLACE_<NAME>` (`BOOTLACE_IP`, `BOOTLACE_DHCP`, `BOOTLACE_HTTP_PORT` ...).
- Tags: `X.Y.Z`, `X.Y` and `latest`. Architectures: amd64 and arm64.
- It answers the local network only by default. Do not expose it to the internet.
- Licence: GPL-3.0-or-later with additional terms; see the repository.
