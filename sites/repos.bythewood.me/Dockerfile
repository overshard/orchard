# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-debian floats.

FROM oven/bun:1-debian@sha256:923bedb355f7746666659f71e724998159a5c8c54bd1cb1030e1975a0698415b AS frontend
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.27-trixie@sha256:2f84bc93ecfb2689f782b153fdcd368b5a7ab96c1386c65cdaccf35e726d6a44 AS build
WORKDIR /src

COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY --from=frontend /build/dist ./build/dist
# CGO off, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so the binary is static and needs no libc.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# debian, not scratch: the clone and push wire is git itself. `git http-backend`
# is git's own CGI and has to be on disk to serve a single clone, and every page
# read is a git plumbing subprocess.
FROM debian@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a

# Debian's git package ships git-http-backend under /usr/lib/git-core, which
# wire.go probes for, so a clone works without a second package.
# ca-certificates is for the mirror lane talking to api.github.com over TLS.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      git \
      ca-certificates \
      && rm -rf /var/lib/apt/lists/*

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
RUN groupadd -g 65532 app && \
    useradd -u 65532 -g app -M -s /usr/sbin/nologin app
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
