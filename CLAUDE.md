# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and for anyone else working in this
repository. Start with `README.md`, this file is the working detail behind it.

## What this is

One repo for every site Isaac Bythewood runs, plus the shared Cloudflare Tunnel
and Caddy that front them. Six sites, all Go, all served from a desktop behind a
tunnel rather than a rented server.

| Directory | What it serves |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Animation heavy, framework free, no third party Go dependency |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card per post, and an Atom feed |
| `sites/analytics.bythewood.me/` | Self hosted analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Self hosted uptime monitoring. SQLite, Lighthouse audits, crawler |
| `sites/logging.bythewood.me/` | Self hosted log aggregation. Every other site ships its slog records here. SQLite, retention and rollups, Typst PDF reports |
| `sites/repos.bythewood.me/` | Self hosted git remote. Push to it over HTTPS with a token, and it mirrors the GitHub account as a backup. Everything git is a subprocess |
| `sites/dash.bythewood.me/` | Dashboard. Markets off Yahoo, Hacker News and Lobsters, the weather, and whether the other six are answering. One poller, server sent events out, no database |
| `edge/` | The shared `cloudflared` tunnel, the Caddy that reverse proxies to each site, and the ntfy every alert is published to |

## The one structural rule

**Every site is its own Go module and owns its own copy of `web/`.**

There is no module at the repo root. `go.work` exists so repo wide `make`
targets and an editor can see all seven at once, and nothing depends on it. Each
site builds standalone:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

and its Docker build context is that directory alone, so a site is a folder you
can copy into its own repository.

`web/` is the small HTTP layer every site needs: the Vite manifest reader,
request logging, panic recovery, security headers, the static and edge cache
policies, graceful shutdown, and `shipper.go`, the tee handler that copies every
log record to logging.bythewood.me.

**A fix in `web/` has to be made seven times.** Do not add a shared parent module
to avoid it, and keep `shipper.go` byte identical across the seven, since it is a
wire format as much as a file.

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

There is no default `SITE`, so a bare `make deploy` cannot replace the wrong
one. `up` does not pass `--build`, so it starts what is missing and reuses
existing images, which makes it both safe on a healthy system and the repair
command on a broken one, and `deploy` is the only thing that rebuilds. Each site
has `run`, `build` and `clean` in its own `Makefile`, but no `serve` target,
since `go run .` without Vite leaves no manifest and that is fatal.

## Conventions that will bite you

**Port 8000, everywhere.** Every container listens on 8000 internally, in dev
and in prod, and no host ports are published. `cloudflared` terminates the
tunnel and hands plain HTTP to Caddy, which reverse proxies to each app by
container name on the `orchard-edge` network. The one other port in the repo is
`orchard-logging:9001`, a plain TCP socket Caddy writes its access log to,
reachable on the bridge and nowhere else.

**Everything is named `orchard-<first label>`.** `orchard-caddy` and
`orchard-cloudflared` for the edge, then `orchard-blog`, `orchard-analytics`,
`orchard-status`, `orchard-isaacbythewood`, `orchard-logging`, `orchard-repos`
and `orchard-dash`, plus an `orchard-<label>-data` volume for each of the four
sites with SQLite. One prefix, so `docker ps --filter name=orchard` is the whole
system and the Makefile derives a container name from a site directory without a
lookup table.

**Five things reference a container by name, and all five bake it in.**
`edge/caddy/Caddyfile` reverse-proxies to each site and writes its access log to
`tcp/orchard-logging:9001`, `sites/isaacbythewood.com/site.go` fetches
`http://orchard-blog:8000/latest.json` for the latest-posts panel,
`web/shipper.go` in every site posts to `http://orchard-logging:8000/ingest`,
`alerts.go` in status and logging posts to `http://orchard-ntfy:8000`, and
`sites/dash.bythewood.me/systems.go` reads `http://orchard-logging:8000/aggregate`
and probes every other site at `http://orchard-<label>:8000/healthz`. None reads
the name at runtime, so renaming one means rebuilding Caddy, the portfolio and
every site, not just editing a compose file. No SQLite database refers to a
container name.

