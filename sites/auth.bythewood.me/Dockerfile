# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-debian floats.

FROM oven/bun:1-debian@sha256:923bedb355f7746666659f71e724998159a5c8c54bd1cb1030e1975a0698415b AS frontend
WORKDIR /src
# bunfig.toml has to arrive before install runs, or install.peer = false
# silently does nothing.
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.27-trixie@sha256:2f84bc93ecfb2689f782b153fdcd368b5a7ab96c1386c65cdaccf35e726d6a44 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
# -tags embed reads build/ at compile time, so the bundle has to land here
# before the compile below.
COPY --from=frontend /build/dist ./build/dist
# CGO off, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so the binary is static and needs no libc. It is also what makes FROM
# scratch possible.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

# The data directory is created here rather than at runtime, because a scratch
# image has no shell to mkdir with and Docker seeds a fresh volume from the
# image path it mounts over, carrying its ownership. Without this the mountpoint
# is root-owned and the first boot dies opening the database.
RUN mkdir -p /data && chown 65532:65532 /data

FROM scratch

# Templates and the Vite bundle are inside the executable, and nothing here
# reaches the internet: ntfy is plain HTTP on the bridge, so there is no TLS and
# no need for ca-certificates.
COPY --from=build /app /app
COPY --from=build --chown=65532:65532 /data /data

ENV SITE_DATA=/data
VOLUME ["/data"]

# There is no /etc/passwd in a scratch image to name a user in, so this is the
# bare numeric UID:GID, the same one that owns every other /data volume here.
USER 65532:65532

# No shell in a scratch image for HEALTHCHECK to call, so the binary probes
# itself. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
