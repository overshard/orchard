# The Caddyfile is baked in rather than bind mounted, since the Docker CLI in
# webdev talks to the host's daemon, which resolves a source path against its
# own filesystem and mounts it empty instead of failing.
#
# The upstream image is only where the binary comes from. It's static, so it
# runs on Debian as is and we only track one distro's CVEs.
FROM caddy:2.11.7-alpine@sha256:e70b164da81ef698b3ad19af799ea6086ce6d9e48ed4079eb9fe26f6fbf14ee5 AS caddy

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

COPY --from=caddy /usr/bin/caddy /usr/bin/caddy
COPY Caddyfile /etc/caddy/Caddyfile

ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
