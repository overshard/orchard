# Build context is the repo root, because the Go module and internal/web live
# there. That is the cost of a monorepo and it is a small one: the alternative
# is a go.mod per site and the shared package copied into each.

# --- frontend -----------------------------------------------------------
FROM oven/bun:1-alpine AS frontend
WORKDIR /src
COPY sites/isaacbythewood.com/frontend/package.json sites/isaacbythewood.com/frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY sites/isaacbythewood.com/frontend/ ./
RUN bun run build

# --- binary -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY internal/ ./internal/
COPY sites/isaacbythewood.com/*.go ./sites/isaacbythewood.com/
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
