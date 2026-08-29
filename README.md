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
| `sites/logging.bythewood.me/` | Log aggregation. Every site ships its own records here; SQLite, retention, Typst PDF reports |
| `edge/` | The shared `cloudflared` tunnel and the Caddy behind it |

## Three situations

**Nothing running.** A machine where none of this is up yet, or one where it
all went down at once. The tunnel is a one-time step; everything after it is
`make up`.

```sh
sh edge/setup-tunnel.sh login      # once per machine, opens a browser
sh edge/setup-tunnel.sh up         # creates the tunnel and its DNS records
ANALYTICS_PASSWORD=... STATUS_PASSWORD=... LOGGING_PASSWORD=... make up
```

`make up` brings up the edge and then every site, and finishes by printing
`doctor`. It is idempotent: it starts what is missing and leaves alone what is
already correct.

**Something changed.** Code, a template, a Dockerfile.

```sh
make deploy SITE=blog.bythewood.me
```

That is the only command that rebuilds. The three sites with a password need it
in the deploying shell, and `make` will tell you which:

```sh
ANALYTICS_PASSWORD=... make deploy SITE=analytics.bythewood.me
```

**Something is broken.** One site is down, or the whole thing is.

```sh
make doctor
```

Read-only. It prints the tunnel credentials, the `orchard-edge` network, both
edge containers, every site, and the three SQLite volumes with their sizes,
putting the command that fixes it next to anything wrong. A container that is
stopped or unhealthy usually wants `make up`; one that is serving the wrong
thing wants `make deploy`.

The volumes are worth having in there because they hold the only state in this
repo that cannot be rebuilt from source, and a report that skipped them would
call a system healthy while its data was gone. Their names are read out of the
compose files, so a site that gains state is picked up without editing the
Makefile. Note that this checks that a volume exists, not that its contents are
writable: a `/data` volume created root-owned stays root-owned and still needs
its one-time `chown -R 65532:65532`.

## Commands

```sh
make up                              everything, from nothing or from broken
make deploy SITE=blog.bythewood.me   rebuild one site and replace it
make doctor                          tunnel, network, containers, and the data volumes
make down                            stop everything

make run SITE=blog.bythewood.me      vite watch + go run, on :8000
make build SITE=blog.bythewood.me    frontend, then a release binary in bin/
make check                           gofmt, then vet and build every site
make test                            every site's tests
```

The development targets touch no Docker at all. Everything else does, and
goes through `sudo`, because the socket in the webdev container is `root:root`
mode 660. On a host where docker needs no sudo: `make up SUDO=`.

Every site has `run`, `build` and `clean` in its own `Makefile`, so you can work
inside one without going through the root. There is no default `SITE`; a bare
`make deploy` asks which one rather than picking the portfolio for you.

## Layout

Each site is its own Go module and builds on its own. There is no module at the
repo root; `go.work` exists so repo-wide `make` targets and an editor can see
all five at once, and nothing depends on it:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

Each site also carries its own copy of `web/`, the small HTTP layer they all
need: the Vite manifest reader, request logging, panic recovery, security
headers, cache policy, graceful shutdown, and the shipper that tees every log
record to `logging.bythewood.me`. This used to be one shared package under a
single root module. Splitting it means a site is a directory you can lift into
its own repository, and its Docker build context is that directory instead of
the whole monorepo. The cost is that a fix in `web/` has to be made five times.

## Logging

Every site installs a `slog.Handler` that tees: stdout is written exactly as
before, and a copy is queued for `logging.bythewood.me`, which a background
goroutine flushes to `http://orchard-logging:8000/ingest` by container name on
the internal bridge. Nothing on that path can block a request, and every failure
drops rather than waits, because stdout stays the source of truth and the worst
outcome is a gap on a dashboard.

There is no token, because there is no route: the Caddy block for the public
hostname answers `/ingest` with a 404, so the only way to reach the endpoint is
from inside the network. Records land in SQLite in batched transactions, with an
hourly rollup written in the same transaction. Raw lines are swept after thirty
days; the rollups are kept forever, which is what lets a year-long chart read a
few hundred rows.

Caddy and cloudflared are the exception and ship nothing: neither can carry a Go
handler. Docker's json-file driver still has all of it.

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
sh edge/setup-tunnel.sh login     # once per machine
sh edge/setup-tunnel.sh up        # create the tunnel and its DNS records
make up                           # brings up the edge along with the sites
```

The script drives docker through `sudo` for the same reason the Makefile does,
and takes the same `SUDO=` override.

One wrinkle worth knowing before the first `make up` after a compose upgrade:
compose changed how it hashes a service's config between major versions, so a
container created by an older compose looks changed to a newer one and is
recreated once even though nothing about it moved. Check with

```sh
sudo docker inspect --format '{{index .Config.Labels "com.docker.compose.version"}}' orchard-caddy
```

against `docker compose version`. If they differ, the first `up` bounces those
containers for a second or two and every one after that is a no-op. This is why
`up` is worth running deliberately rather than reflexively while traffic
matters.

Adding a hostname takes three changes: a site block in `edge/caddy/Caddyfile`,
an ingress rule in `edge/cloudflared/config.yml`, and a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`. Both configs are baked into their images, so a
change needs a rebuild.

Caddy sits between cloudflared and the apps so compression, security headers and
per-host routing are configured in one place. It reaches each app by container
name, and every container here is `orchard-<first label of the site>`, matching
`orchard-caddy` and `orchard-cloudflared`. Because the Caddyfile is baked into
the image, renaming a site's container means rebuilding the edge as well.

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

All five run as UID 65532, with base images pinned by digest. The three Alpine
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

`make` checks for these before it does anything and names the one it wants, so
a forgotten password costs a message rather than a failed build. Note that
`sudo` runs with `env_reset`, which strips them: the Makefile forwards each one
explicitly as a sudo-level assignment, which is why deploying by hand with a
bare `sudo docker compose up` does not work even though the variable is set.

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

## License

BSD 2-Clause. See `LICENSE.md`.
