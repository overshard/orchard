# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and for anyone else working in this
repository. Start with `README.md`, this file is the working detail behind it.

**Anything visual is in `DESIGN.md`, and that file is the authority.** The
palette, the type, the grid, the home page skeleton and its word budget, and the
rules that are easy to get wrong all live there. Read it before touching a
template or a stylesheet, and update it in the same commit when a design
decision changes. There is no shared stylesheet here, so the sites looking
alike is something a person keeps true by following that file.

## What this is

One repo for every site Isaac Bythewood runs, plus the shared Cloudflare Tunnel
and Caddy that front them. Every site is Go, and all of them are served from a desktop behind a
tunnel rather than a rented server.

| Directory | What it serves |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Animation heavy, framework free, no third party Go dependency |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card per post, and an Atom feed |
| `sites/analytics.bythewood.me/` | Self hosted analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Self hosted uptime monitoring. SQLite, Lighthouse audits, crawler |
| `sites/logging.bythewood.me/` | Self hosted log aggregation. Every other site ships its slog records here. SQLite, retention and rollups, Typst PDF reports |
| `sites/repos.bythewood.me/` | Self hosted git remote. Push to it over HTTPS with a token, and it mirrors the GitHub account as a backup. Everything git is a subprocess |
| `sites/dash.bythewood.me/` | Dashboard. Markets off Yahoo, a markets line and a news summary the local model writes three times a day, Hacker News and Lobsters, the weather, and whether the other sites are answering. One poller, server sent events out, no database |
| `sites/auth.bythewood.me/` | The front door. One account, a six digit code pushed over ntfy, and an opaque session every other site checks against it |
| `sites/chat.bythewood.me/` | A conversation with a local model, with tools, attachments, pictures, history in SQLite and an incognito mode that writes nothing. It also answers a house out of public records, which used to be a dashboard of its own. Beside it an offline Wikipedia, served by kiwix off a 12.5GB ZIM |
| `sites/llm.bythewood.me/` | The model gateway. One card behind an API key, Ornith for chat and FLUX.2 klein for pictures taking turns on it, with every prompt and completion logged unless the caller marks the call incognito |
| `edge/` | The shared `cloudflared` tunnel, the Caddy that reverse proxies to each site, and the ntfy every alert is published to |

## The one structural rule

**Every site is its own Go module and owns its own copy of `web/`.**

There is no module at the repo root. `go.work` is for the editor and nothing
depends on it. The Makefile exports `GOWORK=off`, since under a workspace every
site resolves to the newest version any site uses and tests would run against
deps the image doesn't ship. Each site builds standalone:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

and its Docker build context is that directory alone, so a site is a folder you
can copy into its own repository.

`web/` is the small HTTP layer every site needs: the Vite manifest reader,
request logging, panic recovery, security headers, the static and edge cache
policies, graceful shutdown, `shipper.go`, the tee handler that copies every log
record to logging.bythewood.me, and `session.go`, which asks auth.bythewood.me
whether the cookie on a request is a live session.

**A fix in `web/` has to be made ten times.** Do not add a shared parent module
to avoid it, and keep `shipper.go` and `session.go` byte identical across the
ten, since both are wire formats as much as files.

## Commands

From the repo root:

```sh
make install                         once per machine: tunnel, secrets, containers, alerts
make up                              edge, then every site, idempotent and the repair command
make deploy SITE=blog.bythewood.me   rebuild one site and replace it
make doctor                          tunnel, network, containers, and the data volumes
make wiki                            download the offline wikipedia into its volume, 12.5GB
make down                            stop everything

make run SITE=blog.bythewood.me      vite watch + go run
make build SITE=blog.bythewood.me
make check                           gofmt, vet, govulncheck and build every site
make test                            every site's tests, including its own web/ copy
make deps                            update every site's direct Go modules together
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
`orchard-status`, `orchard-isaacbythewood`, `orchard-logging`, `orchard-repos`,
`orchard-auth`, `orchard-chat`, `orchard-llm`
and `orchard-dash`, plus an `orchard-<label>-data` volume for each site with
SQLite. One prefix, so `docker ps --filter name=orchard` is the whole system
and the Makefile derives a container name from a site directory without a
lookup table.

**Container names are baked in wherever they're used.**
`edge/caddy/Caddyfile` reverse-proxies to each site and writes its access log to
`tcp/orchard-logging:9001`, `sites/isaacbythewood.com/site.go` fetches
`http://orchard-blog:8000/latest.json` for the latest-posts panel,
`web/shipper.go` in every site posts to `http://orchard-logging:8000/ingest`,
`web/session.go` in every site asks `http://orchard-auth:8000/verify`,
status, logging and auth publish to `http://orchard-ntfy:8000`,
`sites/dash.bythewood.me/systems.go` reads `http://orchard-logging:8000/aggregate`
and probes every other site at `http://orchard-<label>:8000/healthz`, and
`sites/dash.bythewood.me/quick.go` relays the quick chat to
`http://orchard-chat:8000/api/send`. None of those reads the name at runtime,
so renaming a container means rebuilding Caddy, the portfolio and every site,
not just editing a compose file. No SQLite database refers to a container name.

