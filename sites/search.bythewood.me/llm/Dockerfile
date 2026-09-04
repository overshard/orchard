# The model server for search.bythewood.me: llama.cpp behind llama-swap.
#
# This is its own image rather than a borrowed one. The site is an independent
# entity like every other site in orchard, and it was previously running off
# taproot's aiagent image, which is a development container carrying node, the
# Docker CLI, tmux and a coding agent, none of which belong in a service.
#
# llama-swap is what makes the model on demand. It fronts llama.cpp, starts the
# model when a request names it, and unloads after a TTL, freeing the card for
# whatever else the machine is doing. Without it the weights sit resident
# forever, which on a desktop that is also a workstation is the card gone.

FROM debian:trixie-slim AS build

# CUDA 13.2 produces gibberish from low-bit quants, so pin away from it.
ARG CUDA=13-3

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl git cmake ninja-build build-essential \
        libcurl4-openssl-dev && \
    curl -fsSLo /tmp/keyring.deb https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb && \
    dpkg -i /tmp/keyring.deb && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        cuda-nvcc-${CUDA} cuda-cudart-dev-${CUDA} libcublas-dev-${CUDA} && \
    rm -rf /var/lib/apt/lists/* /tmp/keyring.deb

ENV PATH=/usr/local/cuda/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# sm_86 alone keeps the build short, and it is the 3070 this runs on. Change
# CMAKE_CUDA_ARCHITECTURES for another card.
#
# FA_ALL_QUANTS is what makes flash attention work with a quantized KV cache,
# which is what fits 32k of context on 8GB.
#
# Static libs because upstream ships shared objects with no SONAME, which
# ldconfig then refuses to cache, and the resulting failure names a missing
# .so rather than the cause.
ARG LLAMA_REF=master
RUN git clone --depth 1 --branch ${LLAMA_REF} https://github.com/ggml-org/llama.cpp /tmp/src && \
    cmake -S /tmp/src -B /tmp/build -G Ninja \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_SHARED_LIBS=OFF \
        -DGGML_CUDA=ON \
        -DCMAKE_CUDA_ARCHITECTURES=86 \
        -DGGML_CUDA_FA_ALL_QUANTS=ON \
        -DGGML_NATIVE=OFF \
        -DLLAMA_CURL=ON \
        -DLLAMA_BUILD_TESTS=OFF \
        -DLLAMA_BUILD_EXAMPLES=OFF && \
    cmake --build /tmp/build --target llama-server -j"$(nproc)" && \
    strip /tmp/build/bin/llama-server

# llama-swap ships a static Go binary, so it is fetched rather than built.
# Pinned by digest: a release asset can be replaced in place.
ARG SWAP_VERSION=252
ARG SWAP_SHA256=5f858d72a90ae28aa7c75d34df25d05f9d7daec8c47b9503fb8a264bc6d95d25
RUN curl -fsSL -o /tmp/swap.tgz \
        "https://github.com/mostlygeek/llama-swap/releases/download/v${SWAP_VERSION}/llama-swap_${SWAP_VERSION}_linux_amd64.tar.gz" && \
    echo "${SWAP_SHA256}  /tmp/swap.tgz" | sha256sum -c - && \
    tar -xzf /tmp/swap.tgz -C /tmp && \
    install -m 0755 /tmp/llama-swap /usr/local/bin/llama-swap


FROM debian:trixie-slim

ARG CUDA=13-3

# libgomp1 and libcurl4 are llama-server's runtime needs; curl is the
# healthcheck, since this image has no shell-less alternative.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl libgomp1 libcurl4 && \
    curl -fsSLo /tmp/keyring.deb https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb && \
    dpkg -i /tmp/keyring.deb && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        cuda-cudart-${CUDA} libcublas-${CUDA} && \
    rm -rf /var/lib/apt/lists/* /tmp/keyring.deb

COPY --from=build /tmp/build/bin/llama-server /usr/local/bin/llama-server
COPY --from=build /usr/local/bin/llama-swap /usr/local/bin/llama-swap
COPY config.yaml /etc/llama-swap/config.yaml

# A malformed config would otherwise surface as a container that starts and
# then exits during a deploy. This turns it into a failed build instead.
RUN llama-swap --config /etc/llama-swap/config.yaml --validate

# Weights live on a volume, not in the image, so a rebuild does not re-download
# several gigabytes.
ENV LLAMA_CACHE=/models

# 65532 like the other images here, so one number owns every volume.
RUN groupadd -g 65532 app && useradd -u 65532 -g app -M -d /models app && \
    mkdir -p /models && chown 65532:65532 /models
VOLUME ["/models"]
USER 65532:65532

# /v1/models lists what is configured without loading anything, so the
# healthcheck never keeps the model awake. Asking /health would defeat the TTL
# this image exists to provide.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8091/v1/models >/dev/null || exit 1

EXPOSE 8091
ENTRYPOINT ["llama-swap", "--config", "/etc/llama-swap/config.yaml", "--listen", "0.0.0.0:8091"]
