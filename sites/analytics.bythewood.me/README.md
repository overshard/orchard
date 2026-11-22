# analytics.bythewood.me

Self hosted analytics for one operator. A small collector script on each site
posts page views to `/collect`, they land in SQLite for a year, and the
dashboard turns them into metric tiles, charts, a world map and Markdown or PDF
reports. Managing properties is behind auth.bythewood.me, and a property can be
flipped public so its dashboard reads without signing in.

## Requirements

Go and bun, and typst on the PATH for PDF reports. A `.env` from `.env.example`
carries the ntfy token, and without it nothing is pushed.

## Running it

```sh
make run      vite watch + go run, on :8000
make seed     a Seed Test property full of fake traffic, SESSIONS= and DAYS= to resize it
make maps     regenerate the per country topojson, which the first build does for you
make build    release binary into ../../bin/analytics.bythewood.me
make clean    delete build/ and node_modules
```

## Layout

`collector.go` takes the events, and `dashboard.go`, `filters.go`, `stats.go`,
`tiles.go`, `queries.go` and `chart.go` read them back into the dashboard. Every
row on it is a link that adds an `f_<field>` filter to the URL, and the filters
narrow the SQL, so a filtered view is a link you can share. `goals.go` keeps the
custom events marked as goals,
`report.go` with `reports/` and `typst.go` writes the reports, `geoip.go` and
`ua.go` turn an address and a user agent into a place and a browser, and
`retention.go` deletes anything past a year, and `surge.go` pushes to the
`alerts` ntfy topic when real people arrive in unusual numbers and keeps each
push in the `alerts` table for the chart. `perf.go` takes the one perf event
the collector sends when a page is hidden, web vitals and anything that went
wrong in the visitor's browser, and keeps it in `vitals` and `page_errors`
for the real users panels and for status to read over `/bridge/vitals`. The database and the GeoIP file
live in `data/`, or the `orchard-analytics-data` volume in production.

## Things that will bite you

- The first boot downloads DB-IP's city database, about 131MB, and countries
  stay blank until it lands. It refreshes itself monthly.
- Without typst a PDF report answers 503, and `?report=md` still works.
- Sessions are checked against `orchard-auth` by container name, so a dev run
  off the Docker network only sees public pages. The seeded property is
  private, so set `is_public` on its row in `data/db.sqlite3` to look at it.
- A surge needs at least 10 people and 20 views in an hour and five times the
  property's average hour over two weeks, and one outside site sending 5 people
  in an hour is a link alert. Each pushes once per 6 or 24 hours, and the
  cooldowns are read back from the `alerts` table when the watcher starts.
- `perf` is a reserved event name, a custom event pushed under it goes to
  `vitals` and never shows as a card. Headless Chrome counts as a bot, so a
  browser test needs a normal user agent to see its numbers land.
- `/bridge/vitals` has no auth of its own, Caddy refuses `/bridge` on the
  public hostname so only a container on `orchard-edge` reaches it.
- A visit is a visitor's events with no gap of half an hour, cut at query time,
  so entry pages, bounce and the referrer, utm, entry and goal filters all hang
  off it. Country and region live only on a visitor's first `session_start`,
  so those filters match the visitor and not the row.
