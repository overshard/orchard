# Two images from one file. The default target is the page, and compose
# builds the vw target for Vaultwarden. Base images are pinned by digest
# because their tags float.

FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build
WORKDIR /src
COPY go.mod go.su[m] ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app .
RUN mkdir -p /seed-data

FROM vaultwarden/server:1.37.5@sha256:4fe17f415f90087377d37c755e0ea3cf189dcc6794e49cdd2a9adbb4937d9e1f AS upstream

# Vaultwarden's binary on the house base. It links the clients for every
# database it supports, so those libraries come along though only SQLite is used.
FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad AS vw
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates libssl3t64 libmariadb3 libpq5 && \
    rm -rf /var/lib/apt/lists/*
COPY --from=upstream /vaultwarden /vaultwarden
# The page's binary is the health check, since there's no curl here.
COPY --from=build /app /probe
COPY --from=build --chown=65532:65532 /seed-data /data
USER 65532:65532
VOLUME ["/data"]
ENV ROCKET_ADDRESS=0.0.0.0 ROCKET_PORT=8000 DATA_FOLDER=/data
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/probe", "-probe", "http://127.0.0.1:8000/alive"]
ENTRYPOINT ["/vaultwarden"]

FROM scratch AS app
COPY --from=build /app /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
EXPOSE 8000
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
