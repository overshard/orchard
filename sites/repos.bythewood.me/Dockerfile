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

COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY --from=frontend /build/dist ./build/dist
# CGO off, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so the binary is static and needs no libc.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# alpine, not scratch: the clone and push wire is git itself. `git http-backend`
# is git's own CGI and has to be on disk to serve a single clone, and every page
# read is a git plumbing subprocess.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

# git-daemon is the Alpine package that ships git-http-backend, and plain `git`
# does not, so without it the site starts fine and then 404s every clone.
# ca-certificates is for the mirror lane talking to api.github.com over TLS.
RUN apk add --no-cache git git-daemon ca-certificates

COPY --from=build /app /app

ENV REPOS_ROOT=/data/repos
ENV REPOS_DATA=/data
# git writes a config and a temporary or two, and the app user has no home.
ENV HOME=/tmp

# chown before the VOLUME, and the order matters: Docker seeds a fresh volume
# from the image path it mounts over and carries its ownership. Without it the
# mountpoint is root-owned and the first push dies on a permission error.
RUN mkdir -p /data/repos && chown -R 65532:65532 /data
VOLUME ["/data"]

# UID 65532 is what every other image here runs as, so one number owns every
# /data volume.
RUN addgroup -g 65532 app && adduser -u 65532 -G app -D -H app
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
