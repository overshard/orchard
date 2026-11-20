# The Caddyfile is baked in rather than bind mounted, since the Docker CLI in
# webdev talks to the host's daemon, which resolves a source path against its
# own filesystem and mounts it empty instead of failing.
#
# Stock Caddy built from source, static, so it runs on Debian as is.
FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build

ARG CADDY_VERSION=v2.11.9
RUN CGO_ENABLED=0 GOBIN=/ go install github.com/caddyserver/caddy/v2/cmd/caddy@${CADDY_VERSION} && \
    /caddy version

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

# curl is for the compose health check and nothing else.
RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build /caddy /usr/bin/caddy
COPY Caddyfile /etc/caddy/Caddyfile

ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
USER 65532:65532
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
