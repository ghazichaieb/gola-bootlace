# Gola Bootlace: network boot server for ISO files.
# Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
#
# Build:  docker build --build-arg VERSION=0.1.0 -t gola-bootlace .
# Run:    see deploy/docker-compose.yml (host networking: network boot uses broadcast, which Docker's own networks do not pass; Linux hosts only).

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .
ARG VERSION=0.0.0-dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/ghazichaieb/gola-bootlace/internal/version.Version=${VERSION}" -o /out/bootlace ./cmd/bootlace

FROM alpine:3.22
# wimlib-imagex starts Windows with UEFI Secure Boot on (it puts two files into a copy of the ISO's boot.wim)
RUN apk add --no-cache wimlib ca-certificates tzdata
COPY --from=build /out/bootlace /usr/local/bin/bootlace
COPY LICENSE NOTICE ADDITIONAL-TERMS.md /usr/share/doc/bootlace/

# The settings and the cache are in /data; the library (the folder with the ISOs, it must be writable) is /library.
# Every option of "bootlace serve" can be set as BOOTLACE_<NAME>.
ENV BOOTLACE_CONFIG=/data/bootlace.json \
    BOOTLACE_ISO_DIR=/library
VOLUME ["/data", "/library"]

# Web (and the web interface), Windows share, iSCSI, network disk; TFTP and DHCP (proxy) are UDP.
EXPOSE 8080/tcp 445/tcp 3260/tcp 10809/tcp 69/udp 67/udp 4011/udp

HEALTHCHECK --interval=30s --timeout=6s --start-period=15s --retries=3 CMD ["bootlace", "health"]

LABEL org.opencontainers.image.title="Gola Bootlace" \
      org.opencontainers.image.description="Network boot server: start Windows and Linux installers from ISO files over the network" \
      org.opencontainers.image.source="https://github.com/ghazichaieb/gola-bootlace" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.vendor="Gola Services SUARL"

ENTRYPOINT ["bootlace"]
CMD ["serve"]
