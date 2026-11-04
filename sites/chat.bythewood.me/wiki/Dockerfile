# kiwix-serve for chat's wikipedia tool. The binary is static and comes out of
# the upstream image, leaving its start.sh and dumb-init behind, which only
# existed to download a ZIM and pick a port.
FROM ghcr.io/kiwix/kiwix-serve:3.8.2@sha256:e478554a9920412eeb5ce1d73f24e4b28445ebed233187cdd03c623c32be46a5 AS kiwix

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

COPY --from=kiwix /usr/local/bin/kiwix-serve /usr/local/bin/kiwix-serve

RUN groupadd -g 65532 app && \
    useradd -u 65532 -g app -M -s /usr/sbin/nologin app
USER 65532:65532

EXPOSE 8000
ENTRYPOINT ["kiwix-serve", "--port=8000"]
