# llm.bythewood.me. Two stages and nothing in the last one but the binary, its
# certificates and the timezone database.
FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -tags embed swaps the disk asset loader for the embedded one, so the shipped
# image cannot read templates off a filesystem that is not there.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/
COPY --from=build /app /app
ENV ZONEINFO=/usr/local/go/lib/time/zoneinfo.zip
EXPOSE 8000
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]
ENTRYPOINT ["/app"]
