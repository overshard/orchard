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
under it, so losing it or changing it locks every frog out for good. An
`LLM_KEY` for the nightly lines is optional, `make pond-key` at the root mints
one.

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
- `shop.go` is Little Tully's hats, Big Bean's larder, Moth's curios and Tansy's
  furniture, which the page draws on the water. Stock is
  dealt from the day so everybody sees the same shelf, and `bought` counts
  what each pond took off it.
- `names.go` is the list a frog's name is rolled off. Nobody types a name, so
  renaming only takes one that's on it.
- `wizard.go` turns three of a thing into one thing of any sort, mostly a tier
  down, and `stall.go` is the pond's own stall, where a thing is up for a day
  and its hourly chance of selling falls with the price over that day's rate.
  Both work on anything in the bag, a hat as
  `hat_<shape>_<colour>` and everything else by its item kind, and both keep
  the last hat or trinket back for the collection.
- `genome.go` turns a seed into a frog's name, look and rarity, and
  `frontend/src/frog.js` is the same thing in JavaScript plus the drawing.
- `arcade.go` is the side games, fishing, tongue flick, crossing, raft run,
  lily leap, tadpoles, lily stack, the shell game and pebble skip, which pay
  into the purse like Dive with a cap a day each. A score over a game's `most`
  pays nothing, so keep it well past what a person could reach. One game a
  day, off a hash of the date, is the disk of the day and pays in full a
  fourth time under the same cap.
  `frontend/src/arcade.js` is the frame round them, one file per game beside it.
  It reads a game's `W` and `H` each time, since Crossing changes its `H` to
  fit the window between runs.
  `frontend/src/feel.js` is how a game feels, one made with `feel()` and set
  as the game's `feel`. `got` is a catch and `crash` a bonk, with the blip, the
  freeze, the bits and the popup in one call, `close` is a near miss,
  `squashed` draws the frog squashed, `buffer` and `take` keep an early press
  for 90ms, and the game calls `draw` where its popups belong. A game's
  `demo()` gives the ghost finger its first input.
- `neighbours.go` is the residents, friendship by dishes and a favour a day.
- `burrow.go` is the bank's burrows. Each level adds a pad that doesn't count
  toward the ones you buy, and a friendly neighbour can lodge in one and pay rent.
- `market.go` is the pond market, a business for each of dash's market
  series. A share moves three times the real day's percent in pebbles, the
  day's last reading in the regular session is kept as its close in
  `market_days`, and a pond's shares are in `shares` at what it paid.
- `todo.go` is what's waiting right now and `goals.go` is what the pond has
  never tried, both sent with the view. A new pond's goals are the four first
  day steps in `starter.go`, and after their prize it's the first few down a
  list. `ponds.tried` has a bit for a thing that leaves no trace once done.
- `opening.go` is how a new pond opens up, Works a day in and the market
  three days after that, sent with the view as `open`. The page hides the keys
  for a closed place and the gazette carries a notice the day one opens.
- `key.go` and `words.txt` are the pond key.
- `day.go` reads dash's `/api/state` over the bridge every ten minutes and
  turns the weather, the market, the VIX, Hacker News, the news and the health
  strip into what's on the water. It's the same day in every pond, and with
  dash down or a reading over three hours old it's an ordinary day.
- `mood.go` boils that day down to one mood, which tints the water and picks
  the line after the greeting, plus what the frog is watching or playing, Old
  Clover's forecast for tomorrow and the fortune lily. No number from dash
  ever reaches one of those lines, and a test holds it.
- `tales.go` is the frog's dream on the first visit of a day, each
  neighbour's gossip and the Pond Gazette's headlines, all off hand written
  pools. `nightly.go` asks the model gateway once a night for more lines for
  every mood and kind, checks each one in Go and keeps the night in
  `tales-<day>.json` beside the database. The hand written lines are always
  in the pool, so with no `LLM_KEY` or a failed night nothing is missing.
- `folk.go` gives each resident a personality, what it's doing and thinking
  in each part of the day and what it says to you, coloured by the weather,
  the mood and whether today's gazette names it. The nightly run writes a set
  of lines for each resident in its own voice on top of the hand written ones.
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
  Reeds, Garden and Bank are still views of their own, lit under the Works
  key, and `scenes.js` draws the picture over them and the smaller ones.
- `icon.go` is the web app manifest and the home screen icons, which are
  drawn in Go like the share card since there are no image files.
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
- dash, status, blog and isaacbythewood.com draw frogs with copies of `frog.js`, so a
  change to it is copied to all four and all five deployed. A new site that
  reads `/api/panel` has to be added to `panelOrigins` in `api.go`. `make check` fails on a stale copy.
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
- Big Bean's prices have to stay above what a dish sells for, sale or not, or
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
- The nightly run only goes between 22:30 and 6:00 pond time, skips while the
  gateway says the card is busy or under 3GB is free, and never runs off a
  visitor. A model line with a digit, a banned word or a slot it wasn't given
  is dropped, so add a slot to `taleAsk.allowed` before a pool can use it.
- There's no service worker, so an installed pond needs the network like the
  page does. On an iPhone it also keeps its own cookies apart from Safari's,
  so it opens on a new egg until the pond key is brought over.
- The frame skips a game's `step` while its feel is frozen, and a hidden page
  stops the frame, so a game keeps all its time in `step` and never in a
  timer of its own.
- The tap that starts a run is handed to the game's `drag` and not to its
  `press`, so a held finger steers from the first frame and a tap game doesn't
  act on it.
- A change that lets a game score faster needs its `rate` in `arcade.go`
  checked against a bot, since a run past `rate` points a second pays nothing.
- `index.css` is one flat file and the share page loads it too, so look for a
  class name in it before using one. `place` is already Dive's standings and
  `who` and `scene` are on the share page as well as the pond.
- A closed place is only hidden, every move behind it still works. Anything
  that links somewhere, a todo row or a line in the gazette, has to ask
  `Open.has` first, and only `create` sets `staged`, so an older pond is open.
- The page keeps what's open in localStorage as `pond-open` so the keys are
  right before the pond loads. With nothing kept it starts on four keys.
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
