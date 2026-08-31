# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-alpine floats.

FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS frontend
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

# The bracket glob makes go.sum optional, since a plain COPY go.sum fails the
# build outright for a module with no third party dependency.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
# -tags embed reads build/ at compile time, so the bundle has to land here
# before the compile below.
COPY --from=frontend /build/dist ./build/dist
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# A scratch image cannot RUN mkdir, so the mountpoint has to arrive as a
# directory copied from a stage that can. Docker seeds a fresh volume from the
# image path it mounts over and carries its ownership, so without this the
# mountpoint is root-owned and the guard's first write fails.
RUN mkdir -p /seed-data

# scratch, since nothing here shells out. Every asset is embedded and the only
# thing on disk is the guard's state file.
FROM scratch

# Four upstreams over TLS, and without this every one of them fails x509 with no
# other symptom.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app /app

ENV SITE_DATA=/data

# UID 65532 like the other scratch images here, so one number owns every /data
# volume. There is no /etc/passwd to name a user in, so it is the bare number.
COPY --from=build --chown=65532:65532 /seed-data /data
USER 65532:65532
VOLUME ["/data"]

# The binary probes itself, since a scratch image has no shell for a check to
# call. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
