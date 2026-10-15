# orchard

Every site I host, in one repo, along with the Cloudflare Tunnel and Caddy that
front them. All of it runs on a desktop at home and nothing listens on an
inbound port.

| Directory | Site |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Go and Vite, no third party Go dependencies |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card are built per post |
| `sites/analytics.bythewood.me/` | Analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Uptime monitoring. SQLite, Lighthouse audits, crawler |
| `sites/logging.bythewood.me/` | Log aggregation. Every site ships its records here |
| `sites/repos.bythewood.me/` | Git remote you push to over HTTPS. Also mirrors GitHub |
| `sites/dash.bythewood.me/` | Dashboard. Markets, news, weather and the health of the rest, live over SSE |
| `sites/auth.bythewood.me/` | The front door. One account, a code pushed to a phone, one session for every site |
| `sites/chat.bythewood.me/` | Chat with the local model, with tools that read the rest of this repo, house lookups and pictures |
| `sites/llm.bythewood.me/` | The model gateway. One card behind an API key, a chat model and a picture model taking turns on it |
| `edge/` | The shared cloudflared tunnel, the Caddy behind it, and ntfy for alerts |

## Requirements

Docker, Go and bun, and a Cloudflare account with a zone you control, since
the whole thing is served through a tunnel. Everything else comes out of the
Dockerfiles.

## Running it

```sh
make install                         a machine that has never run this
make deploy SITE=blog.bythewood.me   rebuild one site and replace it
make up                              everything, from nothing or from broken
make doctor                          what's down and what to type
make run SITE=blog.bythewood.me      vite watch + go run, on :8000
make check                           gofmt, web/ drift, vet, govulncheck and build
make test                            every site's tests
make new SITE=notes.bythewood.me     a new site that builds, runs and deploys like the rest
```

`make help` lists the rest. `docs/install.md` walks through what install does
and how to finish one that stopped halfway, and `docs/edge.md` covers the
tunnel, Caddy and ntfy.

## Layout

Each site is its own Go module with its own copy of `web/`, the small HTTP
layer they all need, so a site is a directory you can lift into its own repo and
a fix in `web/` has to be made in every site. There's no root module, and
`go.work` is only there for editors. Go serves every request, Vite is a build
step that writes hashed assets into `build/dist/`, and Typst typesets the PDFs
and social cards at build time. Everything generated lands in
`sites/<name>/build/`, which is gitignored. `template/` is what `make new` copies a
new site from, and isn't a site itself.

## Things that will bite you

- Editing an `edge/` config and running `make up` does nothing, since those
  configs are baked into images. Use `make edge`.
- A `/data` volume created root-owned stays that way and needs a one time
  `chown -R 65532:65532`.
- Bind mounts of paths under `/home/dev` silently mount an empty directory,
  because the daemon resolves the path on the host. Use named volumes.
- `.env` is gitignored by bare name and this repo is public, so check it with
  `git check-ignore -v sites/<site>/.env` rather than trusting it.

`CLAUDE.md` has the rest, the conventions and the mistakes already made once.

## License

BSD 2-Clause. See `LICENSE.md`.
