# dash.bythewood.me

A public dashboard. Markets off Yahoo, Hacker News and Lobsters, the weather,
earnings, whether the other sites are answering, and a markets line and news
summary the local model writes four times a day. One poller per source pushes
the whole state to every open browser over server sent events, so ten tabs cost
the upstreams the same as one.

## Requirements

Go and bun. The `.env` carries `LLM_KEY`, dash's key for llm.bythewood.me,
which `make dash-key` at the root mints. Empty, the page says the briefs are off.

## Running it

```sh
make run      vite watch + go run, on :8000
make build    release binary into ../../bin/dash.bythewood.me
make clean    delete build/ and node_modules
```

## Layout

`market.go`, `news.go`, `weather.go`, `earnings.go`, `systems.go` and the rest
each poll one thing into the store in `state.go`, which broadcasts it on
`/events`. `sources.go` lists every poller with its interval, so a new panel is
one entry there. `brief.go` runs the briefs on the model, and each desk writes
its own in `briefmarkets.go`, `briefnews.go`, `feeds.go` and `glance.go`, with
`briefstories.go` grouping the outlets' stories into events for the first two.
`quick.go` relays the quick chat to chat.bythewood.me, and `guard.go` fences
every upstream with a budget and a breaker. There's no database, just JSON files
in `build/data`, or the `orchard-dash-data` volume in production. `CLAUDE.md`
has the rest.

## Things that will bite you

- Every upstream is free, keyless and somebody else's, so don't loosen a
  budget in `guard.go` to make a dev run feel faster.
- The health strip asks each site by container name first, and a row built
  from the public hostname says `cached`, since Cloudflare will answer
  `/healthz` long after the site behind it stopped.
- `web/server.go` here is a variant of its own, with no write bound and open
  streams ended on shutdown.
- A new site shows on the strip once it's in `monitored` in `systems.go`.
