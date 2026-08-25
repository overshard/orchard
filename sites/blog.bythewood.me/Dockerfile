# Build context is the repo root, because the Go module and internal/web live
# there. That is the cost of a monorepo and it is a small one: the alternative
# is a go.mod per site and the shared package copied into each.

# --- typst --------------------------------------------------------------
# The PDF compiler, pinned by version and verified by hash.
#
# There is no Go binding to Typst, so decisions/0008 left this open with the
# CLI as the recommendation. This is that, with one choice on top: it runs
# here, at build time, rather than on the request path. Posts load once at
# startup and cannot change while the process runs, so every PDF the site will
# ever serve is known before it boots. Compiling them now costs a second, keeps
# a 50MB static binary out of the runtime image entirely, and turns the PDF
# route into serving a file.
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
COPY sites/blog.bythewood.me/frontend/package.json sites/blog.bythewood.me/frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY sites/blog.bythewood.me/frontend/ ./
RUN bun run build

# --- binary and PDFs ----------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
# The same fonts the Rust runtime image installed, so the PDFs keep the faces
# blog_post.typ asks for. Typst embeds only DejaVu Sans Mono and its own serif,
# and would silently fall back for everything else.
RUN apk add --no-cache font-jetbrains-mono ttf-dejavu ttf-liberation

COPY go.mod go.sum ./
COPY internal/ ./internal/
COPY sites/blog.bythewood.me/*.go ./sites/blog.bythewood.me/
COPY sites/blog.bythewood.me/templates/ ./sites/blog.bythewood.me/templates/
# Static, stripped, reproducible. CGO off is what makes FROM scratch possible.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /app ./sites/blog.bythewood.me

# Typst resolves the absolute paths in the generated source (/typst/... and
# /content/images/...) against --typst-root, so the two directories have to sit
# under it with the names the source uses.
WORKDIR /src/sites/blog.bythewood.me
COPY sites/blog.bythewood.me/typst/ ./typst/
COPY sites/blog.bythewood.me/content/ ./content/
RUN SITE_CONTENT=./content /app -pdfs /pdfs -typst-root .

# --- image --------------------------------------------------------------
FROM scratch

COPY --from=build /app /app
COPY --from=build /pdfs /pdfs
COPY --from=frontend /dist /dist
# Posts are the data this site serves, so they ship with it: parsed at startup,
# and read back off disk for the raw Markdown route.
COPY sites/blog.bythewood.me/content/ /content/

ENV SITE_DIST=/dist
ENV SITE_PDFS=/pdfs
ENV SITE_CONTENT=/content
EXPOSE 8000
ENTRYPOINT ["/app"]
