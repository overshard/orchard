# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-debian floats.

FROM debian@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a AS typst
ARG TYPST_VERSION=0.14.0
ARG TYPST_SHA256=99816d2982de08d2b091bac56b59b2faa523a10e1378ad3cdd68e35b8eb74b3d
RUN apt-get update && \
    apt-get install -y --no-install-recommends curl ca-certificates tar xz-utils && \
    rm -rf /var/lib/apt/lists/* && \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" && \
    echo "${TYPST_SHA256}  /tmp/typst.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/typst.tar.xz -C /tmp && \
    mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst

# The resume is build output, so no compiler is needed at runtime. ttf-liberation
# is required: resume.typ names Liberation Sans and Typst falls back without
# saying so, which moves every line break.
FROM typst AS resume
RUN apt-get update && \
    apt-get install -y --no-install-recommends fonts-liberation fontconfig && \
    rm -rf /var/lib/apt/lists/* && \
    fc-cache -f
COPY resume/ /resume/
RUN typst compile --root /resume /resume/resume.typ /resume-isaac-bythewood.pdf

# Its own stage because both the frontend build and the card need node_modules,
# and having the card read it out of the frontend stage would be a cycle.
FROM oven/bun:1-debian@sha256:923bedb355f7746666659f71e724998159a5c8c54bd1cb1030e1975a0698415b AS deps
WORKDIR /src
# bunfig.toml has to arrive before install runs, or install.peer = false
# silently does nothing.
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./
RUN bun install --frozen-lockfile

# PNG, not SVG, since no social platform accepts image/svg+xml for og:image.
# Geist comes out of the bun install because @fontsource ships woff2 only and
# Typst reads TrueType.
FROM typst AS card
COPY --from=deps /src/node_modules/geist/dist/fonts/geist-sans /fonts/geist-sans
COPY typst/card.typ /card.typ
RUN typst compile --format png --ppi 72 --font-path /fonts/geist-sans /card.typ /card.png

FROM deps AS frontend

# Images are generated in their own layer, before the rest of the frontend is
# copied in, so editing CSS or JS does not re-encode 22 variants every deploy.
COPY frontend/scripts/ ./scripts/
COPY images.json ../images.json
COPY frontend/images/ ./images/
RUN bun run images

COPY frontend/ ./
# After the frontend copy, not before: publicDir is part of that copy, so
# landing the PDF first would get it overwritten. `**/pdfs` in .dockerignore
# keeps a stale host copy out of the context.
COPY --from=resume /resume-isaac-bythewood.pdf ./public/pdfs/resume-isaac-bythewood.pdf
# Same ordering, and `**/og` in .dockerignore for the same reason.
COPY --from=card /card.png ./public/og/card.png
RUN bun run vite

FROM golang:1.27-trixie@sha256:2f84bc93ecfb2689f782b153fdcd368b5a7ab96c1386c65cdaccf35e726d6a44 AS build
WORKDIR /src
# The bracket glob makes go.sum optional, since a plain COPY go.sum fails the
# build outright for a module with no third party dependency.
COPY go.mod go.su[m] ./
COPY web/ ./web/
COPY *.go ./
# Embedded by images.go, and shared with the frontend image generator.
COPY images.json ./
COPY templates/ ./templates/
# -tags embed reads build/dist at compile time, so the bundle has to land here
# before the compile below.
COPY --from=frontend /build/dist ./build/dist
# CGO off is what makes FROM scratch possible, and -tags embed is what makes the
# image a single file.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

FROM scratch

# The code page fetches from api.github.com over TLS and scratch has no trust
# store, so without this the commit fetch fails x509 on every refresh.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Templates and the Vite bundle are inside the executable.
COPY --from=build /app /app

# There is no /etc/passwd in a scratch image to name a user in, so this is the
# bare numeric UID:GID. Nothing here reads or writes the disk.
USER 65532:65532

# No shell in a scratch image for HEALTHCHECK to call, so the binary probes
# itself. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
