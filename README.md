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
| `edge/` | The shared cloudflared tunnel, the Caddy behind it, and ntfy for alerts |

## Requirements

Docker, Go and bun. A Cloudflare account with a zone you control, since the
whole thing is served through a tunnel. Everything else comes out of the
Dockerfiles.

## Getting it running

### From nothing

A fresh clone on a machine where none of this exists, or a machine where it all
went down at once. Seven steps, in this order:

```sh
# 1. The tunnel. Once per machine; opens a browser to pick a zone.
sh edge/setup-tunnel.sh login
sh edge/setup-tunnel.sh up

# 2. Secrets. Copy each example and fill in a password of your choosing.
#    Leave NTFY_TOKEN empty; step 5 writes it.
cp sites/analytics.bythewood.me/.env.example sites/analytics.bythewood.me/.env
cp sites/status.bythewood.me/.env.example    sites/status.bythewood.me/.env
cp sites/logging.bythewood.me/.env.example   sites/logging.bythewood.me/.env
cp sites/repos.bythewood.me/.env.example     sites/repos.bythewood.me/.env

# 3. Everything up. Ends by printing doctor.
make up

# 4. The two ntfy accounts. Prompts for a password for each.
sh edge/setup-ntfy.sh up

# 5. The publishers' token, written into both sites' .env for you.
sh edge/setup-ntfy.sh token

# 6. Hand the token to the running sites.
make up

# 7. Confirm.
make doctor
```

Then point the ntfy Android client at `https://ntfy.bythewood.me` with the
reading account from step 4 and subscribe to `status` and `logging`.

Step 3 has to come before step 4, since `setup-ntfy.sh` talks to a running
container. Everything else is safe to re-run at any time.

A different domain means editing three things before step 1: the `HOSTNAMES`
line in `edge/setup-tunnel.sh`, the ingress rules in
`edge/cloudflared/config.yml`, and the site blocks in `edge/caddy/Caddyfile`.
Each site's own hostname is a constant in its `site.go`.

### After you change something

```sh
make deploy SITE=blog.bythewood.me
```

That is the only command that rebuilds. Nothing needs exporting first, since
the four sites with secrets read them from a `.env` beside their compose file.

Editing anything in `edge/` needs `make edge` instead, because those configs are
baked into images and `make up` does not rebuild.

### When something is broken

```sh
make doctor
```

Read-only. It prints the tunnel credentials, the network, both edge containers,
every site, and the SQLite volumes with their sizes, and puts the command that
fixes it next to anything wrong. A container that is stopped or unhealthy
usually wants `make up`; one serving the wrong thing wants `make deploy`.

## Commands

```sh
make up                              everything, from nothing or from broken
make deploy SITE=blog.bythewood.me   rebuild one site and replace it
make edge                            rebuild the edge after editing edge/
make doctor                          tunnel, network, containers, data volumes
make down                            stop everything

make run SITE=blog.bythewood.me      vite watch + go run, on :8000
make build SITE=blog.bythewood.me    frontend, then a release binary in bin/
make check                           gofmt, then vet and build every site
make test                            every site's tests
```

The development targets touch no Docker. Everything else goes through `sudo`,
because the socket in the webdev container is `root:root` mode 660. On a host
where docker needs no sudo: `make up SUDO=`.

Every site has `run`, `build` and `clean` in its own `Makefile`, so you can work
inside one without going through the root. There is no default `SITE`.

## Layout

Each site is its own Go module and builds on its own. There is no module at the
repo root, and `go.work` only exists so repo-wide `make` targets and an editor
can see all seven at once:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

Each site also carries its own copy of `web/`, the small HTTP layer they all
need. That means a site is a directory you can lift into its own repository, and
its Docker build context is that directory instead of the whole monorepo. The
cost is that a fix in `web/` has to be made seven times.

Go serves every request and `html/template` renders the pages. Vite is a build
step and never a server: it writes content-hashed JS and CSS into `build/dist/`,
and the Go binary reads `build/dist/.vite/manifest.json` to turn those hashes
into script and link tags. Typst does the typesetting at build time for post
PDFs, the resume, and the social card each site serves as its `og:image`.

Everything generated lands in `sites/<name>/build/`, which is gitignored.
`make build` passes `-tags embed` so `//go:embed` compiles that directory into
the binary, which is how blog and the portfolio come out as a single file.

## The edge

One cloudflared and one Caddy serve the whole repo. Traffic is outbound only,
so there is no port forwarding, no dynamic DNS, and the origin IP is never
published. Every container listens on 8000 internally and nothing is published
to the host.

Adding a hostname takes three changes: a site block in `edge/caddy/Caddyfile`,
an ingress rule in `edge/cloudflared/config.yml`, and a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`.

Caddy also writes its access log to `logging.bythewood.me`, over a plain socket
on port 9001 rather than through the shipper every site uses, since Caddy can't
carry a Go handler. It keeps writing the same lines to stderr as well, so
`docker logs orchard-caddy` is unchanged. cloudflared and ntfy don't ship
anywhere, because neither can write its log to a network address and pointing
either at a file takes its stdout away.

ntfy runs in the edge and is how anything here reaches a phone. status publishes
outage transitions and logging publishes silence transitions, both over the
internal bridge. Reading is over the tunnel with a read-only account.

## Things that will bite you

Editing an `edge/` config and running `make up` does nothing. Those configs are
baked into images. Use `make edge`.

A `/data` volume created root-owned stays root-owned and needs a one-time
`chown -R 65532:65532`.

`.env` is gitignored by bare name and this repository is public, so it is worth
checking rather than trusting:

```sh
git check-ignore -v sites/status.bythewood.me/.env
```

Bind mounts of paths under `/home/dev` silently mount an empty directory,
because the daemon is Docker Desktop on the Windows host and resolves the source
against its own filesystem. Use named volumes.

An alerter running on this machine cannot tell you this machine lost power. A
tunnel that drops is covered from the other side, since Cloudflare sends its own
tunnel health notification.

`CLAUDE.md` has the rest: the conventions, and the mistakes that have already
been made once.

## License

BSD 2-Clause. See `LICENSE.md`.
