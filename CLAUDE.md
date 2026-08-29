# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and for anyone reading this repository.

This file is deliberately the contributor-facing half. The workspace this repo lives in keeps
its project guidance in a private Markdown vault (see
`code/memory/decisions/0002-consolidate-claude-md-into-memory.md`), and that is still where the
operational detail lives: hostnames under change, cutover history, dashboard configuration,
anything with a credential near it. This file exists because orchard is published and the vault
is not, so a reader who only has the repo still gets the parts that are about the code.

## What this is

The home base monorepo: every site Isaac Bythewood runs, plus the shared Cloudflare Tunnel and
Caddy that front them. Four sites, all Go, all served from a desktop behind a tunnel rather than
a rented server.

| Directory | What it serves |
|---|---|
| `sites/isaacbythewood.com/` | Portfolio. Animation heavy, framework free, no third party Go dependency at all |
| `sites/blog.bythewood.me/` | Markdown blog. Build time PDF and social card per post, Atom feed |
| `sites/analytics.bythewood.me/` | Self hosted analytics. SQLite, GeoIP, Typst PDF reports |
| `sites/status.bythewood.me/` | Self hosted uptime monitoring. SQLite, Lighthouse audits, crawler |
| `edge/` | The shared `cloudflared` tunnel and the Caddy that reverse proxies to each site |

## The one structural rule

**Every site is its own Go module and owns its own copy of `web/`.**

There is no module at the repo root. `go.work` exists only so repo wide `make` targets and an
editor can see all four at once; nothing depends on it. Each site builds standalone:

```sh
cd sites/blog.bythewood.me && GOWORK=off go build ./...
```

and its Docker build context is that directory alone, which is what makes a site liftable into
its own repository by copying the folder.

`web/` is the small HTTP layer every site needs: the Vite manifest reader, request logging, panic
recovery, security headers, the static cache policy, the edge cache policy and graceful shutdown.
It used to be one shared `internal/web` under a single root module. It was split on purpose.

**The cost is real and is the trade that was chosen: a fix in `web/` has to be made four times.**
If a change belongs in all four, change all four. Do not reintroduce a shared parent module to
avoid this; that is the thing that was deliberately removed.

## Commands

From the repo root:

```sh
make run SITE=blog.bythewood.me      # vite watch + go run
make build SITE=blog.bythewood.me
make deploy SITE=blog.bythewood.me   # docker compose up --build --detach
make check                           # gofmt, then vet and build every site
make test                            # every site's tests, including its own web/ copy
make edge-up                         # the shared tunnel and Caddy
make sites                           # list them
```

Each site has the same targets in its own `Makefile` and can be driven from its own directory
without the root one.

## Conventions that will bite you

**Port 8000, everywhere.** Every container listens on 8000 internally, in dev and in prod alike.
No host ports are published. `cloudflared` terminates the tunnel and hands plain HTTP to Caddy,
which reverse proxies to each app by container name on the `orchard-edge` network.

**Adding a hostname is three changes, not one.** A Caddy site block, a `cloudflared` ingress
rule, and a proxied CNAME to `<tunnel-id>.cfargotunnel.com`. The edge config is baked into the
images, so both edge components need a rebuild to pick up a change.

**Containers run as UID 65532, not root**, and base images are pinned by digest.
The two Alpine sites create a real user at that UID; the two scratch ones use the bare
number, because there is no /etc/passwd to name one in. A `/data` volume created
root-owned stays root-owned, so a new one has to be chown'd once.

**The binary is its own health check.** `-healthcheck` does a loopback GET against
`/healthz` and exits 0 or 1. It exists because a `FROM scratch` image has no shell for
a HEALTHCHECK to shell out to; compose and the Dockerfiles both use it.

**Logging is `log/slog`, JSON to stdout, UTC.** `web.SetupLogging()` is called by every
main before anything else logs. UTC is not slog's default and is forced through
ReplaceAttr; local time in a container silently differs from the host's.

**Deploys need `sudo`.** The Docker socket is `root:root` mode 660; being in the `docker` group
does not help. Sites with a password read it from the deploying shell (`compose` refuses to start
rather than bring up a server with no secret), so it is never written next to the repo.

**Frontends are bun and Vite 8.** Vite 8 bundles with Rolldown, which needs a Node
newer than 18 or, as here, bun 1.4. That is why the webdev container dropped nodejs.

**Frontends are bun and Vite.** Output goes to `dist/` with content hashed filenames; the Go
binary reads `dist/.vite/manifest.json` to resolve them. A missing manifest is fatal on purpose:
serving a page whose script tag points at a file that was never built is worse than refusing to
start.

**Typst runs at build time, not on the request path.** Post PDFs, the resume and every social
card are compiled during `docker build` and served as files. The blog and the portfolio end at
`FROM scratch` because of it. Analytics and status keep Typst in the runtime image only because
their reports are generated from live data and have no finite set to precompile.

**Fonts for Typst must be TrueType.** Geist is installed with `bun add geist` and reached through
`--font-path`. Vercel's own package is used rather than `@fontsource/geist` for exactly one
reason: `@fontsource` ships woff2 only and Typst cannot read it. A missing face does not error,
it silently falls back to a serif, which is how `blog_post.typ` asked for Inter for months and
rendered in DejaVu.

## Rules learned the hard way

**Never animate a layout property.** Animating `height`, `width`, `top` or `left` relays out the
page on every frame, and the browser scores every one of those frames as a layout shift even when
the animation is deliberate and covers the whole viewport. The portfolio's opening curtain scored
0.36 CLS this way, three and a half times Google's threshold, with no image involved. Use
`transform` (`scaleY`, `translateX`) with `transform-origin`; it is composited, visually identical
and scores zero. Verify with `document.getAnimations()` and a pixel diff, not by eye.

**`og:image` must be a raster format.** Facebook, X, LinkedIn, Slack, iMessage and Discord all
refuse `image/svg+xml`. A vector card is a card nobody ever sees.

**Do not put `s-maxage` in a cache policy here.** Per RFC 9111 it carries `proxy-revalidate`
semantics, so Cloudflare treats it as "never serve stale without asking first" and it silently
disables both `stale-while-revalidate` and `stale-if-error`. Those two are the point: the origin
is a desktop at the end of a tunnel, and `stale-if-error` is what lets the edge keep serving the
last good copy instead of a 530 when the house goes dark. Use plain `max-age` with both stale
directives, and keep Cloudflare's Always Online off, because it makes `stale-if-error` ignored.

**Error responses get `no-store` explicitly.** Saying nothing is not neutral once a Cache Rule
marks a zone eligible: Cloudflare stamps its own browser TTL on a header-less response and will
hold a 404 at the edge, so publishing a post at a URL somebody already missed would serve them
the 404 for hours.

**Hardcode identity, never hardcode credentials.** Base URLs and site names are constants in
`site.go`. The only environment variables that exist are genuine secrets. A `BASE_URL` that
defaults to empty is worse than no variable at all: it shipped a site whose own tracking silently
did nothing.

## Tests

`make test` runs every site's suite plus its `web/` copy. There are no linter configs anywhere;
`make check` is gofmt, vet and build. The portfolio has no Go tests of its own (it is templates
and handlers over static data) and is covered by `web/` plus browser checks.
