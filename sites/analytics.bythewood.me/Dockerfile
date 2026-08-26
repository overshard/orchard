# Build context is the repo root, because the Go module and internal/web live
# there. That is the cost of a monorepo and it is a small one: the alternative
# is a go.mod per site and the shared package copied into each.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash.
#
# Unlike blog.bythewood.me, this one is copied into the RUNTIME image rather
# than used and discarded during the build. A blog post's PDF is fully
# determined before the process boots, so every one of them can be compiled
# here; a property report covers an arbitrary date range chosen in a query
# string over a table that grows every second, so there is no finite set to
# precompile. See typst.go.
FROM alpine:3.23 AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apk add --no-cache curl tar xz && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

# --- frontend -----------------------------------------------------------
FROM oven/bun:1-alpine AS frontend
WORKDIR /src
COPY sites/analytics.bythewood.me/frontend/package.json sites/analytics.bythewood.me/frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY sites/analytics.bythewood.me/frontend/ ./
RUN bun run build

# --- maps ---------------------------------------------------------------
# Per-country admin-1 topojson from Natural Earth, about 10MB of output.
#
# Its own stage, and deliberately after the frontend one, so the layer cache
# survives: the input is a fixed public-domain dataset that does not change,
# and this is the only step in the build that reaches the network for content.
# Rebuilding it on every CSS tweak would add a minute to each deploy.
FROM oven/bun:1-alpine AS maps
WORKDIR /src
COPY sites/analytics.bythewood.me/frontend/package.json sites/analytics.bythewood.me/frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY sites/analytics.bythewood.me/frontend/scripts/ ./scripts/
RUN bun run scripts/build_maps.js

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
COPY internal/ ./internal/
COPY sites/analytics.bythewood.me/*.go ./sites/analytics.bythewood.me/
COPY sites/analytics.bythewood.me/templates/ ./sites/analytics.bythewood.me/templates/
COPY sites/analytics.bythewood.me/reports/ ./sites/analytics.bythewood.me/reports/
# CGO off is what makes modernc.org/sqlite worth having: it is SQLite
# transpiled to Go rather than bound to it, so the binary is static and needs
# no libc at runtime.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /app ./sites/analytics.bythewood.me

# --- image --------------------------------------------------------------
# alpine, not scratch.
#
# The other two sites in this repo end at FROM scratch. This one cannot: it
# shells out to typst on the request path, and typst needs real font files on
# disk. Those are the same three font packages the Rust image installed for
# the same reason, back when Typst was linked in rather than executed.
FROM alpine:3.23

RUN apk add --no-cache \
      ca-certificates \
      font-jetbrains-mono \
      ttf-dejavu \
      ttf-liberation \
      fontconfig && \
    fc-cache -f

COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
COPY --from=build /app /app
COPY --from=frontend /dist /dist
COPY --from=maps /static_maps /static_maps
# Typst resolves absolute paths in the generated source against SITE_ROOT, so
# it doubles as the fence on what a compile is allowed to read.
WORKDIR /srv
COPY sites/analytics.bythewood.me/reports/ /srv/reports/

ENV SITE_DIST=/dist
ENV SITE_MAPS=/static_maps
ENV SITE_DATA=/data
ENV SITE_ROOT=/srv
# fontconfig wants somewhere writable; without it typst warns on every compile.
ENV XDG_CACHE_HOME=/tmp

# The database and the GeoIP mmdb. A volume, not a bind mount: the daemon here
# is Docker Desktop on the Windows host and resolves paths against its own
# filesystem, so a bind mount of a path under /home/dev silently becomes an
# empty directory.
VOLUME ["/data"]

EXPOSE 8000
ENTRYPOINT ["/app"]