**`orchard-llm` and `orchard-wiki` are the exceptions.** chat and dash reach the
gateway through `LLM_URL` and chat reaches the wiki through `WIKI_URL`, which
compose sets and which fall back to those names, so they're the only ones a dev
run can point somewhere else without a rebuild.

**Alerts leave through ntfy in the edge, and reading them is authenticated.**
status publishes to the `status` topic, logging to `logging` and auth to `auth`,
all to `http://orchard-ntfy:8000` on the bridge with a write-only token from
each site's `.env`, and reading is over the tunnel at `ntfy.bythewood.me` with a
read-only account. There are three accounts, not two: `orchard-auth` writes the
auth topic and nothing else, because the token in status' and logging' `.env`
files can publish to their topics and sharing it would let a copy of either mint
its own login codes. Two fences hold, ntfy runs `auth-default-access: deny-all`
and decides who may publish, and Caddy refuses every publish route on the public
hostname, so the write token on its own gets an outsider nowhere. ntfy has no
source-based ACL, which is why that second fence has to live at the edge.
`make ntfy` creates the accounts and `make ntfy-token` mints the token.

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
instead of assuming. Every site that needs a secret commits a `.env.example`, and
`make env` turns each example into a `.env`, filling in a generated value for any
empty `*_PASSWORD` and printing it once. It skips a site that already has one,
because rewriting one loses the ntfy token or the api key that was written into it.

**Editing anything in `edge/` needs `make edge`.** The Caddyfile and ntfy's
`server.yml` are baked into images and `make up` does not pass `--build`, so an
edited config that was never rebuilt is a silent no-op. cloudflared is worse
again, its config lives in a volume so compose sees no change and will not
restart it, and the tunnel serves the old ingress while a newly added hostname
404s from the Cloudflare edge. `make edge` restarts it explicitly, but a restart alone
still serves the old ingress, because the config it reads is the copy in the
volume and nothing has replaced it.

