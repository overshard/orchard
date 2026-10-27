# chat.bythewood.me.
#
# This one ends at Debian rather than scratch, for poppler's pdftotext. The pure
# Go reader cannot open an encrypted pdf, which is what a bank statement usually
# is, and it loses the column alignment that makes a statement readable at all.
# Everything else here is still parsed in process.
FROM golang:1.27-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -tags embed swaps the disk asset loader for the embedded one, so the shipped
# image cannot read templates off a filesystem that is not there, and the
# development bypass in assets_disk.go cannot be turned on however the
# environment is set.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

# poppler-utils is pdftotext and nothing else here needs a shell. ca-certificates
# because every tool fetches over TLS, and tzdata because the ambient block
# states a local time.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      poppler-utils \
      tzdata \
      && rm -rf /var/lib/apt/lists/*

COPY --from=build /app /app

# chown before the VOLUME, and the order matters: Docker seeds a fresh volume
# from the image path it mounts over and carries its ownership. Without it the
# mountpoint is root-owned and the first boot dies on a database open error.
RUN mkdir -p /data && chown 65532:65532 /data
VOLUME ["/data"]

# UID 65532 like the scratch images here, so one number owns every /data volume.
RUN groupadd -g 65532 app && \
    useradd -u 65532 -g app -M -s /usr/sbin/nologin app
USER 65532:65532

# pdftotext writes its temporary files under the working directory otherwise.
ENV TMPDIR=/tmp

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
