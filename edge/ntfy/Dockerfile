# server.yml is baked in rather than bind mounted, for the reason in
# edge/caddy/Dockerfile.
FROM binwiederhier/ntfy:v2@sha256:f8060a494b69d53866ef505c36a205efcce3204618d6e183a87128cca6e7cac9

# The named volume mounts over this. Created in the image so a first run against
# an empty volume has a directory to write into.
RUN mkdir -p /var/lib/ntfy

COPY server.yml /etc/ntfy/server.yml
