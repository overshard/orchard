# dash.bythewood.me

Working detail for this site. The repo-wide rules are in `CLAUDE.md` at the root, and the look is in `DESIGN.md`.

Markets, Hacker News, Lobsters, the weather and a health strip for every other
site, all on one page that updates itself. It is public and has no login, which
is the constraint everything below follows from.

**Yahoo's `v7/finance/quote` is gone.** It answers 401 Unauthorized to anything
that has not carried a cookie and a crumb through their handshake, and it is the
endpoint every tutorial still points at. `v7/finance/spark` and
`v8/finance/chart` both still answer with no session at all, and spark takes the
whole symbol list in one request and returns the same meta block plus the
intraday closes, so the entire markets panel costs one call per poll. It needs a
browser-like User-Agent or the response is a block page, and Yahoo's edge sends
`cache-control: max-age=10`, so polling faster than that returns bytes you
already have.

**Hacker News comes from Algolia, not from the official API.** The Firebase API
hands back 500 bare story ids and charges one request per story to resolve each
one. `hn.algolia.com/api/v1/search?tags=front_page` returns all thirty with
titles, scores and comment counts in a single response.

**Futures replace the cash indexes outside the session**, driven by a New York
clock in `market.go` rather than by a holiday calendar. There is no calendar,
and the case it would catch is caught instead by the age of the S&P's own
quote: a cash index that has not printed in half an hour during what the clock
calls regular hours means the clock is wrong.

**The health strip asks the bridge first and the public hostname only as a
fallback.** Cloudflare will serve a cached 200 for `/healthz` long after the
origin behind it has stopped answering, and two of these sites do exactly that
right now, so a public probe is not evidence a site is up. A row built from the
fallback says `cached` rather than `up`.

**What logging hands over is counts and nothing else.** `/aggregate` returns a
record, error and 5xx count per source plus the watchdog's up flag, never a
message, a path, a status code or an address, because dash publishes it to the
internet. Caddy refuses the path on the public hostname the same way it refuses
`/ingest`, and a test on each side asserts the field list rather than trusting
the handler to stay honest.

**Everything is fetched once and pushed to every browser.** One poller per
source writes into a store and the store broadcasts the whole state as JSON over
`/events`, so ten open tabs still cost one request upstream. The markets poll
drops from 30 seconds to 5 minutes when nobody is connected, which keeps the
page warm for the first visitor without polling Yahoo all night for no one.

**`web/server.go` here sets `WriteTimeout: 0`,** the third version of that file
in this repo. Go's write bound covers the whole response, so any value at all is
a ceiling on how long a stream may stay open. An idle stream is held up by a
comment frame every 25 seconds instead, which is inside Cloudflare's 100 second
idle drop.

**Caddy's `encode` takes a matcher in this site's block** rather than the
blanket one in `(site)`, because a compressed `text/event-stream` buffers. Both
streams are in it, `/events` and `/api/quick/send`.

**The quick chat and the BRIEFS rerun are the only things on dash that know who
is looking.** The home page can come out of Cloudflare's cache, so it is the
same for everybody and `GET /api/quick` and `GET /api/rebuild` answer `no-store`
whether the `bw_session` cookie is live. `POST /api/quick/send` and `POST
/api/rebuild` check the session themselves, refuse anything but JSON, and refuse
a `Sec-Fetch-Site` other than `same-origin` because another bythewood.me host is
same site and carries the cookie. The quick chat relays to chat over the bridge
with that cookie and nothing else of the caller's, so chat needs no CORS.

**Every card on the strip is drawn against one New York trading day.** It runs
9:30 to 16:00 and rolls at the open rather than at midnight, weekends included,
so gold and crude and bitcoin reset in the morning with the exchanges instead of
whenever Yahoo thinks their day starts. Bars before the open are dropped and
whatever prints after the close stretches the window, and the eight cards share
one window so the right edge of one means the same hour as the right edge of the
next.

