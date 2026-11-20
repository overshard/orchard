# llm.bythewood.me

The model gateway. Every service that wants a model asks here with an API key.
With `ANTHROPIC_API_KEY` in `.env` the answers come from Claude Haiku 5.5 with
web search, translated from the OpenAI shape the callers speak, and pictures are
off. Without one it runs one graphics card, Ornith for chat and FLUX.2 klein for
pictures taking turns on it behind llama-swap. Every prompt and completion is logged for 90
days unless the caller sends `X-Incognito: 1`, and the keys and the log are
behind auth.bythewood.me.

## Requirements

Go, and a `.env` from `.env.example`. Claude needs only the API key, and
everything sent to it leaves this machine. The local model server needs
`COMPOSE_PROFILES=local`, an NVIDIA card, the NVIDIA container toolkit, and the
weights in the `orchard-llm-models` volume at the paths in
`modelserver/config.yaml`.

## Running it

```sh
make run      go run, on :8000
make build    release binary into ../../bin/llm.bythewood.me
make clean    delete that binary
```

`make llm-key NAME=<service>` at the root mints a key and prints it once.
`make dash-key pond-key cron-key` mints those sites' keys into their `.env`
files, or hands llm back the ones already there, which is all it takes after
llm's volume is lost.

## Layout

`proxy.go` checks the key and forwards OpenAI style calls upstream,
`anthropic.go` translates them to the Messages API and back, `db.go`
holds the keys and the log, `spend.go` the prices and the ledger, `stats.go`
and `pages.go` the overview, calls and keys pages, `images.go` the picture calls and `gpu.go` asks the
probe whether the desktop is using the card and turns away calls marked
`X-Background: 1` while it is. `modelserver/` builds llama.cpp,
stable-diffusion.cpp and llama-swap, and `gpuprobe/` is the `nvidia-smi`
sidecar. Templates and `static/` are written by hand, with no Vite.

## Things that will bite you

- Haiku 5.5 refuses sampling parameters, so temperature and the rest are
  dropped, and structured outputs refuse length and count limits, so those are
  stripped from a schema and the prompt has to carry them.
- Web search is offered only on calls that offer tools and don't stream.
- A safety refusal comes back as a 422, never an empty answer.
- `LLM_BUDGET` is a hard stop as well as the ntfy alerts, so once the month's
  spend reaches it every Claude call and search gets a 429 until the 1st, or
  until the budget is raised in `.env` and the site redeployed. Each key also
  gets a burst of 10 calls and 1 a second after that, and `max_tokens` is
  clamped to 16384.
- A key can have a monthly cap on the keys page. Once that key's spend this
  month reaches it, its calls get a 422 with `insufficient_quota` until the
  1st. No cap means no limit, which is how dash, cron and pond run.
- Each spend row points at the call it paid for, so a call's cost is exact.
  Calls from before that link existed are priced from their token counts with
  the whole prompt as uncached input, and show with a `~`. Time to first token
  is only known for streamed calls, and a call that didn't stream shows its
  total alone.

- The model server and the probe sit on a private network with no auth of
  their own, and only the gateway joins the edge.
- `swap` and `gpu` only resolve inside compose, so a dev run wants
  `LLM_UPSTREAM` and `LLM_GPU` pointed at real ones.
- Background calls are turned away for 15 minutes after the gateway starts,
  since a start is usually a reboot and the game comes back after the
  containers. A deploy does the same, and calls not marked background aren't
  affected.
- The model server is built for a 3070's sm_86, and another card means changing
  `CMAKE_CUDA_ARCHITECTURES` in its Dockerfile.
- A Windows NVIDIA driver update moves the driver store, and a container started
  before the CDI spec is regenerated runs the model on the CPU. Health fails
  when that happens, and the host's `nvidia-cdi-refresh-wsl.timer` from
  taproot's `make wsl` regenerates the spec and restarts what's unhealthy.