**Alerts leave through ntfy in the edge, and reading them is authenticated.**
status publishes to the `status` topic and logging to `logging`, both to
`http://orchard-ntfy:8000` on the bridge with a write-only token from each
site's `.env`, and reading is over the tunnel at `ntfy.bythewood.me` with a
read-only account. Two fences hold, ntfy runs `auth-default-access: deny-all`
and decides who may publish, and Caddy refuses every publish route on the public
hostname, so the write token on its own gets an outsider nowhere. ntfy has no
source-based ACL, which is why that second fence has to live at the edge.
`edge/setup-ntfy.sh` creates the accounts and mints the token.

**The Caddy publish fence has to stay a denylist.** Blocking `POST` is not
enough, because ntfy publishes over GET too: `/<topic>/publish`, `/send` and
`/trigger` all publish with no body, and `POST /` publishes with the topic in a
JSON body, all confirmed against the running container. A rule blocking
`POST /<topic>` leaks three ways.

**Cloudflare bounds what you can push to repos and nothing here can raise it.**
It rejects any proxied request body over 100MB on Free and Pro, and a tunnel
hostname has to stay proxied because `<uuid>.cfargotunnel.com` is not publicly
routable and a DNS-only record would resolve to nothing. `http.postBuffer` does
not help either, git's own documentation says raising it only disables chunked
encoding for servers that cannot handle it. Everyday pushes send new objects
only and are kilobytes, so this bites once per repository, on the first push.
Two ways past it, in order of least effort:

1. **Seed over the Docker bridge.** From a container on `orchard-edge`, push to
   `http://orchard-repos:8000/<name>.git`. Cloudflare is not in the path.
2. **Push in slices,** which works from anywhere:
   `git log --oneline --reverse main | awk 'NR % 500 == 0' | cut -d' ' -f1 | while read sha; do git push origin +$sha:refs/heads/main; done`
   then a final `git push origin main`.

The repository page meters each repo against the limit and names which route to
use once one is over it. `orchard` itself packs to about 95MB, so it is the one
to watch.

**Secrets are a `.env` beside each site's compose file.** Compose reads it
because that directory is the project directory, so nothing is exported in a
shell and nothing is forwarded through the Makefile. `.env` is gitignored by
bare name and this repo is public, so check it with `git check-ignore -v`
instead of assuming. Every site needing one commits a `.env.example`.

**Editing anything in `edge/` needs `make edge`.** The Caddyfile and ntfy's
`server.yml` are baked into images and `make up` does not pass `--build`, so an
edited config that was never rebuilt is a silent no-op. cloudflared is worse
again, its config lives in a volume so compose sees no change and will not
restart it, and the tunnel serves the old ingress while a newly added hostname
404s from the Cloudflare edge. `make edge` restarts it explicitly, but a restart alone
still serves the old ingress, because the config it reads is the copy in the
volume and nothing has replaced it.

