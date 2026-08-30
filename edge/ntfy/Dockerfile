# Stock ntfy with server.yml baked in, for the bind-mount reason documented at
# the top of that file and in edge/caddy/Dockerfile.
FROM binwiederhier/ntfy:v2

# The message cache and the user database both live here and the named volume
# mounts over it. Created in the image so a first run against an empty volume
# has a directory to write into rather than failing on the first publish.
RUN mkdir -p /var/lib/ntfy

COPY server.yml /etc/ntfy/server.yml
