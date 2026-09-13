# house.bythewood.me

A house hunting dashboard. It takes listings from an MLS export, filters out the
ones that break a dealbreaker, scores the rest against the household's actual
daily driving, and shows what survives photo first.

Unlike every other site in this repository there is no public half. Every route
is behind the session on auth.bythewood.me, because what is on these pages is an
address, a budget and a child's school run.

## What it needs

- Go 1.27 and bun 1.4, same as every other site here
- A `data/config.json`, copied from `config.example.json` and filled in. The
  whole file is gitignored along with the rest of `data/`, which is where the
  addresses and the budget live: this repository is public
- At least one `.csv` in `data/import/`
- Nothing else. No API keys, no accounts, no paid services

## Running it

```sh
make run        vite watch + go run, on :8000
make refresh    one pass of the pipeline, printing what it did
make build      release binary into ../../bin/
make clean
```

From the repository root, `make run SITE=house.bythewood.me` does the same thing.

The refresh is also a button in the header, which starts it in the background and
answers straight away: a full run paces itself against six external services and
would sit well past any request timeout.

## How it is laid out

```
main.go        wiring and the route table
config.go      the Config struct, and the defaults a missing config falls back to
db.go          the SQLite schema, every statement IF NOT EXISTS
listing.go     the normalized listing, and dedupe
adapter_csv.go the CSV import, which is the only adapter that is real
guard.go       pacing, a persisted breaker and a budget, in front of every upstream
geocode.go     the Census geocoder
flood.go       FEMA flood zones and USGS hydrography
roads.go       NCDOT traffic counts, OSM road class, the corner test, ramps
terrain.go     USGS 3DEP slope sampling, and the fence read off the remarks
schools.go     attendance boundaries, per county
route.go       OSRM, and the drop-off detour model
mortgage.go    the all-in monthly figure
flags.go       every rule, and whether it excludes or docks points
score.go       the nine factors and the breakdown
refresh.go     the pipeline
queries.go     the read side
handlers.go    the pages
photos.go      the photo proxy and its disk cache
```

## What it reads, and what it cannot

Every source is free and needs no key. Each is verified working rather than
assumed.

| Question | Source |
|---|---|
| Where is this address | Census geocoder |
| Is it in a flood zone, and how far from one | FEMA National Flood Hazard Layer, layer 28 |
| How far is the nearest water, and what kind | USGS National Hydrography Dataset |
| How busy is the road it fronts | NCDOT AADT traffic segments |
| What class is the road, is it a corner lot, where is the nearest ramp | OpenStreetMap via Overpass |
| How flat is the yard | USGS 3DEP elevation, 1 m, sampled on a grid |
| Which schools is it zoned to | Alexander and Caldwell county GIS attendance boundaries |
| Where are the licensed nursing homes | NC DHSR licensed facility roster |
| How long is the drive | OSRM |
| What will it cost a month | NCDOR county tax rates, in config |

Four gaps, all of them deliberate and visible on the page rather than filled in
with a guess:

**Catawba, Burke and Iredell publish no attendance boundary layer.** Alexander
and Caldwell both do and both are queried live. The other three do not, so a
listing there reads `zoning unverified` and names the district to check by hand.
It is not guessed from the nearest school, because the drop-off detour is the
heaviest factor here and measuring it to the wrong school would be worse than
leaving it blank.

**School performance grades have to be dropped in.** NC DPI publishes them as a
spreadsheet on a web page, not as an API. Without them the school factor scores
whether the zoning is real rather than how good the schools are.

**There is no defensible neighbourhood quality metric at this scale.** What is
published for these counties is a county-level crime rate, which is one number
for four hundred square miles and says nothing about a road. Municipal police
report separately and the rural majority of the search area has no municipality
at all. Code enforcement and condemned property data is not published by any of
the four. A composite built on that would be a number with no information in it,
so there is none: the road, the flood margin and the school zone are measured
instead, and the rest is a drive past.

**Municipal tax rates are not in the config.** The county rate is, from NCDOR,
and it is the larger half. A house inside Taylorsville or Lenoir or Hickory town
limits pays a municipal rate on top, which is worth $30 to $60 a month, so the
monthly figure for an in-town listing is low. The `county:city` keys in
`county_tax_per_100` are where those go.

## Getting real listings into it

Neither Zillow nor Realtor.com has a free sanctioned listing API. Both block
scrapers, both forbid it in their terms, and a spoofed user agent gets a house
hunt's worth of requests banned from the sites worth searching. So nothing here
scrapes anything. Three ways in, in order of how little they cost.

