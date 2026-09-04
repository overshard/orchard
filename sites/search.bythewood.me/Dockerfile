# Build context is this directory alone: the site is its own Go module with its
# own copy of web/. Base images are pinned by digest because :1-alpine floats.

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY web/ ./web/
COPY *.go ./
COPY templates/ ./templates/
COPY static/ ./static/
# -tags embed swaps assets_disk.go for assets_embed.go, so the templates and the
# stylesheet are compiled in and the binary is the whole site.
RUN CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w" -o /app .

# scratch, since modernc.org/sqlite is SQLite transpiled to Go rather than bound
# to it, so nothing here needs libc. Certificates are the only other thing this
# process touches on disk, and it fetches over TLS constantly.
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app /app

# Zone data, because the ambient context block states a local time and a bare
# scratch image has no /usr/share/zoneinfo to resolve America/New_York against.
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /zoneinfo.zip
ENV ZONEINFO=/zoneinfo.zip

ENV SITE_DATA=/data
# The model, which is the other service in this project's compose file.
ENV LLM_URL=http://llm:8091

# 65532 like the other scratch images here, so one number owns every /data
# volume. There is no adduser on scratch, so the numeric id is set directly and
# the directory is created in the build stage below.
COPY --from=build --chown=65532:65532 /tmp /data
USER 65532:65532
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app", "-healthcheck"]

EXPOSE 8000
ENTRYPOINT ["/app"]
