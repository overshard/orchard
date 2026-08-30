# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and for anyone else working in this
repository. Start with `README.md`; this file is the working detail behind it.

## What this is

One repo for every site Isaac Bythewood runs, plus the shared Cloudflare Tunnel
and Caddy that front them. Five sites, all Go, all served from a desktop behind
a tunnel rather than a rented server.

| Directory | What it serves |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Animation heavy, framework free, no third party Go dependency |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card per post, and an Atom feed |
| `sites/analytics.bythewood.me/` | Self hosted analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Self hosted uptime monitoring. SQLite, Lighthouse audits, crawler |
| `sites/logging.bythewood.me/` | Self hosted log aggregation. Every other site ships its slog records here. SQLite, retention and rollups, Typst PDF reports |
| `edge/` | The shared `cloudflared` tunnel, the Caddy that reverse proxies to each site, and the ntfy every alert is published to |

## The one structural rule

**Every site is its own Go module and owns its own copy of `web/`.**

There is no module at the repo root. `go.work` exists so repo wide `make`
targets and an editor can see all five at once; nothing depends on it. Each site
builds standalone:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

and its Docker build context is that directory alone, which is what makes a site
liftable into its own repository by copying the folder.

`web/` is the small HTTP layer every site needs: the Vite manifest reader,
request logging, panic recovery, security headers, the static and edge cache
policies, graceful shutdown, and `shipper.go`, the tee handler that sends a copy
of every log record to logging.bythewood.me. It was one shared `internal/web`
under a single root module before the split.

**A fix in `web/` has to be made five times.** If a change belongs in all five,
change all five. Do not reintroduce a shared parent module to avoid this; that
is the thing the split removed. `shipper.go` in particular must stay
byte-identical across the five: it is a wire format as much as a file.

## Commands

From the repo root:

```sh
make up                              edge, then every site; idempotent, and the repair command
make deploy SITE=blog.bythewood.me   rebuild one site and replace it
make doctor                          tunnel, network, containers, and the data volumes
make down                            stop everything

make run SITE=blog.bythewood.me      vite watch + go run
make build SITE=blog.bythewood.me
make check                           gofmt, then vet and build every site
make test                            every site's tests, including its own web/ copy
```

There is no default `SITE`. There used to be, and a bare `make deploy` then
meant rebuilding and replacing the portfolio, which is a lot to trigger by
forgetting an argument.

`up` and `deploy` are deliberately different. `up` does not pass `--build`, so
it starts what is missing and reuses images that exist; that is what makes it
safe to run against a healthy system and useful against a broken one. `deploy`
is the only thing that rebuilds.

Each site has `run`, `build` and `clean` in its own `Makefile` and can be driven
from its own directory. None of them has a `serve` target any more: `go run .`
with no Vite means no manifest, and the server treats a missing manifest as
fatal, so it only ever looked like a faster `run`.

## Conventions that will bite you

**Port 8000, everywhere.** Every container listens on 8000 internally, in dev
and in prod. No host ports are published. `cloudflared` terminates the tunnel
and hands plain HTTP to Caddy, which reverse proxies to each app by container
name on the `orchard-edge` network.

**Everything is named `orchard-<first label>`.** `orchard-caddy`,
`orchard-cloudflared`, `orchard-blog`, `orchard-analytics`, `orchard-status`,
`orchard-isaacbythewood`, `orchard-logging`, and the volumes
`orchard-analytics-data`, `orchard-status-data` and `orchard-logging-data`. One
prefix across the repo, so `docker ps --filter name=orchard` is the whole system and the Makefile derives a container name from
a site directory without a lookup table. These carried a `-next` suffix until
2026-08-29, from the migration that made them the live thing; the suffix stopped
being true the day the cutover finished.

