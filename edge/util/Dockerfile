# The one throwaway container the Makefile reaches for: curl into another
# container's network namespace, and fetch the Wikipedia snapshot. Every image
# in this repo is Debian trixie or scratch, so this is too rather than pulling
# alpine and curlimages/curl at random.
#
# Built by `make util`, which the targets that need it depend on.

FROM debian@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*
