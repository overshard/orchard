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
| `edge/` | The shared `cloudflared` tunnel, the Caddy behind it, and the ntfy that carries alerts |

## Three situations

**Nothing running.** A fresh clone on a machine where none of this exists, or a
machine where it all went down at once. Seven steps, in this order, and nothing
outside them:

```sh
# 1. The tunnel. Once per machine; opens a browser to pick a zone.
sh edge/setup-tunnel.sh login
sh edge/setup-tunnel.sh up

# 2. Secrets. Copy each example and fill in a password of your choosing.
#    Leave NTFY_TOKEN empty; step 5 writes it.
cp sites/analytics.bythewood.me/.env.example sites/analytics.bythewood.me/.env
cp sites/status.bythewood.me/.env.example    sites/status.bythewood.me/.env
cp sites/logging.bythewood.me/.env.example   sites/logging.bythewood.me/.env

# 3. Everything up. Idempotent, and the repair command as much as the first-run
#    one. Ends by printing doctor.
make up

# 4. The two ntfy accounts. Prompts for a password for each.
sh edge/setup-ntfy.sh up

# 5. The publishers' token, written into both sites' .env for you.
sh edge/setup-ntfy.sh token

# 6. Hand the token to the running sites. Compose recreates a container when its
#    .env changes, so this is all it takes.
make up

# 7. Confirm. Every line is either fine or carries the command that fixes it.
make doctor
```

Then point the ntfy Android client at `https://ntfy.bythewood.me` with the
reading account from step 4 and subscribe to `status` and `logging`.

The ordering that matters is 3 before 4: `setup-ntfy.sh` talks to a running
container, so ntfy has to exist before it can be given accounts. Everything else
is safe to re-run at any time.

A different domain means editing three things before step 1: the `HOSTNAMES` line
in `edge/setup-tunnel.sh`, the ingress rules in `edge/cloudflared/config.yml`,
and the site blocks in `edge/caddy/Caddyfile`. Each site's own hostname is a
constant in its `site.go`.

`make up` brings up the edge and then every site. It is idempotent: it starts
what is missing, recreates what changed, and leaves alone what is already
correct.

**Something changed.** Code, a template, a Dockerfile.

```sh
make deploy SITE=blog.bythewood.me
```

That is the only command that rebuilds. Nothing needs to be exported first: the
three sites with secrets read them from a `.env` beside their compose file, and
`make` names the file to create if it is missing.

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

Editing any config in `edge/` needs `make edge`, not `make up`: the Caddyfile and
`ntfy/server.yml` are baked into images, and `up` deliberately does not rebuild.
`cloudflared`'s config is the exception, seeded into a volume by
`sh edge/setup-tunnel.sh up` -- and because it comes from a volume rather than an
image, compose sees nothing changed and will not restart it, so `make edge`
restarts it explicitly. Without that the tunnel keeps serving the old ingress and
a newly added hostname 404s with nothing to say why.

## Alerts

`ntfy` runs in the edge and is how anything here reaches a phone. status
publishes outage and recovery transitions to the `status` topic; logging
publishes silence and unclean-restart transitions to `logging`. Both post to
`http://orchard-ntfy:8000` by container name on the bridge. Reading is over the
tunnel at `ntfy.bythewood.me`, like every other hostname here.

**Two independent fences, and either one alone would hold.**

*Who*, in ntfy: the server is `auth-default-access: deny-all`, so an anonymous
request can neither read nor publish on any topic. Two accounts exist and
neither can do the other's job. `isaac` is read-only on `status` and `logging`
and is what the phone uses. `orchard` is write-only on the same two and is what
the sites use, authenticating with a token rather than a password so it can be
revoked on its own.

*From where*, in Caddy: the public hostname allows exactly three routes and
404s everything else, so publishing works only from the bridge **even with the
write token in hand**. The three are `/<topic>/ws`, `/<topic>/json` and
`/<topic>/auth`, which is the complete set the Android client uses, read off
this proxy's own access log rather than guessed.

An allowlist rather than a denylist, for two reasons. ntfy publishes over GET as
well as POST, so a denylist has to enumerate `/<topic>/publish`, `/send` and
`/trigger`, and a `POST /` that carries the topic in a JSON body; miss one and
it leaks. And everything else ntfy serves is surface nothing here needs.

**The web app is off** (`web-root: disable`). ntfy serves a full single-page
client at `/` by default, which on a public hostname is a login form, a service
worker and a pile of static assets answering to anyone who finds the name. It
never exposed a message, since every topic route is deny-all, but it was a front
door nothing needed. Note that Cloudflare caches static assets: after turning it
off, purge the cache or those files answer from the edge for up to four hours
while the origin correctly 404s.

ntfy has no way to restrict an account by source address, which is why the
second fence is at the edge and not in its config.

Setting it up on a new machine, after the tunnel exists:

```sh
sh edge/setup-ntfy.sh up      # create both accounts and their topic access
sh edge/setup-ntfy.sh token   # mint the write token, then into each site's .env
sh edge/setup-ntfy.sh status  # what exists right now
```

Point the ntfy Android client at `https://ntfy.bythewood.me` with the reading
account and subscribe to `status` and `logging`.

The honest limit, accepted rather than solved: an alerter running on this
machine cannot tell you this machine lost power or internet. A tunnel that drops
is covered from the other side, since Cloudflare sends its own tunnel health
notification and does not depend on the tunnel to do it.

To check the path without waiting for something to break:

```sh
make doctor                      # whether ntfy answers
./build/status  -preview-alert down
./build/logging -preview-alert silence
```

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
theme) is a constant in each site's `site.go`, so there is no `BASE_URL`
indirection and no way for a deploy to point a site at the wrong name.

**Secrets live in a `.env` beside the compose file that needs them.** Compose
reads it on its own, because that directory is the project directory, so nothing
is exported in a shell and no secret appears on a command line:

```
sites/analytics.bythewood.me/.env    ANALYTICS_PASSWORD
sites/status.bythewood.me/.env       STATUS_PASSWORD, NTFY_TOKEN
sites/logging.bythewood.me/.env      LOGGING_PASSWORD, NTFY_TOKEN
```

Each of those commits a `.env.example` listing its keys with no values, and
`make deploy` refuses with the `cp` line to run when the example exists and the
real file does not. A site gaining its first secret needs one example file and
no Makefile change.

**`.env` is gitignored by bare name, so it is ignored at any depth. This
repository is public**, which makes that worth verifying rather than trusting:

```sh
git check-ignore -v sites/status.bythewood.me/.env
```

This replaced taking passwords from the deploying shell. That rule was correct
in principle, and in practice it collided with the fact that governs every
docker command here: `sudo` runs with `env_reset`, so an exported password was
stripped before compose ever saw it, and the `${VAR:?}` guard then aborted the
deploy complaining about the shell you had just set it in. It was the single
most common way a deploy failed. See
`code/memory/decisions/0016-env-files-for-secrets.md`.

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