Adding a hostname is five changes: a Caddy site block, a `cloudflared` ingress
rule, the name in `HOSTNAMES` in `edge/setup-tunnel.sh`, a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`, and then `sh setup-tunnel.sh up` to reseed the
volume before `make edge`. Skip the reseed and the container is healthy, Caddy
is right, and the hostname still 404s from the Cloudflare edge. The DNS route
calls in that script fail with an authentication error unless `cert.pem` covers
that zone, which does not matter when the CNAME already exists.

**Containers run as UID 65532, and base images are pinned by digest.** The four
Alpine sites create a real user at that UID and the two scratch ones use the
bare number, since there is no `/etc/passwd` to name one in. A `/data` volume
created root-owned stays root-owned, so a new one has to be chown'd once.

**The binary is its own health check.** `-healthcheck` does a loopback GET
against `/healthz` and exits 0 or 1. A `FROM scratch` image has no shell for
`HEALTHCHECK` to call, so compose and the Dockerfiles both use the flag.

**Logging is `log/slog`, JSON to stdout, UTC.** `web.SetupLogging()` runs in
every main before anything else logs. UTC is forced through `ReplaceAttr`
because it is not slog's default, and local time in a container silently differs
from the host's.

**Log records are teed, never replaced.** `web.ShipLogs(source, web.HTTPSink())`
runs straight after `SetupLogging`, past the healthcheck branch so a
`HEALTHCHECK` invocation does not start a queue it will never flush. It wraps
whatever handler is installed and copies each record onto a bounded channel a
goroutine flushes to `orchard-logging`. Nothing on that path blocks a caller, so
a full queue, a failed POST and a 429 all drop the record, and stdout stays the
source of truth, which means the worst a broken logging site can do is lose
lines from a dashboard. The shipper writes its own state changes to stderr and
never calls `slog`, which would enqueue a record about failing to ship, and
`logging.bythewood.me` passes a local sink, because posting to itself would be
an ingest request that logs a request that becomes an ingest request.

**Caddy ships, cloudflared and ntfy do not.** Caddy can't carry a Go handler, so
it writes its access log to a socket with its own `net` writer and
`logging.bythewood.me` listens on 9001 for it, turning each line into the record
`web.Logged` would have written for the same request. `soft_start` is required
there, or Caddy refuses to boot whenever the logging site is down, which `make
up` guarantees since the edge starts first. A second `log console` block keeps
the same events on stderr so `docker logs orchard-caddy` is unchanged, and only
the access log ships, never Caddy's runtime log, since that's where a failing
`net` writer reports itself and shipping it over the failing connection would
loop. cloudflared and ntfy log to stdout and can't write to a socket, and
pointing either at a file takes its stdout away, so neither ships.

**A site block with no host matcher gets one logger, not a list.** The `:80`
catch-all in the Caddyfile becomes the server's `default_logger_name`, which is
a single string, so naming two loggers there silently keeps one and drops the
other. It carries the console logger alone for that reason.

**`Shipper.Close()` has to stay bounded.** Draining a 4096 deep
queue with a synchronous sink call every 500 records runs far past Docker's ten
second stop grace when the far end is wedged, and one hung container would then
get every other site SIGKILLed on the next `make deploy`, skipping their
`db.Close()`. Close waits on a timer instead, and every compose file sets
`stop_grace_period: 30s`.

**Deploys need `sudo`, and `sudo` then eats the password.** The Docker socket is
`root:root` mode 660 and the `docker` group does not help. But sudoers here sets
`env_reset`, so `ANALYTICS_PASSWORD=... sudo docker compose up` starts compose
with the variable stripped and the `${VAR:?}` guard aborts, complaining about
the shell you just set it in. The Makefile forwards each one as a sudo-level
assignment (`sudo VAR="$VAR" docker compose ...`), which survives. Every docker
command here, `edge/setup-tunnel.sh` included, goes through a `SUDO` variable so
a host that needs no sudo can turn it off.

**Frontends are bun and Vite 8.** Output goes to `sites/<name>/build/dist/` with
content hashed filenames, and the Go binary reads
`build/dist/.vite/manifest.json` to resolve them. A missing manifest is fatal,
since serving a page whose script tag points at a file that was never built is
worse than refusing to start. Vite 8 bundles with Rolldown, which imports
`styleText` from `node:util`, and Node 18 does not export it, so a build under
an old Node dies with a `SyntaxError`. It needs bun 1.4 or a modern Node, and
the pinned `oven/bun:1-alpine` in every frontend stage is already 1.4.

**`build/` holds every generated file, and only generated files.** Vite output,
the blog's PDFs and cards, analytics' topojson. It is gitignored and `make
clean` deletes it. A dev build reads it off disk, and `make build` passes
`-tags embed`, swapping `assets_disk.go` for `assets_embed.go` so `//go:embed`
compiles the directory into the binary. blog and isaacbythewood.com come out as
a single self-contained file, while analytics, status and logging still need
typst on disk, status also bun and chromium, and repos git, because those are
programs and not assets. The tag exists so a fresh clone still builds, since
`//go:embed` fails at compile time on a missing directory, and it lives under
`build/` because a directive cannot reference a path above its own package.

**Typst runs at build time, not on the request path.** Post PDFs, the resume and
every social card are compiled during `docker build` and served as files, which
is how the blog and the portfolio end at `FROM scratch`. Analytics, status and
logging keep Typst in the runtime image because their reports come from live
data over an arbitrary date range, with no finite set to precompile.

