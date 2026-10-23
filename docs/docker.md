# Running Gola Bootlace with Docker

```
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml logs bootlace     # the password of the web interface is in the first lines
```

Then open `http://<address of the host>:8080/admin/`.

## What you need

- **A Linux host.** Network boot uses broadcast traffic, which Docker's own networks do not pass, so the container uses the host's network (`network_mode: host`). Docker Desktop on Windows and macOS cannot reach the network the PCs are on: use a Linux machine, a virtual machine with a bridged network, or a [Proxmox container](proxmox.md).
- **A folder for the library**, readable and writable (the container runs as root). By default `./library` next to the compose file; set another with `BOOTLACE_LIBRARY=/srv/isos`. It can be a network share mounted on the host. It is the only place the ISOs live: the container itself stays small.
- **Free ports on the host** (the container shares the host's network): 8080 (web), 445 (Windows file share: a Samba or Windows file share on the same host holds it, see `BOOTLACE_SMB_PORT`), 3260, 10809, and UDP 67, 69 and 4011.

Resources: see [requirements](requirements.md).

## Settings

Every option of `bootlace serve` can be set as an environment variable `BOOTLACE_<NAME>` (the option's name in capitals, dashes as underscores), in the `environment:` part of the compose file: `BOOTLACE_ADMIN_PASSWORD`, `BOOTLACE_IP`, `BOOTLACE_DHCP`, `BOOTLACE_HTTP_PORT`, `BOOTLACE_SMB_PORT` ... `docker compose run --rm bootlace serve -h` lists them all. The settings made in the web interface (library, rules, menu, cache, networks that may use the server) are kept in the `bootlace-data` volume.

`BOOTLACE_IP` is the address PCs reach the server at. By default it is the address of the host's default route; set it when the host has several networks and the PCs are on another one.

## Updating

```
docker compose -f deploy/docker-compose.yml pull
docker compose -f deploy/docker-compose.yml up -d
```

Pin a version with `BOOTLACE_VERSION=0.1.0` (the image tags are `X.Y.Z`, `X.Y` and `latest`; a pre-release such as `0.2.0-rc.1` has only its own tag).

## Building the image yourself

```
docker build --build-arg VERSION=0.1.0 -t gola-bootlace .
```

The image is Alpine with the program (a static Go binary) and `wimlib`, which Windows with Secure Boot needs. It is about 42 MB.

## Health check

`bootlace health` asks the running server for `/health`; the image uses it as its health check (`docker ps` shows "healthy").

Gola Bootlace is made by [Gola Services](https://gola.tn).
