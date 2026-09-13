# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-alpine floats.

FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS frontend
WORKDIR /src
# bunfig.toml has to arrive before install runs, or install.peer = false silently
# does nothing.
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./
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
# -tags embed reads build/ at compile time, so the bundle has to land here before
# the compile below.
COPY --from=frontend /build/dist ./build/dist
# CGO off, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so the binary is static and needs no libc.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# scratch, because everything this site serves is either compiled in or a photo it
# wrote to /data itself. No typst, no chromium, no git.
FROM scratch

# The only thing a scratch image needs from the outside: the root certificates,
# for FEMA, NCDOT, USGS, Overpass, OSRM, the Census geocoder and DHSR.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app /app

ENV SITE_DATA=/data

# UID 65532 like the other scratch images here, so one number owns every /data
# volume. There is no /etc/passwd to name a user in, so it is the bare number.
USER 65532:65532
VOLUME ["/data"]

# The binary probes itself: a scratch image has no shell for HEALTHCHECK to call.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
