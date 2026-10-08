# How busy the card is, for callers that want to stay off it while the desktop
# is using it. Debian rather than scratch, since the nvidia-smi the container
# toolkit injects is linked against glibc.
FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY gpuprobe ./gpuprobe
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gpuprobe ./gpuprobe

FROM debian:trixie-slim
COPY --from=build /gpuprobe /gpuprobe
USER 65532:65532
EXPOSE 8092
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/gpuprobe", "-healthcheck"]
ENTRYPOINT ["/gpuprobe"]
