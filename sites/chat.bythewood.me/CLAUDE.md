# chat.bythewood.me

Working detail for this site. The repo-wide rules are in `CLAUDE.md` at the root, and the look is in `DESIGN.md`.

## The house lookups

`internal/property/` answers what a house at an address would be like to live in and what
it would cost, from public records and nothing else. It lives in chat because a
dashboard can draw everything about a house at once and cannot answer a
question, while every question about a house is a follow up: what about
the flood zone, what if we put ten percent down, what would USDA cost.

**Every fact comes from a free public service and each one is guarded.** FEMA's
flood layer, USGS hydrography and elevation, NCDOT traffic counts, Overpass, the
Census geocoder, OSRM, the DHSR roster, the Esri ACS layers, EPA's facility layers, USDA's eligibility
map and two county GIS servers that are one machine in a county building.
`guard.go` puts each behind `guard/`, the package dash shares, with jitter on the
pace, a hard per-window budget, 429, every 5xx and both formats of Retry-After
honoured, and a breaker whose state is a row in SQLite instead of memory, so a
restart does not hand a struggling service a fresh round.

**There is no refresh.** The instinct when an answer looks short is to ask again,
which is how a home address gets blocked by somebody's free service. A report that
came up short names what is missing and which upstreams are resting, and the next
question finds the gap filled.

**A geocoded point sits in the road, not on the lot.** The Census geocoder
interpolates along the street centreline, so a point-in-polygon parcel query
matches nothing. It queries an envelope and takes the nearest parcel, and says
when the parcel it found is not the address that was asked about.

**Two county parcel layers are unusable and the statewide one is not.**
Alexander's declares a world projection while holding state plane coordinates, so
ArcGIS reprojects a query into the Gulf of Guinea, and Catawba's publishes geometry
with no attributes. NC OneMap aggregates all hundred counties and declares EPSG
2264 correctly.

**The user agent is per endpoint, and the two kinds want opposite things.** A
public records server behind a WAF answers 403 to anything that is not a browser,
which is what the DHSR roster does. An API meant for programs wants the opposite:
Overpass answers 406 to a browser string, which is how fixing the roster broke
every road lookup after it.

**The water buffer is three buffers, not one.** NHD maps every wet-weather ditch
in these counties and nearly every rural parcel has one inside 300ft, so one
buffer over all water flagged eleven of sixteen sample addresses and found no
flood risk at all. Year-round, intermittent and ditch each get their own.

**A main road has to be at frontage distance to count.** Off a small road that
leads to a main road is a different thing from fronting one, so a primary road
drawn four hundred feet away says nothing about the driveway. Both the traffic
count and the OSM class are gated on `aadt_radius_ft`.

**Two counties publish attendance boundaries and three do not.** Alexander
(`BoundaryLayers` 32, 33, 34) and Caldwell (`Public_Access` 13 to 16) are queried
live. Catawba, Burke and Iredell publish none, so those read `nearest, not
confirmed` and are never passed off as the zoned school, because the drop-off
detour is measured to whatever school is named.

**Overpass answers 504 under load** often enough that a single endpoint means no
road data at all, and publishes a two concurrent query limit it enforces. There
are several mirrors, each with its own guard, and all Overpass traffic goes
through one mutex.

**Distances are -1 when nothing was found, never +Inf,** since these structs are
cached as JSON and JSON cannot carry an infinity, so a marshal would fail on
exactly the rows furthest from water. Both lookups also carry a `Measured` flag,
because the zero value otherwise reads as standing in a creek beside a motorway
ramp.

**A cached payload's shape is part of its key.** Every external answer is cached
on the rounded coordinate, because a flood zone does not change between Tuesdays.
When a struct gains a field the kind has to change with it (`roadv2`, `floodv2`,
`parcel3`, `acs2`), and the assembled report carries a `reportVersion` for the
same reason: an older row decoded into a newer struct reads as zero, and zero is a
claim.

**The tool takes a section, like `orchard_dash` takes a panel.** A whole report is
several thousand tokens and a conversation about one house is a dozen questions,
so `Aspect` slices it and the first answer names the other sections. The first
call about an address does the lookups and every call after it is one SQLite read,
which is what makes a follow up free rather than another minute of somebody else's
servers.

