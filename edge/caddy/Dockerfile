# Stock Caddy with the Caddyfile baked in.
#
# A bind mount would be the obvious choice and does not work: the Docker CLI in
# the webdev container talks to Docker Desktop on the Windows host, and that
# daemon resolves the source path against its own filesystem, where this
# directory does not exist. It mounts empty instead of failing. Baking it in
# also means `compose up --build` is the entire deploy, with the config
# versioned alongside the image.
FROM caddy:2-alpine
COPY Caddyfile /etc/caddy/Caddyfile
