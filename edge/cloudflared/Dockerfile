# The binary comes out of the upstream image the same way caddy's does. The
# config and credentials are in the orchard-cloudflared volume, not in here.
FROM cloudflare/cloudflared:2026.10.7@sha256:4955524aeaa379e154023d5a027c250f7b35041a2e7e950cf13f144bc9b9c5ba AS cloudflared

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=cloudflared /usr/local/bin/cloudflared /usr/local/bin/cloudflared

# 65532 and /home/nonroot match the upstream image, since the files in the
# volume are owned by that uid and `tunnel login` writes under that home.
RUN groupadd -g 65532 nonroot && \
    useradd -u 65532 -g nonroot -d /home/nonroot -m -s /usr/sbin/nologin nonroot
USER 65532:65532
WORKDIR /home/nonroot

ENV NO_AUTOUPDATE=true
ENTRYPOINT ["cloudflared"]
CMD ["version"]