**Redfin's Download All, which needs no account and no key.** Run a search on
redfin.com, scroll to the bottom of the results and click the download link. It
hands back a CSV of every result with a coordinate, an MLS number and a link back
to each listing. Drop it in `data/import/` and hit Refresh. Redfin publishes that
button for people to use and this is a person using it, which is the whole reason
it is the first option here. It carries no photographs.

**An export from a buyer's agent.** A saved MLS search exported out of Canopy
carries everything the other two do plus photographs and the agent remarks, which
are what the fence test reads and what makes the grid worth looking at. Same
folder, same button.

**RentCast, for listings that arrive on their own.** Set `RENTCAST_API_KEY` in
`.env` and the adapter pulls active listings inside the search radius on every
refresh. The free plan is fifty requests a month and one refresh spends one of
them, because a single request covers the whole area. No photographs and no link
to the original listing, so it works best beside one of the two above rather than
instead of them.

The same house from two sources is one row, deduped on the MLS number, then the
normalized address, then a hundred and fifty feet of proximity. A field the newer
row does not carry keeps what is already there, which is what lets the API's
coordinate and the export's photographs end up on the same card.

Column names differ by MLS and by export template, so the reader matches on a
squashed lowercase form: `List Price`, `list_price` and `ListPrice` are one
column. Address is the only one it insists on. A file with no address column is
an error rather than an empty import, because a file that quietly imports nothing
looks like a market with no houses in it.

A RESO Web API adapter is the next one, for Canopy MLS through MLS Grid. That
needs a data licence and usually broker sponsorship, and MLS Grid does not publish
prices, so it is not something to sign up for on a whim.

## What will bite you

**A refresh is slow on purpose.** Every upstream is somebody else's free server,
and several are one machine in a county building. Each sits behind a guard that
paces requests with jitter, keeps a hard per-window budget, honours 429 and 503
and both formats of Retry-After, and trips a breaker whose state lives in SQLite
so a restart does not hand a struggling service a fresh round. The backoff doubles
each time the breaker reopens, from ten minutes up to eight hours, and one clean
answer forgives the history. The footer lists the state of all of them. A run over
a hundred listings takes several minutes, and that is the design.

**The refresh button has a cooldown.** Five minutes, because every answer a run
collects is cached and a second run straight after the first would spend requests
re-asking questions nothing has changed the answer to.

**Overpass answers 504 under load,** often enough that one endpoint means no road
data for a whole run. There are three mirrors, each with its own breaker, tried
in order.

**Every external answer is cached forever, keyed on the rounded coordinate.** A
flood zone and a road layout do not change between Tuesdays. When the shape of a
cached payload changes, the cache kind has to change with it: an older row
decoded into a newer struct reads as zero, and zero feet from water is a very
different claim from not measured.

**Distances are -1 when nothing was found, never +Inf.** These structs are cached
as JSON and JSON cannot carry an infinity, so a marshal would fail on exactly the
rows furthest from water. Both lookups also carry a `Measured` flag, because the
zero value of either one otherwise reads as standing in a creek beside a motorway
ramp.

**The water buffer is three buffers.** NHD maps every wet-weather ditch in these
counties, and nearly every rural parcel has one inside 300 feet. Buffering all
water alike excluded eleven of sixteen sample listings and found no actual flood
risk. Year-round water gets 300 feet, an intermittent line gets 150, and a ditch
gets none and only moves the score.

**A main road has to be at frontage distance to count.** Off a small road that
leads to a main road is the arrangement that is wanted, so a primary road drawn
four hundred feet away says nothing about the driveway. The test is the class or
the traffic count within `aadt_radius_ft`.

**Which rules exclude is config, not code.** Five exclude and the rest dock
points and show a chip saying why, because a corner lot or an old doublewide does
not sink an otherwise perfect house. `filters.severity` flips any of them.

**His verdict lives in its own table and a refresh never touches it.** That is
the reason it is a separate table.

**Photos are served from disk, never hotlinked.** The first request for one
fetches it, writes it under the data volume and serves the copy. A grid of big
photos opened twice a day from two phones would otherwise hammer somebody else's
CDN, and that CDN would know which houses are being looked at.

**The phone layout is the real one.** Both readers are usually standing in a
driveway, so the single column is the design and the wide screens are the media
queries.

## Licence

See `LICENSE.md` at the repository root.
