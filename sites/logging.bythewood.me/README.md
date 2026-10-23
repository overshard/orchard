# logging.bythewood.me

Log aggregation for every site here. Each one tees its slog records to
`/ingest` over the Docker bridge, Caddy writes its access log to a socket on
port 9001, and it all lands in SQLite. Raw records are kept 30 days and hourly
rollups forever, so a graph can cover a year while the database stays about the
same size. It alerts over ntfy when a source goes quiet or restarts without
shutting down, and the dashboard is behind auth.bythewood.me.

## Requirements

Go and bun, and typst for PDF reports. The `.env` carries `NTFY_TOKEN`, the
write token `make ntfy-token` mints. Unset, alerts are logged and never sent.

## Running it

```sh
make run      vite watch + go run, on :8000
make seed     fake records to look at, RECORDS= and DAYS= to resize it
make build    release binary into ../../bin/logging.bythewood.me
make clean    delete build/ and node_modules
```

`go run . -preview-alert silence` prints an alert without waiting for one, and
so do `resumed` and `restart`.

## Layout

`ingest.go` batches records through one writer, `caddy.go` listens on 9001,
`retention.go` ages out raw rows, `watchdog.go` and `alerts.go` decide what to
tell ntfy, and `aggregate.go` is the counts dash shows. `queries.go`,
`dashboard.go` and `report.go` read it all back.

## Things that will bite you

- This site gives itself a local sink, since posting its own records to
  `/ingest` would log a request that becomes another ingest.
- Caddy refuses `/ingest` and `/aggregate` on the public hostname.
- Caddy's line for a hostname whose site is shipping only goes to the rollups,
  since the site logged the same request, so searching `caddy` finds the 5xx
  and the hostnames nothing behind ships for, like the apex and ntfy.
- A rollup row is an hour, so a window that doesn't start on the hour has to
  floor its start or it drops that first bucket whole.
