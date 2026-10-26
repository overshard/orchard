# The Caddyfile is baked in rather than bind mounted, since the Docker CLI in
# webdev talks to the host's daemon, which resolves a source path against its
# own filesystem and mounts it empty instead of failing.
FROM caddy:2.11.7-alpine@sha256:eb4d35b7452d1ab6171fbdbe8d9275b2301dad6dad92115d91e19fb9b24d510f
COPY Caddyfile /etc/caddy/Caddyfile
