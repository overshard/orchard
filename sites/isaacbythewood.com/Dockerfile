# Build context is the repo root, because the Go module and internal/web live
# there. That is the cost of a monorepo and it is a small one: the alternative
# is a go.mod per site and the shared package copied into each.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash, exactly as
# blog.bythewood.me and analytics.bythewood.me pin it.
FROM alpine:3.23 AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apk add --no-cache curl tar xz && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

# --- resume -------------------------------------------------------------
# The resume is a build artifact like the blog's post PDFs: its content is a
# file in the repo and cannot change while the process runs, so there is no
# reason for a compiler on the request path or in the runtime image.
#
# ttf-liberation is not optional. resume.typ names Liberation Sans because that
# is the face the Chromium-rendered original actually embedded, so without the
# package Typst silently falls back and every line breaks somewhere else.
FROM typst AS resume
RUN apk add --no-cache ttf-liberation fontconfig && fc-cache -f
COPY resume/ /resume/
RUN typst compile --root /resume /resume/resume.typ /resume-isaac-bythewood.pdf

# --- frontend -----------------------------------------------------------
FROM oven/bun:1-alpine AS frontend
WORKDIR /src
COPY sites/isaacbythewood.com/frontend/package.json sites/isaacbythewood.com/frontend/bun.lock ./
RUN bun install --frozen-lockfile

# Images are generated in their own layer, before the rest of the frontend is
# copied in, so editing CSS or JS does not re-encode 22 variants on every
# deploy. They only regenerate when a source image actually changes.
COPY sites/isaacbythewood.com/frontend/scripts/ ./scripts/
COPY sites/isaacbythewood.com/images.json ../images.json
COPY sites/isaacbythewood.com/frontend/images/ ./images/
RUN bun run images

COPY sites/isaacbythewood.com/frontend/ ./
# After the frontend copy, not before: publicDir is part of that copy, so
# landing the PDF first would only get it overwritten. `**/pdfs` in
# .dockerignore keeps the host's stale copy out of the context, which is what
# makes this the only source of the file. That exclusion is also why the
# committed PDF never reached the image before this stage existed, and why the
# About page's resume link answered 404 on the deployed site while working
# locally.
COPY --from=resume /resume-isaac-bythewood.pdf ./public/pdfs/resume-isaac-bythewood.pdf
RUN bun run vite

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY internal/ ./internal/
COPY sites/isaacbythewood.com/*.go ./sites/isaacbythewood.com/
# Embedded by images.go, and shared with the frontend image generator.
COPY sites/isaacbythewood.com/images.json ./sites/isaacbythewood.com/
COPY sites/isaacbythewood.com/templates/ ./sites/isaacbythewood.com/templates/
# Static, stripped, reproducible. CGO off is what makes FROM scratch possible.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /app ./sites/isaacbythewood.com

# --- image --------------------------------------------------------------
FROM scratch

# The code page fetches from api.github.com over TLS, and scratch has no trust
# store at all. Without this the commit fetch fails x509 on every refresh and
# the cards silently render without commits.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /app /app
COPY --from=frontend /dist /dist

ENV SITE_DIST=/dist
EXPOSE 8000
ENTRYPOINT ["/app"]
