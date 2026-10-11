# How busy the card is, for callers that want to stay off it while the desktop
# is using it. Debian rather than scratch, since the nvidia-smi the container
# toolkit injects is linked against glibc.
FROM golang:1.27-trixie@sha256:2f84bc93ecfb2689f782b153fdcd368b5a7ab96c1386c65cdaccf35e726d6a44 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY gpuprobe ./gpuprobe
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gpuprobe ./gpuprobe

FROM debian@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
COPY --from=build /gpuprobe /gpuprobe
USER 65532:65532
EXPOSE 8092
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/gpuprobe", "-healthcheck"]
ENTRYPOINT ["/gpuprobe"]
