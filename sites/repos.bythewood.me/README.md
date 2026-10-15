# repos.bythewood.me

A git remote with a browser on top. You push to it over HTTPS with a token, and
it also mirrors the GitHub account every six hours as a backup, with one UI over
both. Settings are behind auth.bythewood.me, but the push tokens live in this
site's own database, since git can't answer a code prompt.

## Requirements

Go, bun and git. The `.env` carries `REPOS_MIRROR`, where `0` turns the GitHub
mirror off and anything else, unset included, leaves it on.

## Running it

```sh
make run      vite watch + go run, on :8000, with the mirror off
make build    release binary into ../../bin/repos.bythewood.me
make clean    delete build/dist and node_modules, leaving the repositories
```

Make a token on `/settings`, add `https://repos.bythewood.me/<name>.git` as a
remote, and push with the token as the password. The first push creates it.

## Layout

`wire.go` is clone, fetch and push through `git http-backend`, `git.go` every
read the pages make, `mirror.go` the GitHub lane, and `throttle.go` the rate
limit on the routes a crawler could walk forever. Repositories live in
`build/repos`, or the `orchard-repos-data` volume in production.

## Things that will bite you

- Cloudflare refuses a body over 100MB, so the first push of a big repository
  goes over the bridge to `http://orchard-repos:8000/<name>.git` or in slices,
  both in the root `CLAUDE.md`.
- Every git call is a plumbing subprocess with NUL delimiters and `--` before
  anything a visitor typed. Keep it that way in `git.go`.
- Hiding a repository only takes it off the index. Anyone with the URL can
  still read and clone it, so nothing private belongs here.
