# The Caddyfile is baked in rather than bind mounted: the Docker CLI in the
# webdev container talks to Docker Desktop on the Windows host, which resolves
# the source path against its own filesystem and mounts empty instead of failing.
FROM caddy:2-alpine@sha256:98eb57d882ccd5213d1688764db10c1ca2c58a1ca3a6717a3411ad798f7a423a
COPY Caddyfile /etc/caddy/Caddyfile
