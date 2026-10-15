# status.bythewood.me

Uptime monitoring for one operator. A scheduler inside the process probes every
tracked URL every three minutes, audits it with Lighthouse daily and crawls it
for SEO problems weekly, and the pages mostly render what it wrote to SQLite.
Outages and recoveries go out over ntfy, and a property can be public or kept
behind auth.bythewood.me.

## Requirements

Go and bun, typst for PDF reports, and a Chromium for the audits (or
`CHROMIUM_BIN` pointing at one). The `.env` carries `NTFY_TOKEN`, the write
token `make ntfy-token` mints. Unset, alerts are logged and never sent.

## Running it

```sh
make run          vite watch + go run, on :8000
make lighthouse   install the Lighthouse CLI, without which audits are skipped
make preview      print a down and a recovery alert without anything breaking
make build        release binary into ../../bin/status.bythewood.me
make clean        delete build/, both node_modules and data/, the dev database
```

## Layout

`scheduler.go` runs the timers, `checker.go` does the probes, `lighthouse.go`
and `process.go` the audits, and the `crawler*.go` files the SEO crawl.
`alerts.go` talks to ntfy, and `report.go` with `reports/` and `typst.go` writes
the PDFs. The image is the heaviest here, since it carries Chromium, bun and
Lighthouse as well as typst.

## Things that will bite you

- Uptime checks get their own worker pool apart from the crawls and audits, so
  two slow crawls can't quietly stop every check. Don't merge them.
- The prober times DNS, TCP, TLS and the request on its own connection rather
  than through `http.Client`, since a pooled one would hide what a first visit
  pays. It speaks HTTP/2 over TLS only, so a property has to be `https://`.
