# How busy the card is, for callers that want to stay off it while the desktop
# is using it. Debian rather than scratch, since the nvidia-smi the container
# toolkit injects is linked against glibc.
FROM golang:1.27-trixie@sha256:c47c3fda018d13d43a6bf5ebac3e5bba8a23a35782aa4faf531afaf47c64c39f AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY gpuprobe ./gpuprobe
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gpuprobe ./gpuprobe

FROM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad
COPY --from=build /gpuprobe /gpuprobe
USER 65532:65532
EXPOSE 8092
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/gpuprobe", "-healthcheck"]
ENTRYPOINT ["/gpuprobe"]
