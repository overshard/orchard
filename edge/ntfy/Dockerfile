# server.yml is baked in rather than bind mounted, for the reason in
# edge/caddy/Dockerfile.
FROM binwiederhier/ntfy:v2

# The named volume mounts over this. Created in the image so a first run against
# an empty volume has a directory to write into.
RUN mkdir -p /var/lib/ntfy

COPY server.yml /etc/ntfy/server.yml