**A card whose market shut gets its dead stretch marked.** The VIX stops at the
bell and the seven cards beside it keep trading, so it ends its line about 60%
across an evening card, and an empty right hand third on one of eight reads as a
broken card rather than a closed market. It takes a faint dashed rule at the last
print and the amber cursor is dropped, since on every other card that cursor means
here is now and it would be saying the opposite thing in the same place. The space
right of the rule is left alone, because the dotted baseline already runs the width
of the card and a filled block there shouted over everything else on the page. A
card only counts as shut once it is more than half an hour behind the latest bar
anywhere on the strip, because the futures normally trail bitcoin by about ten
minutes without either having stopped.

**The previous close comes off the bars for anything that trades around the
clock.** Yahoo dates bitcoin's day by UTC and a future's by its contract, so
their own `chartPreviousClose` measures from a different moment than the S&P's
and the eight cards disagree about what day it is. The four cash indexes and the
VIX keep Yahoo's number, since theirs is already the 4pm one and it is what every
other site quotes.

**The strip goes out as two requests, at two ranges.** The cash indexes only need
the day they are in, and everything else needs enough history to find 4pm
yesterday, which over a weekend is three days back. `1d` and `5d`, and it is
still two batches because the split lands under the ten symbol limit either way.

**The earnings panel is the top hundred of the S&P 500, and that takes two
sources.** Nasdaq's screener answers every US listing sorted by market cap in one
call, and `sp500.go` holds the index membership, because a cap floor on its own
fills the panel with ASML, TSM and ARM. The hundredth name is worth about $120B
right now, so the old $10B floor was twelve times too loose and the panel was
showing companies nobody has heard of.

**Nasdaq drops the time of day once a date is in the past.** An upcoming row says
`time-pre-market` or `time-after-hours` and the same row a week later says
`time-not-supplied`, so which of the two sessions around a report carried it is
not knowable from the calendar. The reaction takes the one that moved, which is
right for both cases and does not matter when neither moved. The presence of an
actual EPS is also what sorts a row into reported or upcoming, rather than
comparing its date to today, so a company that reported before the bell this
morning reads correctly.

**Nothing free publishes guidance.** A beat the market sold is the closest this
gets, so a row where the result and the move disagree by more than 1.5% is
tagged `SOLD THE BEAT` or `BOUGHT THE MISS` and the panel says nothing else about
the outlook.

**A notice is named by the thing and never by when it was seen.** Every frame
carries `notices`, each poller rebuilding its own kind, and the browser remembers
which ids it has shown and counts the ones on the page at load as seen. A market
move is the close it's measured from and its direction, a warning is its VTEC event
number rather than the NWS id that changes with every update, and a major story is
its lead's URL, which is what keeps a restart or a reconnect from notifying twice.
Market notices are the glance's act bars, `marketBars` in `glance.go`, read off
the live cards on every poll, since the glance's own chip holds at its slot while
futures move. Watch is never pushed, because a 1% S&P day or a VIX of 22 is an
ordinary week. The S&P and the Nasdaq share one notice a session per direction,
evening futures count as the day they still carry, and the VIX only notifies on
the session it crosses 30, or a week above it would be a notice every morning.
News only notifies when TODAY's read is major, seven in ten outlets on one story,
and the rater gave that story a 4 or 5, since a story can be everywhere and
change nothing. The notice is the outlet's own headline and link, and a lead that
changes outlet between slots can notify a second time.

