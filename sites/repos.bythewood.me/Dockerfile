# The build context is this directory. Each site is its own Go module and
# carries its own copy of web/, so nothing above this folder is needed.
#
# Base images are pinned by digest, not only by tag, so two builds a month apart
# build the same thing.

# --- frontend -----------------------------------------------------------
FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS frontend
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY --from=frontend /build/dist ./build/dist
# CGO off is what modernc.org/sqlite buys: SQLite transpiled to Go rather than
# bound to it, so the binary is static and needs no libc at runtime.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# --- image --------------------------------------------------------------
# alpine, not scratch, and this one is not negotiable: the clone and push wire
# IS git. `git http-backend` is git's own CGI and it has to be on disk for the
# site to serve a single clone, and every page read is a git plumbing
# subprocess. This is the same reason analytics is Alpine for typst, except that
# here the subprocess is the product rather than a feature of it.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

# git-daemon is the package that ships git-http-backend on Alpine; plain `git`
# does not include it, and the site starts fine without it and then 404s every
# clone, which is a bad way to find out. ca-certificates is for the mirror lane
# talking to api.github.com over TLS.
RUN apk add --no-cache git git-daemon ca-certificates

COPY --from=build /app /app

# Templates and the Vite bundle are inside the executable. The repositories are
# the one thing that cannot be, and they live on the volume.
ENV REPOS_ROOT=/data/repos
ENV REPOS_DATA=/data
# git writes a config and a temporary or two, and the app user has no home.
ENV HOME=/tmp

# /data is created and chown'd before the VOLUME, and the ordering is the whole
# point: Docker seeds a fresh volume from the image path it is mounted over and
# carries that path's ownership with it. Without this the daemon creates the
# mountpoint root-owned and the first push dies on a permission error that names
# neither permissions nor the volume.
RUN mkdir -p /data/repos && chown -R 65532:65532 /data
VOLUME ["/data"]

# The same UID 65532 the other images here use, so every container in this repo
# runs as one non-root identity and a /data volume is chown'd to one number.
RUN addgroup -g 65532 app && adduser -u 65532 -G app -D -H app
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
