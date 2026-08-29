# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and for anyone else working in this
repository. Start with `README.md`; this file is the working detail behind it.

## What this is

One repo for every site Isaac Bythewood runs, plus the shared Cloudflare Tunnel
and Caddy that front them. Four sites, all Go, all served from a desktop behind
a tunnel rather than a rented server.

| Directory | What it serves |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Animation heavy, framework free, no third party Go dependency |
| `sites/blog.bythewood.me/` | Markdown blog. A PDF and a social card per post, and an Atom feed |
| `sites/analytics.bythewood.me/` | Self hosted analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Self hosted uptime monitoring. SQLite, Lighthouse audits, crawler |
| `edge/` | The shared `cloudflared` tunnel and the Caddy that reverse proxies to each site |

## The one structural rule

**Every site is its own Go module and owns its own copy of `web/`.**

There is no module at the repo root. `go.work` exists so repo wide `make`
targets and an editor can see all four at once; nothing depends on it. Each site
builds standalone:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

and its Docker build context is that directory alone, which is what makes a site
liftable into its own repository by copying the folder.

`web/` is the small HTTP layer every site needs: the Vite manifest reader,
request logging, panic recovery, security headers, the static and edge cache
policies, and graceful shutdown. It was one shared `internal/web` under a single
root module before the split.

**A fix in `web/` has to be made four times.** If a change belongs in all four,
change all four. Do not reintroduce a shared parent module to avoid this; that
is the thing the split removed.

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
`orchard-isaacbythewood`, and the volumes `orchard-analytics-data` and
`orchard-status-data`. One prefix across the repo, so `docker ps --filter
name=orchard` is the whole system and the Makefile derives a container name from
a site directory without a lookup table. These carried a `-next` suffix until
2026-08-29, from the migration that made them the live thing; the suffix stopped
being true the day the cutover finished.

**Two things reference a container by name, and both bake it in.**
`edge/caddy/Caddyfile` reverse-proxies to each site, and
`sites/isaacbythewood.com/site.go` fetches `http://orchard-blog:8000/latest.json`
for the portfolio's latest-posts panel. Neither reads the name at runtime, so
renaming a container means rebuilding Caddy *and* the portfolio, not just editing
a compose file. Nothing in either SQLite database refers to a container name.

**Adding a hostname is three changes, not one.** A Caddy site block, a
`cloudflared` ingress rule, and a proxied CNAME to
`<tunnel-id>.cfargotunnel.com`. The edge config is baked into the images, so
both edge components need a rebuild to pick up a change.

**Containers run as UID 65532, and base images are pinned by digest.** The two
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
blog and the portfolio end at `FROM scratch` because of it. Analytics and status
keep Typst in the runtime image because their reports come from live data over
an arbitrary date range, with no finite set to precompile.

**Fonts for Typst must be TrueType.** Geist is installed with `bun add geist`
and reached through `--font-path`. Vercel's package rather than
`@fontsource/geist` for one reason: `@fontsource` ships woff2 only and Typst
cannot read it. A missing face does not error, it falls back to a serif, which
is how `blog_post.typ` asked for Inter for months and rendered in DejaVu.

## Rules learned the hard way

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

`make test` runs every site's suite plus its `web/` copy. There are no linter
configs; `make check` is gofmt, vet and build. The portfolio has no Go tests of
its own, being templates and handlers over static data, and is covered by `web/`
plus browser checks.