**Four things reference a container by name, and all four bake it in.**
`edge/caddy/Caddyfile` reverse-proxies to each site,
`sites/isaacbythewood.com/site.go` fetches `http://orchard-blog:8000/latest.json`
for the portfolio's latest-posts panel, `web/shipper.go` in every site posts
to `http://orchard-logging:8000/ingest`, and `alerts.go` in both status and
logging posts to `http://orchard-ntfy:8000`. None of them reads the name at
runtime, so renaming a container means rebuilding Caddy, the portfolio *and*
every site, not just editing a compose file. Nothing in any SQLite database
refers to a container name.

**Alerts leave through ntfy in the edge, and reading them is authenticated.**
status publishes to the `status` topic and logging to `logging`, both to
`http://orchard-ntfy:8000` on the bridge with a write-only token from each
site's `.env`. Reading is over the tunnel at `ntfy.bythewood.me` with a
read-only account. Two independent fences: ntfy is `auth-default-access:
deny-all` and decides *who*, and Caddy refuses every publish route on the public
hostname and decides *from where*, so publishing is impossible from outside even
holding the write token. **ntfy has no source-based ACL**, which is why the
second fence is at the edge and not in its config. `edge/setup-ntfy.sh` creates
the accounts and mints the token.

**The Caddy publish fence must stay a denylist, not a "block POST".** ntfy
publishes over GET too: `/<topic>/publish`, `/send` and `/trigger` all publish
with no body, and `POST /` publishes with the topic in a JSON body. All four
were confirmed against the running container. A rule blocking `POST /<topic>`
leaks three ways.

**Secrets are a `.env` beside each site's compose file.** Compose reads it
because that directory is the project directory, so nothing is exported in a
shell and nothing is forwarded through the Makefile. This replaced taking
passwords from the deploying shell, which was correct in principle and cost a
failed deploy nearly every time: `sudo` runs with `env_reset`, so the exported
value was stripped before compose saw it, and the `${VAR:?}` guard then
complained about the shell you had just set it in. **`.env` is gitignored by
bare name and this repo is public**: verify with `git check-ignore -v` rather
than trusting it. Every site needing one commits a `.env.example`.

**Editing anything in `edge/` needs `make edge`.** The Caddyfile and ntfy's
`server.yml` are baked into images and `make up` does not pass `--build`, so an
edited config that was never rebuilt is a silent no-op. **cloudflared is the
exception and is worse**: its config lives in a volume, so compose sees no
change and will not restart it, and the tunnel goes on serving the old ingress
while a newly added hostname 404s from the Cloudflare edge. `make edge` restarts
it explicitly for that reason.

