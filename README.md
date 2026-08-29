# orchard

Every site I host, in one repo, together with the Cloudflare Tunnel and Caddy
that front them. Adding or removing a site is a directory and a compose file
rather than a new repository with its own deploy hook and its own conventions.

All of it runs on a desktop at home. Nothing listens on an inbound port.

| Directory | Site |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Go and Vite, with no third party Go dependencies |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card are built per post |
| `sites/analytics.bythewood.me/` | Analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Uptime monitoring. SQLite, Lighthouse audits, crawler |
| `edge/` | The shared `cloudflared` tunnel and the Caddy behind it |

## Commands

```sh
make run SITE=blog.bythewood.me      # vite watch + go run, on :8000
make build SITE=blog.bythewood.me    # frontend, then a release binary in bin/
make deploy SITE=blog.bythewood.me   # docker compose up --build --detach
make check                           # gofmt, then vet and build every site
make test                            # every site's tests
make edge-up                         # the shared tunnel and Caddy
make sites                           # list them
```

Every site has the same targets in its own `Makefile`, so you can work inside
one without going through the root.

## Layout

Each site is its own Go module and builds on its own. There is no module at the
repo root; `go.work` exists so repo-wide `make` targets and an editor can see
all four at once, and nothing depends on it:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

Each site also carries its own copy of `web/`, the small HTTP layer they all
need: the Vite manifest reader, request logging, panic recovery, security
headers, cache policy and graceful shutdown. This used to be one shared package
under a single root module. Splitting it means a site is a directory you can
lift into its own repository, and its Docker build context is that directory
instead of the whole monorepo. The cost is that a fix in `web/` has to be made
four times.

## How a site works

Go serves every request and `html/template` renders the pages. Vite is a build
step and never a server: it writes content-hashed JS and CSS into `build/dist/`,
and the Go binary reads `build/dist/.vite/manifest.json` to turn those hashes
into script and link tags. A missing manifest stops the server from starting,
rather than serving pages whose script tags point at files that were never
built.

Typst does the typesetting, always at build time: post PDFs, the resume, and the
1200x630 PNG each site serves as its `og:image`. Geist is the typeface,
installed with `bun add geist`, because Typst reads TrueType and the
`@fontsource` packages ship woff2 only.

### build/ and the embed tag

Everything a tool generates lands in `sites/<name>/build/`, and nothing else
does, so the `.go` files sit beside the generated output instead of among it.
`build/` is gitignored, and `make clean` deletes it.

A development build reads that directory off disk, which is what lets Vite
rewrite a stylesheet under a running server. `make build` passes `-tags embed`,
swapping `assets_disk.go` for `assets_embed.go` so `//go:embed` compiles
`build/` into the executable. `blog` and `isaacbythewood.com` come out as a
single file that runs from an empty directory. `analytics` and `status` embed
the same way but still need `typst` and Chromium beside them, since those are
programs rather than assets.

The build tag is what keeps a fresh clone building. `//go:embed` resolves at
compile time and fails on a directory that does not exist, so an unconditional
directive would break `go build ./...` until someone had run Vite once.
Templates and the blog's posts are embedded unconditionally, since they are
source rather than build output.

## The edge

One `cloudflared` and one Caddy serve the whole repo. Traffic is outbound only:
no port forwarding, no dynamic DNS, no inbound firewall rules, and the origin IP
is never published.

```sh
cd edge
sh setup-tunnel.sh login     # once per machine
sh setup-tunnel.sh up        # create the tunnel and its DNS records
docker compose up --build --detach
```

Adding a hostname takes three changes: a site block in `edge/caddy/Caddyfile`,
an ingress rule in `edge/cloudflared/config.yml`, and a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`. Both configs are baked into their images, so a
change needs a rebuild.

Caddy sits between cloudflared and the apps so compression, security headers and
per-host routing are configured in one place.

## Caching

The two content sites send:

```
public, max-age=300, stale-while-revalidate=86400, stale-if-error=604800
```

`stale-if-error` is the directive that earns its place when the origin is a
desktop in a house: the edge keeps serving the last good copy for a week instead
of an error page. There is no `s-maxage`, because RFC 9111 gives it
`proxy-revalidate` semantics and Cloudflare then disables both stale directives.
Responses of 400 and above get an explicit `no-store`, so an error cannot be
pinned at the edge.

## Containers

All four run as UID 65532, with base images pinned by digest. The two Alpine
images create a real user at that UID; the two `FROM scratch` images use the
bare number, since there is no `/etc/passwd` to name a user in. A `/data` volume
created root-owned stays root-owned and needs a one-time
`chown -R 65532:65532`.

Each binary is its own health check. `-healthcheck` does a loopback GET against
`/healthz` and exits 0 or 1, which is what a `FROM scratch` image needs, having
no shell for `HEALTHCHECK` to call.

## Secrets and state

Nothing here reads a config file. Site identity (hostname, contact address,
theme) is a constant in each site's `site.go`, so there is no `.env.example` and
no `BASE_URL` indirection.

Two passwords exist, and both come from the deploying shell rather than an
`.env` beside the compose file:

```sh
ANALYTICS_PASSWORD=... make deploy SITE=analytics.bythewood.me
STATUS_PASSWORD=...    make deploy SITE=status.bythewood.me
```

The tunnel's credential lives in a Docker volume that `setup-tunnel.sh` creates.
The remaining environment variables (`SITE_DATA`, `SITE_ROOT`, `CHROMIUM_BIN`)
are paths set by the Dockerfiles, with defaults that work in a local checkout.

`analytics` and `status` each keep a SQLite database in a named Docker volume
mounted at `/data`; nothing else here has state. Named volumes rather than bind
mounts, because the daemon is Docker Desktop on the Windows host and resolves
bind paths against its own filesystem, where a path under `/home/dev` silently
becomes an empty directory.

## Contributing

`CLAUDE.md` has the working detail: the rule about `web/`, the conventions that
bite, and the mistakes that have already been made once.