**A cold report answers before it is finished.** A dozen paced calls run past a
minute and nobody in a conversation sits through that, so the assessment runs in
the background on its own context and the tool waits 75 seconds for it. What is
done comes back, the gaps are named, and the finished report lands in the cache for
the next question. The background run outliving the turn is the part that matters:
a caller who gave up waiting still leaves a finished report behind.

**Every value a tool returns is already in words with its unit in the key.** A
model handed `{"sfha_feet": -1}` writes that the house is minus one foot from a
flood zone. Distances are -1 when nothing was found, so `feetOrNone` is what turns
that into "none found within the search radius" before the model ever sees it.

**Four loan programmes, and they are not variations on one loan.** FHA charges an
upfront premium and then an annual one that usually never comes off, VA charges a
funding fee and no insurance at all, USDA charges both but less, and conventional
charges neither once there is twenty percent equity. Comparing note rates alone
gets the ranking wrong most weeks, which is why every quote is the all-in monthly.
Every government programme finances its upfront fee, so it lands on the loan and
not on the cash to close.

**The rate is Freddie Mac's weekly survey and nothing else is free.** Every lender
rate table is behind a lead form and every aggregator wants a key. PMMS is a
conventional conforming survey rate, so the other three are a spread off it, and
the report says so rather than passing it off as a quote.

**USDA's map publishes the ineligible areas, not the eligible ones.** A point that
matches no polygon is a point they will lend on. Reading it the other way round
calls the whole county ineligible, which is why the test pins a town and a rural
address on both sides of it.

**A programme that was not checked and one that failed must not read alike.**
Blockers are what is known to be wrong and checks are what nobody said, and they
are separate lists for the same reason a failed lookup never moves a score.

**The FBI key is the one thing here that is not keyless.** `FBI_API_KEY` in `.env`,
free and instant at api.data.gov, and without it every crime line says it has no
figures rather than guessing.

**Everything personal is in `data/property.json`,** gitignored, with `property.example.json` committed carrying placeholders. A work address, a
budget and a child's school run are not facts to publish, and a missing config is
not fatal: the reports run on defaults and say which config they used.

**The Census Bureau's own API stopped answering without a key,** so the figures
come from the Esri Living Atlas ACS layers instead, four of them, because no single
layer carries age, income, tenure and composition together. Layer 1 is the county
layer on all four and they share the NAME and State columns, so one where clause
queries all of them.

**Race and religion are shown and never scored.** Nothing here ranks a house at
all, so there is no score for them to move, and the published figures are on the page because they are public and were asked for.

**A gap without a reason is worth nothing.** `Resting` names which upstreams have
asked us to slow down and until when, because "no schools found nearby" and "the
schools server is refusing us this hour" are not the same fact.

**A model decides when to call this.** A turn here gets six tool rounds and a
nudge to keep going, so one question can become six addresses, and a model that
misreads a street name will spell it six ways. One cold address costs about forty
requests, measured: ten routes, five and four to the two county GIS boxes, four
census layers, and one or two of everything else. So `spend.go` caps cold lookups
at **six an hour and twenty five a day**, in the database rather than in memory,
because a restart handing back a fresh allowance is how a limit becomes a
suggestion and a deploy is a restart. An address already in the cache costs one
read and is not counted, so a conversation about one house never runs it down.

**Every endpoint budget is sized for a person asking now and then,** against the
per address cost: 150 for OSRM, 80 for a county GIS box, 40 per Overpass mirror,
60 for the rest, and `GuardOpts` defaults to 60 with a 1.5 second gap so an endpoint somebody adds without thinking gets the
cautious numbers rather than the convenient ones. A test asserts all of it,
because a budget that drifts up is exactly the change nobody notices.

**An assessed value is only worth comparing to a price when the record is for that
house.** The geocoder puts the point in the road so the nearest parcel is
sometimes next door, and a tax roll carrying land and no buildings is a vacant lot
record against a house standing on it. Both produce a percentage precise enough
for a model to repeat as a finding, so `parcelCaveat` leaves the figure out
entirely and says what is wrong with the record instead, and a parcel next door
takes its acreage with it. Handed the number with the caveat beside it, a
model leads with the number.