**Fonts for Typst must be TrueType.** Geist comes from `bun add geist`, Vercel's
package and not `@fontsource/geist`, which ships woff2 only, and Typst reaches
it through `--font-path`. A missing face does not error, it falls back to a
serif, which is how `blog_post.typ` asked for Inter and rendered in DejaVu.

## dash.bythewood.me

The seventh site, built 2026-08-30. Markets, Hacker News, Lobsters, the weather
and a health strip for the other six, all on one page that updates itself. It is
public and has no login, which is the constraint everything below follows from.

**Yahoo's `v7/finance/quote` is gone.** It answers 401 Unauthorized to anything
that has not carried a cookie and a crumb through their handshake, and it is the
endpoint every tutorial still points at. `v7/finance/spark` and
`v8/finance/chart` both still answer with no session at all, and spark takes the
whole symbol list in one request and returns the same meta block plus the
intraday closes, so the entire markets panel costs one call per poll. It needs a
browser-like User-Agent or the response is a block page, and Yahoo's edge sends
`cache-control: max-age=10`, so polling faster than that returns bytes you
already have.

**Hacker News comes from Algolia, not from the official API.** The Firebase API
hands back 500 bare story ids and charges one request per story to resolve each
one. `hn.algolia.com/api/v1/search?tags=front_page` returns all thirty with
titles, scores and comment counts in a single response.

**Futures replace the cash indexes outside the session**, driven by a New York
clock in `market.go` rather than by a holiday calendar. There is no calendar on
purpose, and the case it would catch is caught instead by the age of the S&P's
own quote: a cash index that has not printed in half an hour during what the
clock calls regular hours means the clock is wrong.

**The health strip asks the bridge first and the public hostname only as a
fallback.** Cloudflare will serve a cached 200 for `/healthz` long after the
origin behind it has stopped answering, and two of these sites do exactly that
right now, so a public probe is not evidence a site is up. A row built from the
fallback says `cached` rather than `up`.

**What logging hands over is counts and nothing else.** `/aggregate` returns a
record, error and 5xx count per source plus the watchdog's up flag, never a
message, a path, a status code or an address, because dash publishes it to the
internet. Caddy refuses the path on the public hostname the same way it refuses
`/ingest`, and a test on each side asserts the field list rather than trusting
the handler to stay honest.

**Everything is fetched once and pushed to every browser.** One poller per
source writes into a store and the store broadcasts the whole state as JSON over
`/events`, so ten open tabs still cost one request upstream. The markets poll
drops from 30 seconds to 5 minutes when nobody is connected, which keeps the
page warm for the first visitor without polling Yahoo all night for no one.

**`web/server.go` here sets `WriteTimeout: 0`,** the third version of that file
in this repo. Go's write bound covers the whole response, so any value at all is
a ceiling on how long a stream may stay open. An idle stream is held up by a
comment frame every 25 seconds instead, which is inside Cloudflare's 100 second
idle drop.

**Caddy's `encode` takes a matcher in this site's block** rather than the
blanket one in `(site)`, because a compressed `text/event-stream` buffers.

**Yahoo will return two closes for a symbol that had 287 a minute earlier.**
Seen on BTC-USD. `carrySparks` keeps the previous shape when a poll comes back
with fewer than five points and the last one had more, within one symbol and one
trading day so a card never shows yesterday's chart or the other instrument's.
Only the shape is held back, the price and the percent are always fresh.

## Rules learned the hard way

**An hourly rollup cannot answer an unaligned window.** `logging`'s
`rollups.hour` is an hour-floored timestamp, so `hour >= start` against a
`now`-relative start drops the bucket that contains the start, whole, and every
tile then lost up to an hour of data while the raw-backed panels beside them did
not. Floor the start of the window and the rollup sum equals the raw count. A
test using an hour-aligned base with a `base-1 .. base+1` window cannot catch
this, so give the test an unaligned base.

**Percentiles in SQLite want `CUME_DIST`, not `PERCENT_RANK`.** `PERCENT_RANK`
assigns exactly 1.0 to the largest row of every partition, so `pr <= 0.95` never
selects the slowest sample and a path with few samples reports a value well
below the percentile its column claims. `CUME_DIST` reaches 1.0 at the maximum,
so `MIN(CASE WHEN cd >= 0.95 ...)` is the nearest-rank percentile it says it is,
in one query instead of one per percentile.