**The briefs are the one thing on dash a model writes.** At 7:00, 9:00, 11:00
and 16:05 New York time `briefnews.go` and `briefmarkets.go` read sixteen news feeds
and seven market feeds, `briefstories.go` groups the same event across outlets
and ranks the groups by how many outlets carried them, and the two desks ask
llm.bythewood.me for a markets line and a TODAY summary under a JSON schema,
on the one model load `brief.go` schedules. The model cites events by number and Go turns those into
links, so it cannot make up a URL, and a news point it could not cite is dropped.
Each feed carries its AllSides lean, which is counted per point (`L1 C3 R1`) and
never shown to the model, and a story only one side's outlets ran is marked as
that rather than left out. Common words of the day are dropped before grouping,
or two stories about the administration share four words and become one. The
model rates every event in its own calls first, twenty at a time with each
rating carrying the event's number, since a single list of sixty drifted a place
partway down. A rating is a topic and an impact from 1 to 5 on an ordinary
American's money and life. Crime, trials, one-off incidents, human interest,
sport, celebrity and local stories are dropped in Go unless seven or more
outlets ran them, because told to skip a trial in the summary prompt it writes
one up anyway and calls it politics, and so is anything rated 1. A minor label
rated 4 or 5 is kept anyway, since the rater files futures falling after an
attack under incident. Go keeps the
ten highest rated and the writer only rewrites them as headlines, so the
order on the page is the impact order. The markets line is always the last
session, today and the next, and on a weekend it's the week just gone and a lean
on the week ahead, scored against that week's move. A session that already happened gets its up or down
from the daily closes in Go, and when its line cites no story Go writes the line
too, as the move and "no story gave a reason", since uncited it blamed one day
on the next day's news. The forward lines lean on the Fed's meeting
dates (copied into `calendar.go`, add next year's in December), the BLS and BEA
release calendars and two documented patterns, the turn of the month and the
drift into a Fed decision. Above TODAY's headlines sits a read of the day, its
level counted in Go from the share of outlets on the top story (seven in ten is
major, four in ten or under is quiet) and one headline per most carried story
from the model, each its own schema field with a `maxLength`, because a word
limit in a prompt is ignored and handed a longer list it skipped the second
most carried story for whatever read best. The model's cut lands a few
characters short of a `maxLength`, so `whole` judges a long line by whether it
ends on a full stop after an ordinary word, and drops it if not. Hacker News and
Lobsters each get a pulse level computed in `pulse.go` on every poll, since a
thread takes off inside an hour. Blowing up means fresh and already high, a huge
thread from yesterday is only a big thread, and Lobsters runs on about a tenth of
Hacker News' numbers. Under it sits a third desk, `feeds.go`, which at each slot
reads both front pages plus the top three comments on the five busiest threads
(Firebase for HN, since it lists replies in the site's rank order and Algolia
doesn't) and writes a read of about thirty words on
what the page as a whole is about and what the commenters make of it. A fourth
desk, `glance.go`, runs last and reads everything on the page into AT A GLANCE,
a sentence or two and one line each for markets, news, tech, weather and play.
Each line's level, quiet, watch or act, is a threshold in Go and the model only
phrases it, a figure in its text that the facts don't carry swaps the line for
one Go writes, and bold only lands on a phrase the facts carry. WEATHER
watches an inch of rain, or half an inch over ten hours, or 30mph gusts today or
tomorrow even with no NWS alert out, and PLAY watches a game out under a month in
Steam's top three or a No. 1 that isn't the usual one, which `steam.json` on the
volume keeps by day since Steam publishes no chart history. A story every
outlet runs doesn't raise the level, since being everywhere isn't the same as
touching him. The 9B
writes right up to whatever `maxLength` it gets, so the cap sits well past the
asked length and `whole` keeps a cut line up to its last whole clause. BLS answers 403 to any User-Agent without a contact
address in it, browsers included. The latest brief per desk lives
in `briefs.json` on the volume, so a deploy shows them rather than nothing, and
a missed slot runs on start. Every brief is also appended to `briefs.jsonl`, and
every forward lean to `leans.json`, where it's scored against the S&P's close
the day after its session and logged as `brief lean scored` with the running
record, so how often the leans are right is a query and not a guess. Every
slot before a session makes a call on it and only the last one counts, and a
forward line is kept as it was while the S&P's price hasn't moved, since asked
again over a weekend with the same prices the 9B flips its lean. The 9:00 slot
is there because CPI, PPI and the other 8:30 releases are out by then and it
still lands before the open, so its TODAY is a lean like 7:00's and the calendar
tells it which releases already came out. `LLM_KEY` in `.env` from `make dash-key`, and
without it the panels say the briefs are off.

**A brief never loads a model onto a card the desktop is using.** Each slot asks
the gateway's `GET /v1/gpu` first, which reads `nvidia-smi` through the
`orchard-llm-gpu` sidecar, and a busy card, 55% or 3000MiB with none of our
models on it, defers the slot an hour and says so on the panels. Weekends run
too, pointed at the week ahead. Every desk shares one load, and the weights come off with
`/v1/unload` straight after, unless a model was already resident, which is
somebody's chat and is left to its own idle timer. The sidecar exists because the
gateway is a scratch image and the injected `nvidia-smi` needs glibc.

**BRIEFS in the rail reruns every desk now, signed in only.** The POST queues a
run for the briefer's own loop, so it never overlaps a slot, and a slot that
comes due while one is queued leaves it to that run. It skips the GPU gate,
since the person asking is the one at the desktop, but still unloads what it
loaded. It's refused while any run is going and for ten minutes after the last
forced one finished, since a run spends most of an hour's article and comment
budget. A forced brief's `slot` is the time it ran, so the slot it beat isn't
owed again, and its lean is just the latest call on that session, which is the
one `record()` counts. Before 7am it's framed as a morning.

**The on air banner reads YouTube first and Twitch only when YouTube has
nothing.** YouTube is a scrape of `@theburntpeanut/live`, which is the watch page
of the stream when he's live and carries `"isLiveNow":true`, and canonicals to
the channel when he isn't. A page that is neither counts as a failure, so a
consent wall or a redesign shows up on the UPLINK row instead of quietly hiding
the banner. Twitch is its GraphQL endpoint with the web player's public
Client-ID, since Helix wants a registered app. Offline only needs one of the two
to have answered, or a dead source would hold the banner up forever. A stream
that drops and comes back inside half an hour keeps its notice id, whichever
platform it comes back on, so a reconnect doesn't notify twice.

**Every feed row carries its top comments and a read of them.** The news poll
fetches the top three top level comments for each HN and Lobsters row into a
cache keyed by discussion URL and only looks again after 45 minutes, since every
HN comment is its own Firebase request. The feeds desk fetches eight for each of
the ten rows and writes one or two sentences per thread on what most of the
commenters think and where they split, kept on the brief as `threads` and copied
onto the rows by URL with the time it was read. A thread that turned up after
the last slot has comments and no read. A read that doesn't end on a full stop
was cut at the cap and gets one retry, since `whole` would keep "while others."

**The cards' TL;DRs come off the article pages, read at each slot.** The feeds
desk fetches each row's link and the news desk every outlet a TODAY point links
to, and `article.go` pulls the text out with regexps since dash has no
dependencies: the paragraphs in `<article>` or else the page, with script, nav,
header, footer, aside, form and figure cut first, and the meta description when
that leaves under 400 characters. Google News links only redirect with script
and are skipped, so AP and Reuters fall back to their feed description. Every
site shares one `articles` budget and never trips the breaker, since one
paywall's 403 says nothing about the next site. About four in five HN and
Lobsters links give a TL;DR.

**Nasdaq only fills in an evening report once its date has passed,** so an after
hours print is heard about the next morning at the earliest. A quick check asks
for today and the weekday before every quarter hour from 6am to 8pm on weekdays,
and runs the full earnings poll only when it finds a print the last one did not
have, or one whose figures changed, since a full one is up to twenty requests.
Each new or revised print is logged as `earnings reported` or `earnings revised`
with Nasdaq's figures, because the panel only holds the latest version and a row
that read MISS in the morning and BEAT the next day left nothing to look at.

**`range=1d` for BTC-USD is the UTC day and not the last 24 hours.** It rolls at
8pm New York, so a poll at 8:05pm came back with six bars and drew a straight
line across the card. That is what `carrySparks` was written for, and the wider
range fixes it at the source, but it stays as a guard against Yahoo
having a moment. It keeps the previous shape when a poll returns fewer than five
points and the last one had more, within one symbol and one trading day so a card
never shows yesterday's chart or the other instrument's. Only the shape is held
back, the price and the percent are always fresh.
