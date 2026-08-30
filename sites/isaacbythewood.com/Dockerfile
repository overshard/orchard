# The build context is this directory. Each site is its own Go module and
# carries its own copy of web/, so nothing above this folder is needed.
#
# Base images are pinned by digest, not only by tag: `oven/bun:1-alpine` floats
# across an entire major version, so two builds a month apart would not build
# the same thing. Bump these the same way the typst pin below is bumped.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash, the same way the
# other sites pin it.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apk add --no-cache curl tar xz && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

# --- resume -------------------------------------------------------------
# The resume is build output like the blog's post PDFs: its content is a file in
# the repo and cannot change while the process runs, so no compiler is needed on
# the request path or in the runtime image.
#
# ttf-liberation is required. resume.typ names Liberation Sans, and without the
# package Typst falls back without saying so and every line breaks somewhere
# else.
FROM typst AS resume
RUN apk add --no-cache ttf-liberation fontconfig && fc-cache -f
COPY resume/ /resume/
RUN typst compile --root /resume /resume/resume.typ /resume-isaac-bythewood.pdf

# --- frontend dependencies ----------------------------------------------
# Its own stage because two later stages need node_modules: the frontend build,
# and the social card that reads Geist out of it. Without the split the card
# would wait on the whole Vite build while the frontend build waits on the card,
# which is a cycle.
FROM oven/bun:1-alpine@sha256:07235578f79ef8c6f97d94aee7938e76f5cdba5f21ae5dbfdd3d3d38058437eb AS deps
WORKDIR /src
# bunfig.toml has to arrive before install, or install.peer = false silently
# does nothing here while working locally.
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./
RUN bun install --frozen-lockfile

# --- social card --------------------------------------------------------
# Build output like the resume, and a PNG for the same reason the blog's cards
# are: no social platform accepts image/svg+xml for og:image.
#
# Geist comes out of the bun install rather than a second download. @fontsource
# ships woff2 only and Typst reads TrueType, so the package that works is
# Vercel's own, which carries the .ttf files.
FROM typst AS card
COPY --from=deps /src/node_modules/geist/dist/fonts/geist-sans /fonts/geist-sans
COPY typst/card.typ /card.typ
RUN typst compile --format png --ppi 72 --font-path /fonts/geist-sans /card.typ /card.png

# --- frontend -----------------------------------------------------------
FROM deps AS frontend

# Images are generated in their own layer, before the rest of the frontend is
# copied in, so editing CSS or JS does not re-encode 22 variants on every
# deploy. They only regenerate when a source image actually changes.
COPY frontend/scripts/ ./scripts/
COPY images.json ../images.json
COPY frontend/images/ ./images/
RUN bun run images

COPY frontend/ ./
# After the frontend copy, not before: publicDir is part of that copy, so
# landing the PDF first would get it overwritten. `**/pdfs` in .dockerignore
# keeps a stale host copy out of the context, which makes this the only source
# of the file.
COPY --from=resume /resume-isaac-bythewood.pdf ./public/pdfs/resume-isaac-bythewood.pdf
# Same reasoning as the resume above, and `**/og` in .dockerignore is what
# keeps a stale host copy from winning.
COPY --from=card /card.png ./public/og/card.png
RUN bun run vite

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src
# The bracket glob makes go.sum optional. A module with no third party
# dependency has none for `go mod tidy` to write, and a plain `COPY go.sum`
# fails the build outright when the file is absent.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
# Embedded by images.go, and shared with the frontend image generator.
COPY images.json ./
COPY templates/ ./templates/
# The Vite bundle has to be on disk before the compile, not after: -tags embed
# reads build/dist at compile time and bakes it into the executable.
COPY --from=frontend /build/dist ./build/dist
# Static, stripped, reproducible. CGO off is what makes FROM scratch possible,
# and -tags embed is what makes the image a single file.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

# --- image --------------------------------------------------------------
FROM scratch

# The code page fetches from api.github.com over TLS, and scratch has no trust
# store at all. Without this the commit fetch fails x509 on every refresh and
# the cards silently render without commits.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# The whole site. Templates and the Vite bundle are inside the executable, so
# there is nothing to copy in beside it and no SITE_DIST to point anywhere.
COPY --from=build /app /app

# There is no /etc/passwd in a scratch image to name a user in, so this is the
# numeric UID:GID distroless calls "nonroot"; the kernel only needs the numbers.
# Nothing here writes to disk, and nothing is read from it either.
USER 65532:65532

# Docker cannot shell out to a health check in an image with no shell, so the
# binary probes itself. Compose sets the same check; this one makes the image
# describe its own liveness even when run without compose.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
