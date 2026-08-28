# Build context is the repo root, because the Go module and internal/web live
# there. That is the cost of a monorepo and it is a small one: the alternative
# is a go.mod per site and the shared package copied into each.
#
# This is the heaviest image in the repo by a wide margin, and every kilogram
# of it is Lighthouse. The audit is Google's own JavaScript driving a real
# Chromium over DevTools; there is no in-process substitute in any language,
# so the runtime carries a browser, a JavaScript runtime and the CLI itself.
# The other three sites are 25MB, 44MB and 161MB.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash.
#
# Copied into the runtime image rather than used and discarded during the
# build, for the same reason as analytics: a property report describes a live
# monitoring state that changes every three minutes, so unlike a blog post's
# PDF there is no finite set to precompile. See typst.go.
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
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# --- lighthouse ---------------------------------------------------------
# Its own stage, and deliberately separate from the frontend one, so the layer
# cache survives: this installs a large dependency tree that changes only when
# the Lighthouse version does, and rebuilding it on every CSS tweak would add
# minutes to each deploy.
FROM oven/bun:1-alpine AS lighthouse
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# The bracket glob makes go.sum optional. A module with no third party
# dependency has none for `go mod tidy` to write, and a plain `COPY go.sum`
# fails the build outright when the file is absent.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY reports/ ./reports/
# CGO off is what makes modernc.org/sqlite worth having: it is SQLite
# transpiled to Go rather than bound to it, so the binary is static and needs
# no libc at runtime.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /app .

# --- social card --------------------------------------------------------
# A build artifact: the card cannot change while the process runs, so there is
# no reason to render it per request. PNG rather than SVG because no social
# platform accepts image/svg+xml for og:image.
#
# Geist comes out of the frontend stage's bun install rather than a second
# download. @fontsource ships woff2 only and Typst reads TrueType, so the
# package that works is Vercel's own, which carries the .ttf files.
FROM typst AS card
COPY --from=frontend /src/node_modules/geist/dist/fonts/geist-sans /fonts/geist-sans
COPY typst/card.typ /card.typ
RUN typst compile --format png --ppi 72 --font-path /fonts/geist-sans /card.typ /card.png

# --- image --------------------------------------------------------------
FROM alpine:3.23

# chromium is for Lighthouse and nothing else. PDF is typst, which needs the
# three font packages rather than a browser; that split is why this image does
# not run headless Chrome to print a PDF the way the pre-Rust version did.
#
# There is no nodejs and no npm anywhere: `bun run --bun` symlinks node to bun,
# so the lighthouse shim's `#!/usr/bin/env node` shebang resolves to bun.
RUN apk add --no-cache \
      ca-certificates \
      chromium \
      font-jetbrains-mono \
      ttf-dejavu \
      ttf-liberation \
      fontconfig && \
    fc-cache -f

COPY --from=oven/bun:1-alpine /usr/local/bin/bun /usr/local/bin/bun
COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
COPY --from=build /app /app
COPY --from=frontend /dist /dist
# Served at /static/og/card.png by the same handler that serves the bundle,
# so the card needs no route of its own.
COPY --from=card /card.png /dist/og/card.png

# SITE_ROOT is where lighthouse.go looks for node_modules/.bin/lighthouse and
# where typst resolves absolute paths against, so it doubles as the fence on
# what a compile is allowed to read.
WORKDIR /srv
COPY --from=lighthouse /src/node_modules /srv/node_modules
COPY reports/ /srv/reports/

ENV SITE_DIST=/dist
ENV SITE_DATA=/data
ENV SITE_ROOT=/srv
# Chromium ships in the image, so there is no need to make findChromium walk
# anything: PATH resolution finds it immediately.
ENV CHROMIUM_BIN=/usr/bin/chromium-browser
# fontconfig wants somewhere writable; without it typst warns on every compile.
ENV XDG_CACHE_HOME=/tmp

# The SQLite database. A volume, not a bind mount: the daemon here is Docker
# Desktop on the Windows host and resolves paths against its own filesystem, so
# a bind mount of a path under /home/dev silently becomes an empty directory.
VOLUME ["/data"]

EXPOSE 8000
ENTRYPOINT ["/app"]