**Adding a hostname is three changes, not one.** A Caddy site block, a
`cloudflared` ingress rule, and a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`. The edge config is baked into the images, so
both edge components need a rebuild to pick up a change.

**Containers run as UID 65532, and base images are pinned by digest.** The three
Alpine sites create a real user at that UID; the two scratch ones use the bare
number, because there is no `/etc/passwd` to name one in. A `/data` volume
created root-owned stays root-owned, so a new one has to be chown'd once.

**The binary is its own health check.** `-healthcheck` does a loopback GET
against `/healthz` and exits 0 or 1. A `FROM scratch` image has no shell for
`HEALTHCHECK` to call; compose and the Dockerfiles both use the flag.

**Logging is `log/slog`, JSON to stdout, UTC.** `web.SetupLogging()` runs in
every main before anything else logs. UTC is not slog's default and is forced
through `ReplaceAttr`, because local time in a container silently differs from
the host's.

**And it is teed, never replaced.** `web.ShipLogs(source, web.HTTPSink())` runs
straight after `SetupLogging`, past the healthcheck branch so a `HEALTHCHECK`
invocation does not start a queue it will never flush. It wraps whatever handler
is installed and copies each record onto a bounded channel that a goroutine
flushes to `orchard-logging`. Nothing on that path blocks a caller: a full queue
drops, a failed POST drops, a 429 drops. stdout stays the source of truth, so
the worst a broken logging site can do is lose lines from a dashboard. The
shipper never calls `slog` itself, because a shipper that logged its own
failures would enqueue a record about failing to ship; state changes go straight
to stderr. `logging.bythewood.me` is the one exception and passes a local sink
instead, since posting to itself would be an ingest request that logs a request
that becomes an ingest request.

**`Shipper.Close()` is bounded, and the bound is load bearing.** Unbounded, it
drained the whole 4096-deep queue calling the sink synchronously every 500
records: nine flushes at the ten second timeout, measured at 100 seconds against
a wedged logging site. Docker's default stop grace is ten, so one hung container
would have SIGKILLed every other site on the next `make deploy`, skipping their
`db.Close()`. Close now waits on a timer, and every compose file sets
`stop_grace_period: 30s`. The hot path was never the risk: 20,000 log calls
against a dead sink cost 28ms in total.

**Deploys need `sudo`, and `sudo` then eats the password.** The Docker socket is
`root:root` mode 660; being in the `docker` group does not help. But sudoers
here sets `env_reset`, so `ANALYTICS_PASSWORD=... sudo docker compose up` starts
compose with the variable stripped and the `${VAR:?}` guard aborts, complaining
about the shell you just set it in. The Makefile forwards each one as a
sudo-level assignment (`sudo VAR="$VAR" docker compose ...`), which is the form
that survives. Every docker command in this repo, `edge/setup-tunnel.sh`
included, goes through a `SUDO` variable so a host that needs no sudo can turn
it off.

**Frontends are bun and Vite 8.** Output goes to `sites/<name>/build/dist/` with
content hashed filenames, and the Go binary reads
`build/dist/.vite/manifest.json` to resolve them. A missing manifest is fatal:
serving a page whose script tag points at a file that was never built is worse
than refusing to start.

Vite 8 bundles with Rolldown, which imports `styleText` from `node:util`. Node
18 does not export it, so a build under an old Node dies with a `SyntaxError`
before compiling anything. It needs bun 1.4 or a modern Node; the pinned
`oven/bun:1-alpine` in every frontend stage is already 1.4.

**`build/` holds every generated file, and only generated files.** Vite output,
the blog's PDFs and cards, analytics' topojson. It is gitignored and `make
clean` deletes it. A dev build reads it off disk; `make build` passes
**`-tags embed`**, swapping `assets_disk.go` for `assets_embed.go` so
`//go:embed` compiles the directory into the binary. `blog` and
`isaacbythewood.com` come out as a single self-contained file; `analytics` and
`status` still need typst and bun/chromium on disk, because those are programs,
not assets. The tag exists so a fresh clone still builds, since `//go:embed`
fails at compile time on a directory that is not there. It lives under `build/`
rather than the repo root because a directive cannot reference a path above its
own package.

**Typst runs at build time, not on the request path.** Post PDFs, the resume and
every social card are compiled during `docker build` and served as files. The
blog and the portfolio end at `FROM scratch` because of it. Analytics, status and
logging keep Typst in the runtime image because their reports come from live data
over an arbitrary date range, with no finite set to precompile.

**Fonts for Typst must be TrueType.** Geist is installed with `bun add geist`
and reached through `--font-path`. Vercel's package rather than
`@fontsource/geist` for one reason: `@fontsource` ships woff2 only and Typst
cannot read it. A missing face does not error, it falls back to a serif, which
is how `blog_post.typ` asked for Inter for months and rendered in DejaVu.

## Rules learned the hard way

**An hourly rollup cannot answer an unaligned window.** `logging`'s `rollups.hour`
is an hour-floored timestamp, so `hour >= start` against a `now`-relative start
drops the bucket that *contains* the start, whole. Every tile lost up to an hour
of data while the raw-backed panels beside them did not, so the two disagreed on
screen: "Last hour" reported 31 records against a raw window holding 59. Floor
the start of the window, which makes the rollup sum exactly equal the raw count
rather than merely closer. **A test using an hour-aligned base with a
`base-1 .. base+1` window cannot catch this**, which is the shape every original
test used.

