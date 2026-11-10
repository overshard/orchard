# pond.bythewood.me

A virtual pet game where everybody who opens it gets a frog of their own. The
frog is drawn from a seed as pixel art, grows up over about a month on the
server's clock and then stays for good. You earn pebbles diving, spinning a
wheel and turning over a log, and spend them on hat boxes. There are no
accounts, a frog is kept by six words in a cookie called its pond key.

## Requirements

Go and bun. It needs one secret, `POND_PEPPER`, in a `.env` beside the compose
file, and `.env.example` says how to make one. Pond keys are stored hashed
under it, so losing it or changing it locks every frog out for good.

## Running it

```sh
make run      vite watch + go run, on :8000
make build    release binary into ../../bin/pond.bythewood.me
make clean    delete build/ and node_modules
```

From the repo root `make deploy SITE=pond.bythewood.me` builds the image and
replaces `orchard-pond`. A dev run keeps its database in `data/pond.db`, uses a
fixed pepper, and has a dev clock on the pond page for jumping hours and days
ahead, with made up days beside it since a dev run can't reach dash. A release build has neither the fixed pepper nor the clock.

## Layout

- `main.go` wires the routes and the pages, `api.go` is the JSON handlers.
- `store.go` opens SQLite and loads and saves a pond, `moves.go` is each thing
  a visitor can do to one, and `game.go` is the rules and the odds.
- `breed.go` is the egg bench, the reeds and pads. A frog off the bench keeps
  how it differs from its seed as JSON in `frogs.genome_json`.
- `genome.go` turns a seed into a frog's name, look and rarity, and
  `frontend/src/frog.js` is the same thing in JavaScript plus the drawing.
- `key.go` and `words.txt` are the pond key.
- `day.go` reads dash's `/api/state` over the bridge every ten minutes and
  turns the weather, the market, the VIX, Hacker News, the news and the health
  strip into what's on the water. It's the same day in every pond, and with
  dash down or a reading over three hours old it's an ordinary day.
- `share.go` is a frog's public page at `/f/<name>-<code>`, its power, and the
  PNG card a link unfurls with, drawn by `sprite.go` in the JetBrains Mono under
  `fonts/`. The code is the pond's public id and can't open the pond.
- `frontend/src/` has one file a view, `dive.js`, `wheel.js`, `hats.js` and
  `reeds.js`, with `index.js` for the pond itself, `share.js` for a frog's
  public page and `key.js` and `k.js` for the key pages.
- `web/` is this site's own copy of the HTTP layer every site here shares.

`GET /api/panel` answers with the visitor's frog as JSON, or a house frog when
they have none, for other sites to draw.

## Things that will bite you

- `go run .` on its own exits at startup, because there's no Vite manifest
  until the frontend has built once. `make run` waits for it.
- `genome.go` and `frog.js` have to make the same random draws in the same
  order, and `sprite.go` has to draw the same cells as `frog.js`. After
  changing any of them run `bun fixture.js` in `frontend/` and then `go test`,
  which checks the Go side against what that wrote.
- `words.txt` can never lose or reorder a word once a key is out, since a key
  is six words off it.
- The server owns the clock and every earner has a daily cap in `game.go`. A
  test plays 90 days and fails if the purse runs away, so run it after touching
  a price or a prize.
- The ghosts in Dive are dealt by `POST /api/dive/start`, which is how the
  server knows a podium place at the finish. The page only plays them out.
- A new column goes in `addedColumns` in `store.go` as well as the schema,
  since the live tables already exist.
- A bred frog's seed only names it and gives it a voice and a pupil. Anything
  that draws or rates a frog has to apply its mods, `genomeOf` and `lookFor`
  here and `bred` in `frog.js`.
- The egg is laid by `POST /api/greet` from the page's script and not by the
  page load, so crawlers don't fill the database with eggs.
- `/key` and `/k` show or carry a pond key, so they load no analytics and send
  no referrer. Keep it that way.
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
