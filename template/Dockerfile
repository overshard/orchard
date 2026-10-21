# Build context is this directory alone, since the site is its own Go module with
# its own copy of web/. Base images are pinned by digest because their tags float.

FROM oven/bun:1-debian@sha256:923bedb355f7746666659f71e724998159a5c8c54bd1cb1030e1975a0698415b AS frontend
WORKDIR /src
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.27-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS build
WORKDIR /src
# The bracket glob makes go.sum optional, since a plain COPY go.sum fails the
# build outright for a module with no third party dependency.
COPY go.mod go.su[m] ./
RUN go mod download
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
# -tags embed reads build/dist at compile time, so the bundle has to land here
# before the compile below.
COPY --from=frontend /build/dist ./build/dist
# CGO off is what makes FROM scratch possible.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" \
    -o /app .

FROM scratch

# Templates and the Vite bundle are inside the executable. Copy
# /etc/ssl/certs/ca-certificates.crt from the build stage the day this fetches
# anything over TLS, or every such fetch fails x509.
COPY --from=build /app /app

# There is no /etc/passwd in a scratch image to name a user in, so this is the
# bare numeric UID:GID.
USER 65532:65532

# No shell in a scratch image for HEALTHCHECK to call, so the binary probes
# itself. Compose sets the same check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
