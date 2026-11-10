# pond.bythewood.me

A virtual pet game where everybody who opens it gets a frog of their own. The
frog is drawn from a seed as pixel art, grows up over about a month on the
server's clock and then stays for good. You earn pebbles diving, spinning a
wheel and turning over a log, and spend them in town on boxes, hats, food and
furniture, or sell your own things off a stall. There are no accounts, a frog
is kept by six words in a cookie called its pond key.

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
- `jobs.go` is frog jobs. Every frog past the egg works one, the work piles
  up for twelve hours on the server's clock, and each frog has a level from 1
  to 99 in every job in `job_xp`, on RuneScape's xp table. Crafting is a job
  that turns sticks, reeds and clay into trinkets and stamps. A frog fed a
  dish is full for some hours by its tier and works harder while it is. A 99
  earns the pond that job's hood, a hat with shape 101 and up that's worn
  but never held, and each job has a pet trinket that only work turns up.
- `bank.go` is the bank, the gravel bar and the garden plots, and the kitchen
  that turns two things from the garden into a dish. `kitchen.go` has the
  ingredients, what grows in which month and every dish.
- `dyeworks.go` is the dye works in the bank, three rocks boiled down into a
  dye. Each rock leans the colour and `bank.js` works out the same odds to
  show them.
- `trinket.go` is the second collection, trinkets set out in three spots round
  the pad. They're items with a `t_` prefix, and `trinkets_found` remembers
  every one a pond has had. The cookable ones are also on the end of
  `ingredients`.
- `rocks.go` is the rock shelf, and `rocks_found` remembers every rock a pond
  has had since the dye works boils them away.
- `keepsake.go` is the keepsake shelf, one thing for each kind of real day
  (a frost, a full moon, a bad day on the market) handed over on a visit that
  day and kept in `keepsakes` with the date. They're never spent.
- `shop.go` is Wren's hats, Brindle's larder, Old Moss's curios and Fenn's
  furniture, which the page draws on the water. Stock is
  dealt from the day so everybody sees the same shelf, and `bought` counts
  what each pond took off it.
- `names.go` is the list a frog's name is rolled off. Nobody types a name, so
  renaming only takes one that's on it.
- `wizard.go` turns three of a thing into one thing of any sort, mostly a tier
  down, and `stall.go` is the pond's own stall, where neighbours buy what's put
  up sooner or later by price. Both work on anything in the bag, a hat as
  `hat_<shape>_<colour>` and everything else by its item kind, and both keep
  the last hat or trinket back for the collection.
- `genome.go` turns a seed into a frog's name, look and rarity, and
  `frontend/src/frog.js` is the same thing in JavaScript plus the drawing.
- `arcade.go` is the side games, tongue flick, crossing, raft run, lily leap
  and tadpoles, which pay into the purse like Dive with a cap a day each.
  `frontend/src/arcade.js` is the frame round them, one file per game beside it.
- `neighbours.go` is the residents, friendship by dishes and a favour a day.
- `burrow.go` is the bank's burrows. Each level adds a pad that doesn't count
  toward the ones you buy, and a friendly neighbour can lodge in one and pay rent.
- `market.go` is the pond market, a business for each of dash's market
  series. A share moves three times the real day's percent in pebbles, the
  day's last reading in the regular session is kept as its close in
  `market_days`, and a pond's shares are in `shares` at what it paid.
- `key.go` and `words.txt` are the pond key.
- `day.go` reads dash's `/api/state` over the bridge every ten minutes and
  turns the weather, the market, the VIX, Hacker News, the news and the health
  strip into what's on the water. It's the same day in every pond, and with
  dash down or a reading over three hours old it's an ordinary day.
- `mood.go` boils that day down to one mood, which tints the water and picks
  the line after the greeting, plus what the frog is watching or playing, Old
  Clover's forecast for tomorrow and the fortune lily. No number from dash
  ever reaches one of those lines, and a test holds it.
- `share.go` is a frog's public page at `/f/<name>-<code>`, its power, and the
  PNG card a link unfurls with, drawn by `sprite.go` in the JetBrains Mono under
  `fonts/`. The code is the pond's public id and can't open the pond.
- `frontend/src/` has one file a place, `dive.js` (with `race.js` for the
  standings and results laid over it), `wheel.js`, `bag.js`, `collect.js`,
  `reeds.js`, `bank.js` (the garden and kitchen too), `shops.js`, `town.js`
  (the boxes, the stall and the wizard) and `settings.js`, with `index.js` for
  the pond itself and the tabs. `ui.js` is the bottom sheet a thing opens in,
  the reveal a prize pops up in, and the slot every collectable is drawn as.
  `share.js` is a frog's public page and `key.js` and `k.js` the key pages.
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
- dash, status and isaacbythewood.com draw frogs with copies of `frog.js`, so a
  change to it is copied to all three and all four deployed. `make check` fails on a stale copy.
- `words.txt` can never lose or reorder a word once a key is out, since a key
  is six words off it.
- The server owns the clock and every earner has a daily cap in `game.go`. A
  test plays 90 days and fails if the purse runs away, so run it after touching
  a price or a prize.
- The ghosts in Dive are dealt by `POST /api/dive/start`, which is how the
  server knows a podium place at the finish. The page only plays them out.
- Every garden grows off one clock in the `growth` table, wound every ten
  minutes at the rate the real day gives (rain doubles it, frost stops it). A
  plot is sown at a reading of that clock, so nothing walks every garden.
- A dish's kind is `dish_<a>_<b>` by position in `ingredients`, so a new
  ingredient only ever goes on the end of that list.
- Brindle's prices have to stay above what a dish sells for, sale or not, or
  buying and cooking mints pebbles. A test holds it, so run it after touching
  a price.
- A new column goes in `addedColumns` in `store.go` as well as the schema,
  since the live tables already exist.
- A bred frog's seed only names it and gives it a voice and a pupil. Anything
  that draws or rates a frog has to apply its mods, `genomeOf` and `lookFor`
  here and `bred` in `frog.js`.
- The egg is laid by `POST /api/greet` from the page's script and not by the
  page load, so crawlers don't fill the database with eggs.
- `/key` and `/k` show or carry a pond key, so they load no analytics and send
  no referrer. Keep it that way.
- Anything sold to the stall has a going rate in `worthOf`, and it has to sit
  under what any shop asks for the same thing or buying to resell mints
  pebbles. A box's spares are held under the box's price the same way, and the
  wizard has to hand back less worth than the three it took. Tests hold all
  three.
- Outside the regular session dash's market cards are futures, so the pond
  market only writes a close while dash says `regular`, apart from Glowworm
  Coin. Buying and selling both take a fee so a round trip always loses, and
  the 90 day purse test plays the market with a random walk.
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
