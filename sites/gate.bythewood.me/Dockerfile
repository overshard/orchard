# Templates are compiled in, so the binary needs nothing beside it but the CA
# bundle for fetching a client's metadata document over TLS.
FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build
WORKDIR /src
COPY go.mod go.su[m] ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app .

RUN mkdir -p /seed-data

FROM scratch
COPY --from=build /app /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8000

# A fresh volume copies this directory's owner, so the database is writable
# by 65532 without a chown.
COPY --from=build --chown=65532:65532 /seed-data /data
USER 65532:65532
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
