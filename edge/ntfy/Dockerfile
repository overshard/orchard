# server.yml is baked in rather than bind mounted, for the reason in
# edge/caddy/Dockerfile. The binary comes out of the upstream image the same
# way caddy's does.
FROM binwiederhier/ntfy:v2.36.0@sha256:fb48c413eb11d50339962b5d62cddce0f800e7f77c626d638a7c20cb7e9c4465 AS ntfy

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

# curl is for the compose health check and nothing else.
RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

COPY --from=ntfy /usr/bin/ntfy /usr/bin/ntfy

# The named volume mounts over this. Created in the image so a first run against
# an empty volume has a directory to write into.
RUN mkdir -p /var/lib/ntfy

COPY server.yml /etc/ntfy/server.yml
ENTRYPOINT ["ntfy"]
