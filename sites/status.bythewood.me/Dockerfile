# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-alpine floats.
#
# This is the heaviest image in the repo and Lighthouse is all of it: the audit
# is Google's own JavaScript driving a real Chromium over DevTools, so the
# runtime carries a browser, a JavaScript runtime and the CLI.

# Typst is copied into the runtime image rather than discarded after the build,
# because a report describes a live monitoring state that changes every three
# minutes, so there is nothing finite to precompile. See typst.go.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apk add --no-cache curl tar xz && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS frontend
WORKDIR /src
# bunfig.toml has to arrive before install runs, or install.peer = false
# silently does nothing.
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# Its own stage so a CSS change does not reinstall the Lighthouse dependency
# tree, which changes only when the Lighthouse version does.
FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS lighthouse
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# PNG, not SVG, since no social platform accepts image/svg+xml for og:image.
# Geist comes out of the frontend stage's node_modules because @fontsource ships
# woff2 only and Typst reads TrueType.
FROM typst AS card
COPY --from=frontend /src/node_modules/geist/dist/fonts/geist-sans /fonts/geist-sans
COPY typst/card.typ /card.typ
RUN typst compile --format png --ppi 72 --font-path /fonts/geist-sans /card.typ /card.png

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

# The bracket glob makes go.sum optional, since a plain COPY go.sum fails the
# build outright for a module with no third party dependency.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY reports/ ./reports/
# -tags embed reads build/dist at compile time, so the bundle has to land here
# before the compile below, the card included.
COPY --from=frontend /build/dist ./build/dist
COPY --from=card /card.png ./build/dist/og/card.png
# CGO off, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so the binary is static and needs no libc.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

# chromium is for Lighthouse and nothing else, and typst wants the font packages
# instead. No nodejs and no npm anywhere, because `bun run --bun` symlinks node
# to bun so the lighthouse shim's `#!/usr/bin/env node` shebang resolves.
RUN apk add --no-cache \
      ca-certificates \
      chromium \
      font-jetbrains-mono \
      ttf-dejavu \
      ttf-liberation \
      fontconfig && \
    fc-cache -f

COPY --from=oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb /usr/local/bin/bun /usr/local/bin/bun
COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
COPY --from=build /app /app

# SITE_ROOT is where lighthouse.go looks for node_modules/.bin/lighthouse and
# what typst resolves absolute paths against, so it doubles as the fence on what
# a compile is allowed to read.
WORKDIR /srv
COPY --from=lighthouse /src/node_modules /srv/node_modules
COPY reports/ /srv/reports/

ENV SITE_DATA=/data
ENV SITE_ROOT=/srv
# Chromium ships in the image, so findChromium has nothing to walk.
ENV CHROMIUM_BIN=/usr/bin/chromium-browser
# fontconfig wants somewhere writable, or typst warns on every compile.
ENV XDG_CACHE_HOME=/tmp

# chown before the VOLUME, and the order matters: Docker seeds a fresh volume
# from the image path it mounts over and carries its ownership. Without it the
# mountpoint is root-owned and the first boot dies on a database open error.
RUN mkdir -p /data && chown 65532:65532 /data
VOLUME ["/data"]

# UID 65532 like the scratch images here, so one number owns every /data volume.
# -H because this user has no home and writes only to /data and /tmp.
RUN addgroup -g 65532 app && adduser -u 65532 -G app -D -H app
USER 65532:65532

# The binary probes itself rather than shelling out to wget, which this image
# does have, because the scratch images cannot. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
