# __SITE__

What this site is for, in a sentence or two. It started from `make new`, so
until that changes it's one page, a 404 and a health check.

## Requirements

Go and bun. It reads no secrets, so there's no `.env`.

## Running it

```sh
make run      vite watch + go run, on :8000
make build    release binary into ../../bin/__SITE__
make clean    delete build/ and node_modules
```

From the repo root `make deploy SITE=__SITE__` builds the image and replaces
`orchard-__LABEL__`.

## Layout

`main.go` wires the routes, `site.go` holds the hostname and the rest of its
identity, and `templates/` is embedded in every build. `frontend/` is the Vite
source, which builds into `build/dist/`, and a release build embeds that too.
`web/` is this site's own copy of the HTTP layer every site here shares.

## Things that will bite you

- `go run .` on its own exits at startup, because there's no Vite manifest
  until the frontend has built once. `make run` waits for it.
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
- The image ends at `FROM scratch` with no CA bundle, so the first fetch over
  TLS fails x509 until the Dockerfile copies one in.