Adding a hostname is five changes: a Caddy site block (with `import hold` inside
its `reverse_proxy`, or a deploy of that site answers 502 until it is back), a `cloudflared` ingress
rule, the name in `HOSTNAMES` in `edge/setup-tunnel.sh`, a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`, and then `make tunnel` to reseed the
volume before `make edge`. Skip the reseed and the container is healthy, Caddy
is right, and the hostname still 404s from the Cloudflare edge. The DNS route
calls in that script fail with an authentication error unless `cert.pem` covers
that zone, which does not matter when the CNAME already exists.

**One base image, and it is Debian trixie.** Every stage of every Dockerfile here
is `debian:trixie-slim`, `golang:1.27-trixie` or `oven/bun:1-debian`, pinned by
digest, and a site with nothing to exec ends at `FROM scratch` instead. Nothing
pulls Alpine, `curlimages/curl` or any other base, including the throwaway
containers the Makefile runs, which use `orchard-util` out of `edge/util`. The
exceptions are the vendor images we do not build (`caddy`, `ntfy`, `cloudflared`
and `kiwix-serve`), where the base is theirs to pick. Reach for a new base image
and the answer is no: add the package to `edge/util` or to the runtime stage.

**Containers run as UID 65532, and runtime base images are pinned by digest.** The
five Debian sites create a real user at that UID with `groupadd`/`useradd` and the
seven scratch ones use the bare number, since there is no `/etc/passwd` to name
one in. A `/data` volume created root-owned stays root-owned, so a new one has to
be chown'd once.

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
root-owned mode 660 and the `docker` group does not help. But sudoers here sets
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
the pinned `oven/bun:1-debian` in every frontend stage is already 1.4.

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
logging keep Typst in the runtime image, which is why those three are Debian
rather than scratch because their reports come from live
data over an arbitrary date range, with no finite set to precompile.

**Fonts for Typst must be TrueType.** Geist comes from `bun add geist`, Vercel's
package and not `@fontsource/geist`, which ships woff2 only, and Typst reaches
it through `--font-path`. A missing face does not error, it falls back to a
serif, which is how `blog_post.typ` asked for Inter and rendered in DejaVu.

## Per site detail

dash and chat each keep their working detail in a `CLAUDE.md` in their own folder, which Claude Code loads when it touches that site. Read it before changing either one.

## Signing in

**One account, and it lives on auth.bythewood.me.** No site behind it holds a
password of its own. Signing in is a username and a six digit code pushed to a
phone over the `auth` ntfy topic, and every gated site asks
`orchard-auth:8000/verify` whether the cookie on a request is live.

**The session cookie is opaque and checked on every request.** It is 32 random
bytes on `.bythewood.me`, stored here only as a SHA-256, and a site cannot
validate it alone. That is what revocation needs: a signed cookie stays valid
until it expires whatever the issuer says, so signing a device out would mean
rotating a key and ending every other session with it. Nothing caches the
answer, because a cache is a window in which a revoked session still works.

**With orchard-auth down, every dashboard behind it is unreachable.** That is
inherent, and it is why the break-glass is ten Argon2id recovery codes rather
than a password, and why `make auth-init` prints the first set. ntfy is behind
the same tunnel as the sites it gates, so a bad tunnel takes the code path with
it.

**What bounds abuse is a per account ceiling, not a per IP one.** The username is
in this public repository, so anyone can post it to `/login`. One code is
outstanding at a time and a repeat request publishes nothing, and at most five
notifications go out per hour whatever address they are asked from. A per IP
limit is bypassed by sending one request from each of a thousand proxies. Codes
publish at ntfy priority `low` so a flood is silent, and only a session that
actually opened is `high`.

**A code is bound to the browser that asked for it** by a second short lived
cookie, so a code visible on a lock screen cannot be typed into somebody else's
session.

**Cloudflare's location headers are only trustworthy because of the tunnel.**
`CF-IPCountry` and the visitor location transform feed the session list and the
notification that says where a login came from. Nothing publishes a host port,
so nothing reaches an app without passing cloudflared and Caddy. The day one is
directly reachable a client can set its own country.

**auth cannot change other containers, and must not learn how.** It does not hold
the ntfy account password, which ntfy stores hashed and will not hand back, and
it does not hold a Cloudflare token that could write WAF rules. Both would make
an internet facing container into the thing that owns the machine. `make
ntfy-passwd` changes the ntfy password.

**repos keeps its own push tokens.** git cannot answer a code prompt, so the
Argon2id credentials on the wire stay in repos' database and are unaffected by
any of this. Only its browser UI moved.

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

**A template listed with no file behind it ships and then crash-loops.**
`web.NewRenderer` resolves its page list at boot, not at build, so deleting
`login.html` while leaving it listed compiled, passed every test, built an image
and then failed at startup with `pattern matches no files`. Every site now keeps
`layoutTemplates` and `pageTemplates` as package vars and a test parses them.

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

`make test` runs every site's suite plus its `web/` copy. There
are no linter configs, and `make check` is gofmt, vet, govulncheck and build.
govulncheck also checks the local Go, so it fails when webdev's Go is behind
the `golang:1.27-trixie` the images build with. The portfolio
has no Go tests of its own, being templates and handlers over static data, and
is covered by its `web/` package plus browser checks.

`web/shipper_test.go` is identical in every site and covers the parts
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
`Fix rollup window off by an hour`, and they carry no body. Keep them under 50
characters with one change per commit, since a subject that needs an and is two
commits, and leave off any Co-Authored-By trailer. Prose has no em dashes and no
semicolons, use a comma, a full stop, or and.
