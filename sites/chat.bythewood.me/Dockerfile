# chat.bythewood.me.
#
# This one ends at Alpine rather than scratch, for poppler's pdftotext. The pure
# Go reader cannot open an encrypted pdf, which is what a bank statement usually
# is, and it loses the column alignment that makes a statement readable at all.
# Everything else here is still parsed in process.
FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -tags embed swaps the disk asset loader for the embedded one, so the shipped
# image cannot read templates off a filesystem that is not there, and the
# development bypass in assets_disk.go cannot be turned on however the
# environment is set.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

# poppler-utils is pdftotext and nothing else here needs a shell. ca-certificates
# because every tool fetches over TLS, and tzdata because the ambient block
# states a local time.
RUN apk add --no-cache ca-certificates poppler-utils tzdata

COPY --from=build /app /app

# chown before the VOLUME, and the order matters: Docker seeds a fresh volume
# from the image path it mounts over and carries its ownership. Without it the
# mountpoint is root-owned and the first boot dies on a database open error.
RUN mkdir -p /data && chown 65532:65532 /data
VOLUME ["/data"]

# UID 65532 like the scratch images here, so one number owns every /data volume.
RUN addgroup -g 65532 app && adduser -u 65532 -G app -D -H app
USER 65532:65532

# pdftotext writes its temporary files under the working directory otherwise.
ENV TMPDIR=/tmp

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENTRYPOINT ["/app"]
