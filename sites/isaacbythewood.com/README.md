# isaacbythewood.com

My portfolio, five pages heavy on animation with no framework and no third
party Go dependency. The code page shows each project's latest commit off
GitHub, and the home page the blog's latest post.

## Requirements

Go and bun, and typst for the resume PDF and the social card, without which the
resume skips with a warning and 404s. No `.env`.

## Running it

```sh
make run      vite watch + go run, on :8000
make images   regenerate the responsive image ladder from frontend/images
make resume   compile the resume PDF
make og       compile the social card
make build    all of the above, then the release binary into ../../bin/isaacbythewood.com
make clean    delete build/, node_modules and the generated PDF and card
```

A fresh clone wants `make frontend` once before `make run`, since run neither
installs the bun packages nor generates the images.

## Layout

`site.go` holds every word and link the pages use, `handlers.go` renders them,
and `github.go` and `latest.go` keep the commit and blog caches. `images.json`
is the list of widths, read by both `images.go` and the bun script that makes
them, from the archival masters in `frontend/images`, and `resume/` has its
own README.

## Things that will bite you

- The masters are about 84MB of scans and most of why the repo packs to
  roughly 95MB, against the 100MB Cloudflare allows in one push to
  repos.bythewood.me. Add to them with care.
- The images are AVIF only, so a browser older than Safari 16.4 gets none.
- GitHub is asked without a token, so it's 60 requests an hour from this IP.
