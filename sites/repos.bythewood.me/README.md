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
read the pages make, `history.go` the reads kept in memory between views,
`mirror.go` the GitHub lane, and `throttle.go` the rate limit on the routes a
crawler could walk forever. A repository page opens on the suite strip, `t`
jumps to any file at the branch, and a line number picks a line or, with shift,
a range for a `#L10-L24` link. Repositories live in
`build/repos`, or the `orchard-repos-data` volume in production.

## Things that will bite you

- Cloudflare refuses a body over 100MB, so the first push of a big repository
  goes over the bridge to `http://orchard-repos:8000/<name>.git` or in slices,
  both in the root `CLAUDE.md`.
- Every git call is a plumbing subprocess with NUL delimiters and `--` before
  anything a visitor typed. Keep it that way in `git.go`.
- A hidden repository is off the index and answers a 404 to anyone signed out,
  so its name doesn't leak. Cloning one takes a push token sent up front, like
  `git -c http.proactiveAuth=basic clone https://git:<token>@repos.bythewood.me/<name>.git`,
  since git only sends a credential after a 401. It's still not for anything
  private.
- Signed out, pages are only served at a branch or tag name, and history,
  diffs and anything at a commit SHA answer a signed in only 404. Every SHA is
  another copy of the tree for a crawler, so a signed out page must never link
  to one. The commit tiles, recent commits, the last commit column and `y`
  pinning a line link to its commit are all signed in only for that reason.
- The last commit column is a second request after the page paints, one
  `git log` that stops once every entry has a commit, after 2,000 commits or
  after five seconds, whichever comes first.
