# The Caddyfile is baked in rather than bind mounted, since the Docker CLI in
# webdev talks to the host's daemon, which resolves a source path against its
# own filesystem and mounts it empty instead of failing.
FROM caddy:2.11.7-alpine@sha256:e70b164da81ef698b3ad19af799ea6086ce6d9e48ed4079eb9fe26f6fbf14ee5
COPY Caddyfile /etc/caddy/Caddyfile
