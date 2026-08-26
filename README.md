# orchard

One repo for everything I host. Sites live in `sites/`, the shared Cloudflare
Tunnel and Caddy that front them live in `edge/`, and the pieces every site
needs live in `internal/web/`.

The point is that adding or removing a project is a directory and a compose
file, not a new repository, a new deploy hook and a new set of half-remembered
conventions.

## Layout

```
orchard/
  go.mod                     one module for the whole repo
  internal/web/              asset manifest, middleware, render, server
  sites/
    isaacbythewood.com/      personal site (Go + Vite)
    blog.bythewood.me/       Markdown blog, PDFs compiled at build time
    analytics.bythewood.me/  self-hosted analytics (Go + Vite + SQLite)
  edge/                      cloudflared -> Caddy, shared by every site
```

## Running a site

```
make run SITE=isaacbythewood.com     # vite watch + go run, port 8000
make build SITE=isaacbythewood.com   # frontend then binary
make check                           # gofmt, vet and build everything
```

Every site listens on 8000, in dev and in production, and only one runs at a
time in dev.

## How a site is put together

Go serves every request. `html/template` renders the pages, Vite builds one
hashed JS bundle and one hashed CSS bundle, and the server reads
`dist/.vite/manifest.json` to resolve those names into the markup. Vite never
serves anything; it is a build step.

Templates are embedded in the binary because they are source. `dist/` is not:
it is a build artifact copied in beside the binary, which keeps `go build`
working on a fresh clone before anyone has run Vite.

Images end at `FROM scratch`, holding a static binary, the build output, and a
CA bundle for the sites that talk to an external API. `analytics.bythewood.me`
is the exception: it shells out to the `typst` CLI to build a PDF report for an
arbitrary date range, and typst needs real font files on disk, so that one ends
at `alpine`.

## The edge

`edge/` runs one `cloudflared` and one Caddy for the whole repo. Traffic is
outbound-only: no port forwarding, no dynamic DNS, no inbound firewall rules,
and the origin IP is never published.

```
cd edge
sh setup-tunnel.sh login     # once per machine
sh setup-tunnel.sh up        # create the tunnel and its DNS records
docker compose up --build --detach
```

Adding a hostname is a site block in `edge/caddy/Caddyfile` plus an ingress
rule in `edge/cloudflared/config.yml`. Caddy stays in front of the apps rather
than cloudflared pointing at them directly, so compression, security headers
and per-host routing are configured once.

## Secrets

There are none in this repo, and that is deliberate rather than lucky. Site
identity (hostname, contact address, theme) is hardcoded, so there is no
`.env.example`, no `BASE_URL` indirection and no config-or-credential gray zone
to misjudge.

Two credentials exist in the system and neither is a file here. The tunnel's
lives in a Docker volume that `setup-tunnel.sh` creates. `analytics` reads one
password from the environment, which is the only environment variable any site
in this repo reads:

```
ANALYTICS_PASSWORD=... make deploy SITE=analytics.bythewood.me
```

It comes from the deploying shell rather than an `.env` next to the compose
file, so it is never written near the repo, and compose refuses to start
without it.

## State

Only `analytics` has any. Its SQLite database and its GeoIP database live in a
named Docker volume mounted at `/data`, because every deploy runs
`docker compose up --build` and the container is replaced. Bind mounts are not
an option: the daemon is Docker Desktop on the Windows host and resolves paths
against its own filesystem, so a bind mount of a path under `/home/dev`
silently becomes an empty directory rather than an error.
