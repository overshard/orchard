# llm.bythewood.me

The model gateway. One graphics card, Ornith for chat and FLUX.2 klein for
pictures taking turns on it behind llama-swap, and every service that wants a
model asks here with an API key. Every prompt and completion is logged for 90
days unless the caller sends `X-Incognito: 1`, and the keys and the log are
behind auth.bythewood.me.

## Requirements

Go. The model server needs an NVIDIA card, the NVIDIA container toolkit, and
the weights in the `orchard-llm-models` volume at the paths in
`modelserver/config.yaml`. No `.env`, since this is what mints keys.

## Running it

```sh
make run      go run, on :8000
make build    release binary into ../../bin/llm.bythewood.me
make clean    delete that binary
```

`make llm-key NAME=<service>` at the root mints a key and prints it once.

## Layout

`proxy.go` checks the key and forwards OpenAI style calls upstream, `db.go`
holds the keys and the log, `images.go` the picture calls and `gpu.go` asks the
probe whether the desktop is using the card. `modelserver/` builds llama.cpp,
stable-diffusion.cpp and llama-swap, and `gpuprobe/` is the `nvidia-smi`
sidecar. Templates and `static/` are written by hand, with no Vite.

## Things that will bite you

- The model server and the probe sit on a private network with no auth of
  their own, and only the gateway joins the edge.
- `swap` and `gpu` only resolve inside compose, so a dev run wants
  `LLM_UPSTREAM` and `LLM_GPU` pointed at real ones.
- The model server is built for a 3070's sm_86, and another card means changing
  `CMAKE_CUDA_ARCHITECTURES` in its Dockerfile.
