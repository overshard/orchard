# The build context is this directory. Each site is its own Go module and
# carries its own copy of web/, so nothing above this folder is needed.
#
# Base images are pinned by digest, not only by tag: `oven/bun:1-alpine` floats
# across an entire major version, so two builds a month apart would not build
# the same thing. Bump these the same way the typst pin below is bumped.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash.
#
# Unlike blog.bythewood.me this is copied into the RUNTIME image rather than
# used and discarded during the build. A property report covers an arbitrary
# date range over a table that grows every second, so there is no finite set to
# precompile. See typst.go.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apk add --no-cache curl tar xz && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

# --- frontend -----------------------------------------------------------
FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS frontend
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# --- maps ---------------------------------------------------------------
# Per-country admin-1 topojson from Natural Earth, about 10MB of output.
#
# Its own stage, after the frontend one, so the layer cache survives. The input
# is a fixed public-domain dataset and this is the only build step that reaches
# the network for content, so rebuilding it on every CSS tweak would add a
# minute to each deploy.
FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS maps
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/scripts/ ./scripts/
RUN bun run scripts/build_maps.js

# --- social card --------------------------------------------------------
# The card cannot change while the process runs, so it is built here rather
# than rendered per request. PNG rather than SVG, because no social platform
# accepts image/svg+xml for og:image.
#
# Geist comes out of the frontend stage's bun install rather than a second
# download. @fontsource ships woff2 only and Typst reads TrueType, so the
# package that works is Vercel's own, which carries the .ttf files.
FROM typst AS card
COPY --from=frontend /src/node_modules/geist/dist/fonts/geist-sans /fonts/geist-sans
COPY typst/card.typ /card.typ
RUN typst compile --format png --ppi 72 --font-path /fonts/geist-sans /card.typ /card.png

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

# The bracket glob makes go.sum optional. A module with no third party
# dependency has none for `go mod tidy` to write, and a plain `COPY go.sum`
# fails the build outright when the file is absent.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY reports/ ./reports/
# Everything the site serves has to be on disk before the compile, not after:
# -tags embed reads build/ at compile time and bakes it into the executable.
# The card lands inside the bundle for the same reason, and is still served at
# /static/og/card.png by the handler that serves the rest of it.
COPY --from=frontend /build/dist ./build/dist
COPY --from=card /card.png ./build/dist/og/card.png
COPY --from=maps /build/static_maps ./build/static_maps
# CGO off is what modernc.org/sqlite buys: SQLite transpiled to Go rather than
# bound to it, so the binary is static and needs no libc at runtime.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

# --- image --------------------------------------------------------------
# alpine, not scratch: this site shells out to typst on the request path, and
# typst needs real font files on disk.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

RUN apk add --no-cache \
      ca-certificates \
      font-jetbrains-mono \
      ttf-dejavu \
      ttf-liberation \
      fontconfig && \
    fc-cache -f

COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
# Templates, the Vite bundle and the topojson are inside the executable.
# Everything below is what genuinely cannot be: the compiler this binary execs
# on the request path, and the fonts it reads.
COPY --from=build /app /app
# Typst resolves absolute paths in the generated source against SITE_ROOT, so
# it doubles as the fence on what a compile is allowed to read.
WORKDIR /srv
COPY reports/ /srv/reports/

ENV SITE_DATA=/data
ENV SITE_ROOT=/srv
# fontconfig wants somewhere writable; without it typst warns on every compile.
ENV XDG_CACHE_HOME=/tmp

# The database and the GeoIP mmdb. A volume, not a bind mount: the daemon here
# is Docker Desktop on the Windows host and resolves paths against its own
# filesystem, so a bind mount of a path under /home/dev silently becomes an
# empty directory.
VOLUME ["/data"]

# The same UID 65532 the two FROM scratch images use, so every container in this
# repo runs as one non-root identity and a /data volume is chown'd to one
# number.
#
# -H because this user owns no home: everything it writes goes to /data (the
# volume) or /tmp (XDG_CACHE_HOME, where Typst scribbles).
#
# A volume created root-owned stays root-owned and needs a one-time chown. A
# fresh volume inherits ownership from the image and needs nothing.
RUN addgroup -g 65532 app && adduser -u 65532 -G app -D -H app
USER 65532:65532

# The binary probes itself rather than shelling out to wget, which this image
# does have, because the two scratch images cannot and one behaviour across four
# sites beats two. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