**A container health check is not traffic.** Every one is the binary probing
itself over loopback with no `CF-Ray`, and in `logging` they outnumbered real
requests and were counted by the latency percentiles and the busiest-paths
ranking, dragging both away from anything a visitor sees. They are demoted to a
rollup counter rather than dropped, so the proof that each site answered its
probe survives without a raw row.

**Never put `$(MAKE)` on a recipe line that also does something.** GNU make runs
any recipe line containing that string even under `-n`, so a one-line shell
conditional ending in `$(MAKE) doctor` is executed in full by a dry run, side
effects and all, which is enough to replace a running container. Keep `$(MAKE)`
on a line of its own.

**Do not let a poller that probes this process start before the listener.**
dash's health strip asks itself over loopback, and starting the pollers before
`web.Serve` has bound the socket makes the dashboard report the site it is
running on as down or unknown until the next round, which is every deploy. Split
the synchronous first fetch from the loops and gate the loops on a health check.

**A middleware that wraps the ResponseWriter has to implement `Unwrap`.** Both
wrappers in `web/middleware.go` do now. Without it `http.ResponseController`
cannot reach the real writer, so a server-sent events handler behind `Logged`
cannot flush and dash's `/events` answered 500 for every request. Assert on
`http.Flusher` and you get the wrapper, not the connection.

**Never animate a layout property.** Animating `height`, `width`, `top` or
`left` relays out the page every frame, and the browser scores each frame as a
layout shift even when the animation is intentional and covers the whole
viewport, which is enough to fail CLS with no image involved. Use `transform`
(`scaleY`, `translateX`) with `transform-origin`, which is composited, looks
identical and scores zero. Verify with `document.getAnimations()` and a pixel
diff instead of by eye.

**`og:image` must be a raster format.** Facebook, X, LinkedIn, Slack, iMessage
and Discord all refuse `image/svg+xml`.

**Do not put `s-maxage` in a cache policy here.** Per RFC 9111 it carries
`proxy-revalidate` semantics, so Cloudflare treats it as never serve stale
without asking first and it disables both `stale-while-revalidate` and
`stale-if-error`. The origin is a desktop at the end of a tunnel, so
`stale-if-error` is what keeps the edge serving the last good copy instead of a
530 when the house goes dark. Use plain `max-age` with both stale directives,
and keep Cloudflare's Always Online off, since it makes `stale-if-error`
ignored.

**Error responses get `no-store` explicitly.** Once a Cache Rule marks a zone
eligible, Cloudflare stamps its own browser TTL on a header-less response and
will hold a 404 at the edge, so publishing a post at a URL somebody already
missed would serve them the 404 for hours.

**Hardcode identity, never hardcode credentials.** Base URLs and site names are
constants in `site.go`, and the only environment variables that exist are real
secrets and paths set by the Dockerfiles. A `BASE_URL` that defaults to empty is
worse than no variable at all, since it ships a site whose own tracking silently
does nothing.

## Tests

`make test` runs every site's suite plus its `web/` copy, fourteen packages. There
are no linter configs, and `make check` is gofmt, vet and build. The portfolio
has no Go tests of its own, being templates and handlers over static data, and
is covered by its `web/` package plus browser checks.

`web/shipper_test.go` is one of the seven identical copies and covers the parts
easy to get quietly wrong: that a record reaches both the original handler and
the queue, that `WithAttrs` and `WithGroup` still tee, that a full queue drops
instead of blocking, and that logging after `Close` does not panic.
`logging.bythewood.me` also renders every page against a real seeded database,
because a template referencing a field that does not exist fails at execute time
rather than at parse time.

## Writing

Comments explain why, and only when the why is not visible from the code around
them. Three lines is the ceiling and most should be one or two. Delete anything
that restates the line, narrates what the code used to be, or quotes a number
from a single run. Nothing written here points at anything outside this
repository, since a reader only ever has the repo. Commit subjects are short,
capitalised, imperative and carry no trailing period, like `Add atom feed` or
`Fix rollup window off by an hour`, and they carry no body. Prose has no em
dashes and no semicolons, use a comma, a full stop, or and.