**Percentiles in SQLite want `CUME_DIST`, not `PERCENT_RANK`.** `PERCENT_RANK`
assigns exactly 1.0 to the largest row of every partition, so `pr <= 0.95` can
never select the slowest sample: a five-sample path reported its 4th value, the
80th percentile, under a column headed p95. `CUME_DIST` reaches 1.0 at the
maximum, so `MIN(CASE WHEN cd >= 0.95 ...)` is the nearest-rank percentile it
claims to be. It is also one query instead of one per percentile.

**A container health check is not traffic.** Every one is the binary probing
itself over loopback with no `CF-Ray`, roughly 480 an hour across this repo. In
`logging` they were 59% of request records and were being counted by the latency
percentiles and the busiest-paths ranking, which made p50 read 0.042ms against a
real 0.537ms. They are demoted to a rollup counter rather than dropped, so the
proof that each site answered its probe survives, forever, without a raw row.

**Never put `$(MAKE)` on a recipe line that also does something.** GNU make
runs any recipe line containing that string even under `-n`, so the sub-make
can print its own commands. A one-line shell conditional that ends in
`$(MAKE) doctor` is therefore executed in full by `make -n`, side effects and
all. This is not hypothetical: it is how a dry run of taproot's `update`
replaced a running container. Keep `$(MAKE)` on a line of its own.


**Never animate a layout property.** Animating `height`, `width`, `top` or
`left` relays out the page every frame, and the browser scores each of those
frames as a layout shift even when the animation is deliberate and covers the
whole viewport. The portfolio's opening curtain scored 0.36 CLS this way, three
and a half times Google's threshold, with no image involved. Use `transform`
(`scaleY`, `translateX`) with `transform-origin`; it is composited, looks
identical and scores zero. Verify with `document.getAnimations()` and a pixel
diff, not by eye.

**`og:image` must be a raster format.** Facebook, X, LinkedIn, Slack, iMessage
and Discord all refuse `image/svg+xml`.

**Do not put `s-maxage` in a cache policy here.** Per RFC 9111 it carries
`proxy-revalidate` semantics, so Cloudflare treats it as "never serve stale
without asking first" and it disables both `stale-while-revalidate` and
`stale-if-error`. Those two are the point: the origin is a desktop at the end of
a tunnel, and `stale-if-error` is what lets the edge keep serving the last good
copy instead of a 530 when the house goes dark. Use plain `max-age` with both
stale directives, and keep Cloudflare's Always Online off, since it makes
`stale-if-error` ignored.

**Error responses get `no-store` explicitly.** Once a Cache Rule marks a zone
eligible, Cloudflare stamps its own browser TTL on a header-less response and
will hold a 404 at the edge, so publishing a post at a URL somebody already
missed would serve them the 404 for hours.

**Hardcode identity, never hardcode credentials.** Base URLs and site names are
constants in `site.go`. The only environment variables that exist are genuine
secrets and paths set by the Dockerfiles. A `BASE_URL` that defaults to empty is
worse than no variable at all: it shipped a site whose own tracking silently did
nothing.

## Tests

`make test` runs every site's suite plus its `web/` copy, ten packages. There are
no linter configs; `make check` is gofmt, vet and build. The portfolio has no Go
tests of its own, being templates and handlers over static data, and is covered
by `web/` plus browser checks.

**`logging` alone drops `script-src 'unsafe-inline'`.** The allowance in the
other four is inherited from analytics, whose comment cites an inline
self-tracking snippet. `logging` has no inline executable script at all: its
`application/json` and `ld+json` blocks are data and load regardless. It is also
the one site that renders text written by other programs, so it is the one that
most needs a second line of defence behind its escaping.

`web/shipper_test.go` is one of the five identical copies and covers the part
that is easy to get quietly wrong: that a record reaches both the original
handler and the queue, that `WithAttrs` and `WithGroup` return something that
still tees, that a full queue drops instead of blocking, and that logging after
`Close` does not panic. `logging.bythewood.me` additionally renders every page
against a real seeded database, because a template referencing a field that does
not exist fails at execute time rather than at parse time.
