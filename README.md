# orchard

One repo for everything I host. Sites live in `sites/`, and the shared
Cloudflare Tunnel and Caddy that front them live in `edge/`.

The point is that adding or removing a project is a directory and a compose
file, not a new repository, a new deploy hook and a new set of half-remembered
conventions.

Everything here runs on a desktop at home behind a Cloudflare Tunnel. There is
no server to rent and nothing listening on an inbound port.

## Layout

Each site is its own Go module and builds on its own. There is no module at the
repo root: `go.work` exists only so repo-wide `make` targets and an editor can
see all four at once, and nothing depends on it.

```
orchard/
  go.work                    use ./sites/*, depended on by nothing
  sites/
    isaacbythewood.com/      personal site (Go + Vite), no third party deps
    blog.bythewood.me/       Markdown blog, PDFs and social cards at build time
    analytics.bythewood.me/  self-hosted analytics (Go + Vite + SQLite)
    status.bythewood.me/     self-hosted uptime monitoring (Go + Vite + SQLite)
  edge/                      cloudflared -> Caddy, shared by every site
```

Every site carries its own copy of `web/`, the small HTTP layer they all need:
the Vite manifest reader, request logging, panic recovery, security headers and
the cache policies. It used to be one shared package under a single root
module. It was split on purpose, so that a site is a folder you can copy out
into its own repository with nothing to untangle first, and so that its Docker
build context is that folder rather than the whole monorepo.

The trade is honest and worth stating: a fix in `web/` has to be made four
times.

## Running a site

```
make run SITE=isaacbythewood.com     # vite watch + go run, port 8000
make build SITE=isaacbythewood.com   # frontend, then binary
make deploy SITE=isaacbythewood.com  # docker compose up --build --detach
make check                           # gofmt, then vet and build every site
make test                            # every site's tests, and its own web/ copy
```

Every site listens on 8000, in dev and in production, and only one runs at a
time in dev. The same targets exist inside each site directory, so you can work
in one without going through the root.

## How a site is put together

Go serves every request. `html/template` renders the pages, Vite builds hashed
JS and CSS bundles, and the server reads `dist/.vite/manifest.json` to resolve
those names into the markup. Vite never serves anything; it is a build step. A
missing manifest is fatal on purpose, because serving a page whose script tag
points at a file that was never built is worse than refusing to start.

Templates are embedded in the binary because they are source. `dist/` is not:
it is a build artifact copied in beside the binary.

Typst does the typesetting, always at build time where the inputs are known
before the process boots. That covers every post PDF, the resume, and the
1200x630 social card each site serves as its `og:image`. Geist is the typeface,
installed with `bun add geist`, because Typst reads TrueType and the
`@fontsource` packages ship woff2 only.

`isaacbythewood.com` and `blog.bythewood.me` end at `FROM scratch`, holding a
static binary and the build output. `analytics` and `status` end at `alpine`,
because both generate reports from live data over an arbitrary date range and
so genuinely need the `typst` CLI at runtime; `status` also carries Chromium
for its Lighthouse audits.

## The edge

`edge/` runs one `cloudflared` and one Caddy for the whole repo. Traffic is
outbound only: no port forwarding, no dynamic DNS, no inbound firewall rules,
and the origin IP is never published.

```
cd edge
sh setup-tunnel.sh login     # once per machine
sh setup-tunnel.sh up        # create the tunnel and its DNS records
docker compose up --build --detach
```

Adding a hostname is a site block in `edge/caddy/Caddyfile`, an ingress rule in
`edge/cloudflared/config.yml`, and a proxied CNAME. Both edge configs are baked
into their images, so a change there needs a rebuild. Caddy stays in front of
the apps rather than cloudflared pointing at them directly, so compression,
security headers and per-host routing are configured once.

## Caching

The two content sites send a policy built for an origin that is a desktop in a
house:

```
public, max-age=300, stale-while-revalidate=86400, stale-if-error=604800
```

`stale-if-error` is the one that matters. It lets the edge keep serving the last
good copy for a week instead of an error page when the machine or the tunnel is
down. Note the absence of `s-maxage`: per RFC 9111 it implies
`proxy-revalidate`, which makes Cloudflare disable both stale directives, so
adding it would quietly undo the whole point. Anything that answers 400 or above
is given an explicit `no-store`, so an error cannot be pinned at the edge.

## Running as non-root

Every container runs as UID 65532 rather than root, and the base images are pinned by
digest. The two Alpine images create a real user at that UID; the two `FROM scratch`
images use the bare number, because there is no `/etc/passwd` to name a user in.

A `/data` volume created root-owned stays root-owned, so an existing one needs a
one-time `chown -R 65532:65532` before its service can be switched over.

Each service also health-checks itself: `-healthcheck` does a loopback GET against
`/healthz` and exits 0 or 1. That exists because an image with no shell has nothing for
a `HEALTHCHECK` to call.

## Secrets

There are none in this repo, and that is deliberate rather than lucky. Site
identity (hostname, contact address, theme) is hardcoded, so there is no
`.env.example`, no `BASE_URL` indirection and no config-or-credential gray zone
to misjudge.

Three credentials exist in the system and none is a file here. The tunnel's
lives in a Docker volume that `setup-tunnel.sh` creates. `analytics` and
`status` each read one password from the environment, and those two are the
only *secrets* anything here reads. The handful of other variables that exist
(`SITE_DIST`, `SITE_CONTENT`, `SITE_PDFS`, `SITE_OG`, `CHROMIUM_BIN`) are paths
set by the Dockerfiles, with working defaults for a local checkout:

```
ANALYTICS_PASSWORD=... make deploy SITE=analytics.bythewood.me
STATUS_PASSWORD=...    make deploy SITE=status.bythewood.me
```

They come from the deploying shell rather than an `.env` next to the compose
file, so they are never written near the repo, and compose refuses to start
without them.

## State

`analytics` and `status` each have a SQLite database, and nothing else here has
any state at all. Those databases live in named Docker volumes mounted at
`/data`, because every deploy runs `docker compose up --build` and the container
is replaced. Bind mounts are not an option: the daemon is Docker Desktop on the
Windows host and resolves paths against its own filesystem, so a bind mount of a
path under `/home/dev` silently becomes an empty directory rather than an error.

## Contributing

`CLAUDE.md` in this directory has the working detail: the one structural rule
about `web/`, the conventions that will bite you, and the rules learned the hard
way (never animate a layout property, `og:image` must be raster, and the
`s-maxage` trap above).
