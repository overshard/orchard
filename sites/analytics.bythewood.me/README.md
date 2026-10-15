# analytics.bythewood.me

Self hosted analytics for one operator. A small collector script on each site
posts page views to `/collect`, they land in SQLite for a year, and the
dashboard turns them into metric tiles, charts, a world map and Markdown or PDF
reports. Managing properties is behind auth.bythewood.me, and a property can be
flipped public so its dashboard reads without signing in.

## Requirements

Go and bun, and typst on the PATH for PDF reports. No `.env`.

## Running it

```sh
make run      vite watch + go run, on :8000
make seed     a Seed Test property full of fake traffic, SESSIONS= and DAYS= to resize it
make maps     regenerate the per country topojson, which the first build does for you
make build    release binary into ../../bin/analytics.bythewood.me
make clean    delete build/ and node_modules
```

## Layout

`collector.go` takes the events, `queries.go` and `dashboard.go` read them back,
`report.go` with `reports/` and `typst.go` writes the reports, `geoip.go` and
`ua.go` turn an address and a user agent into a place and a browser, and
`retention.go` deletes anything past a year. The database and the GeoIP file
live in `data/`, or the `orchard-analytics-data` volume in production.

## Things that will bite you

- The first boot downloads DB-IP's city database, about 131MB, and countries
  stay blank until it lands. It refreshes itself monthly.
- Without typst a PDF report answers 503, and `?report=md` still works.
- Sessions are checked against `orchard-auth` by container name, so a dev run
  off the Docker network only sees public pages. The seeded property is
  private, so set `is_public` on its row in `data/db.sqlite3` to look at it.
