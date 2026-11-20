# One image for the site and the DoT front, which runs it with -dot. Templates
# are compiled in, so it needs nothing beside it but the CA bundle for fetching
# the lists and talking to Let's Encrypt and Cloudflare.
FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build
WORKDIR /src
COPY go.mod go.su[m] ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app .

RUN mkdir -p /seed-data /seed-tls

FROM scratch
COPY --from=build /app /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8000 8853

# A fresh volume copies these directories' owner, so both are writable by
# 65532 without a chown.
COPY --from=build --chown=65532:65532 /seed-data /data
COPY --from=build --chown=65532:65532 /seed-tls /tls
USER 65532:65532
VOLUME ["/data", "/tls"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
