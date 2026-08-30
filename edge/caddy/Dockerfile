# The Caddyfile is baked in rather than bind mounted: the Docker CLI in the
# webdev container talks to Docker Desktop on the Windows host, which resolves
# the source path against its own filesystem and mounts empty instead of failing.
FROM caddy:2-alpine
COPY Caddyfile /etc/caddy/Caddyfile
