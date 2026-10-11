# llm.bythewood.me. Two stages and nothing in the last one but the binary, its
# certificates and the timezone database.
FROM golang:1.27-trixie@sha256:2f84bc93ecfb2689f782b153fdcd368b5a7ab96c1386c65cdaccf35e726d6a44 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -tags embed swaps the disk asset loader for the embedded one, so the shipped
# image cannot read templates off a filesystem that is not there.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# A scratch image cannot RUN mkdir, so the mountpoint has to arrive as a
# directory copied from a stage that can. Docker seeds a fresh volume from the
# image path it mounts over and carries its ownership, so without this the
# mountpoint is root-owned and the first write fails.
RUN mkdir -p /seed-data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/
COPY --from=build /app /app
ENV ZONEINFO=/usr/local/go/lib/time/zoneinfo.zip
EXPOSE 8000

# UID 65532 like the other scratch images here, so one number owns every /data
# volume. There is no /etc/passwd to name a user in, so it is the bare number.
COPY --from=build --chown=65532:65532 /seed-data /data
USER 65532:65532
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENV SHIP_LOGS=1
ENTRYPOINT ["/app"]
