# unbound from Debian, recursing from the root itself with DNSSEC on. Only
# orchard-dns asks it, over the bridge.
FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad
RUN apt-get update && \
    apt-get install -y --no-install-recommends unbound dns-root-data && \
    rm -rf /var/lib/apt/lists/*
COPY unbound.conf /etc/unbound/unbound.conf
RUN cp /usr/share/dns/root.key /var/lib/unbound/root.key && unbound-checkconf /etc/unbound/unbound.conf
# The same UID as the app, which reaches the control socket through a volume
# only that UID can enter.
USER 65532:65532
EXPOSE 5353/udp 5353/tcp
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["unbound-control", "status"]
# The trust anchor is copied fresh into tmpfs each start so unbound can keep
# it current through a root key rollover while it runs.
CMD ["sh", "-c", "cp /usr/share/dns/root.key /var/lib/unbound/root.key && exec unbound -d -p -c /etc/unbound/unbound.conf"]
