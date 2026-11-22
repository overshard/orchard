# status.bythewood.me

Uptime monitoring for one operator. A scheduler inside the process probes every
tracked URL every three minutes, audits it daily and crawls it
for SEO problems weekly, and the pages mostly render what it wrote to SQLite.
Outages and recoveries go out over ntfy and are kept as incidents, and a
property can be public or kept behind auth.bythewood.me. Signed out, a public
property shows only its 90 day bar, incidents and response times.

## Requirements

Go and bun, and typst for PDF reports. The `.env` carries `NTFY_TOKEN`, the write
token `make ntfy-token` mints. Unset, alerts are logged and never sent.

## Running it

```sh
make run          vite watch + go run, on :8000
make preview      print a down and a recovery alert without anything breaking
make build        release binary into ../../bin/status.bythewood.me
make clean        delete build/, node_modules and data/, the dev database
```

## Layout

`scheduler.go` runs the timers, `checker.go` does the probes, the `audit*.go`
files the daily audit, and the `crawler*.go` files the SEO crawl. The audit is
plain Go with no browser, one fetch of the page and everything it loads, scored
on performance, accessibility, best practices and SEO. Paint timings and the
errors visitors hit come from analytics over `orchard-analytics:8000/bridge/vitals`
in `realusers.go`, since only a real browser can measure them.
`rollup.go` writes a row per property per UTC day into `days`, and `history.go`
reads that and `incidents` back for the pages. `alerts.go` words the alerts and
`ntfy/` sends them, and `report.go` with
`reports/` and `typst.go` writes the PDFs. The frog on the properties pages is the visitor's
own from pond's `/api/panel`, drawn with a copy of pond's `frog.js` that
`make check` holds to the original.

## Things that will bite you

- Uptime checks get their own worker pool apart from the crawls and audits, so
  two slow crawls can't quietly stop every check. Don't merge them.
- Checks are deleted after three days, but `days` and `incidents` are kept
  forever. A day's row is only ever replaced by one with more checks, so the
  oldest day doesn't shrink as its checks age out. The first boot with an
  `incidents` table reads past outages back out of the checks still kept, once,
  and marks those rows backfilled since whether a push went out isn't known.
- The error budget is 99.9% of thirty days, about 43 minutes, and each failed
  check spends one check interval of it.
- The prober times DNS, TCP, TLS and the request on its own connection rather
  than through `http.Client`, since a pooled one would hide what a first visit
  pays. It speaks HTTP/2 over TLS only, so a property has to be `https://`.
- The audit's TTFB is timed from this server out through Cloudflare, so it's
  the edge as much as the origin. The real users panel only fills when the
  analytics property is named after the host, like `blog.bythewood.me`.
