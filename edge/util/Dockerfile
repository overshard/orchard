# The one throwaway container the Makefile reaches for: curl into another
# container's network namespace, and fetch the Wikipedia snapshot. Every image
# in this repo is Debian trixie or scratch, so this is too.
#
# Built by `make util`, which the targets that need it depend on.

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*
