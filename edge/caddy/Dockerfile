# The Caddyfile is baked in rather than bind mounted, since the Docker CLI in
# webdev talks to the host's daemon, which resolves a source path against its
# own filesystem and mounts it empty instead of failing.
#
# Built with xcaddy for the rate_limit directive, which stock Caddy lacks.
# Static, so it runs on Debian as is.
FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build

ARG CADDY_VERSION=v2.11.7
ARG XCADDY_VERSION=v0.4.7
ARG RATELIMIT_VERSION=v0.1.1-0.20260612195517-5625512f24f6
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@${XCADDY_VERSION} && \
    CGO_ENABLED=0 xcaddy build ${CADDY_VERSION} \
        --with github.com/mholt/caddy-ratelimit@${RATELIMIT_VERSION} \
        --output /caddy && \
    /caddy list-modules | grep -q '^http.handlers.rate_limit$'

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

COPY --from=build /caddy /usr/bin/caddy
COPY Caddyfile /etc/caddy/Caddyfile

ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
USER 65532:65532
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
