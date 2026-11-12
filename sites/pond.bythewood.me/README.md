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
ahead, with made up days beside it since a dev run can't reach dash, a full moon and frogspawn season among them. A release build has neither the fixed pepper nor the clock.

## Layout

- `main.go` wires the routes and the pages, `api.go` is the JSON handlers.
- `store.go` opens SQLite and loads and saves a pond, `moves.go` is each thing
  a visitor can do to one, and `game.go` is the rules and the odds.
- `breed.go` is the egg bench, the reeds and pads. A frog off the bench keeps
  how it differs from its seed as JSON in `frogs.genome_json`.
- `jobs.go` is frog jobs. Every frog past the egg works one target of one
  job, a list opened by level, as a run of actions on the server's clock.
  Each finished action puts its thing in the frog's basket and pays xp for
  it, and a basket holds twelve hours of actions before the frog stops. Each
  frog has a level from 1 to 99 in every job in `job_xp`, on RuneScape's xp
  table. Every job opens its targets on the same levels, and what an action
  pays comes off `xpLadder` by the level its target opens at. `mastery` keeps
  each frog's actions at each target, and its level, held under a cap the
  job level sets, makes that target quicker, surer and likelier to bring
  two. Crafting is a job whose targets are recipes, one thing made from
  sticks, reeds and clay an action. A frog fed a dish is full for some hours
  by its tier and has a chance of bringing in two while it is. A 99 earns
  the pond that job's hood, a hat with shape 101 and up that's worn but
  never held, and each job has a pet trinket that only work turns up.
- `tools.go` is a tool for each job in three tiers, sold on Moth's, Big
  Bean's and Tansy's shelves and kept in `tools` as the pond's best tier.
  A frog takes the best one its level allows up at the start of a basket,
  into `frogs.work_tool`, so buying one never changes a basket on the go.
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
  day and kept in `keepsakes` with the date. They're never spent. The last
  four are for 7, 30, 100 and 365 days at the pond, counted in
  `ponds.days_visited` once a New York day at the greeting.
- `shop.go` is Little Tully's hats, Big Bean's larder, Moth's curios and Tansy's
  furniture, which the page draws on the water. Stock is
  dealt from the day so everybody sees the same shelf, and `bought` counts
  what each pond took off it.
- `want.go` is the one hat a pond can ask Little Tully to look out for. What
  he pays for spares adds up in `ponds.want_fill`, and once that covers the
  hat's bar it's on his shelf for that pond alone, in slot 9, at its tier's
  price. It's the only way a legendary is sold.
- `marks.go` works out which things in the bag somebody is after, and
  sends a line for each on the thing as `wanted`. Each source is a func on
  `markers` that reads the finished view, so a new one is one more func.
- `sets.go` is the sets, four things each from across the collections, read
  off the found tables so a thing cooked or sold since still counts. A
  finished one is kept in `sets` with the day and puts a piece of scenery on
  the water, which `frontend/src/scenery.js` draws and places.
- `badges.go` is the badges, small goals worked out from the view a move
  already built, kept in `badges` with the day. `frontend/src/pins.js` is
  each one's pin. `land` in `sets.go` keeps both after every move and sends
  what's new as `landed`, which `collect.js` pops up.
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
  lily leap, tadpoles, lily stack, the shell game, pebble skip, pebble
  drop, chorus, pad putt, the spawn jar, tongue lash and swarm season, which pay into the purse like Dive with a cap a day each. A score over a game's `most`
  pays nothing, so keep it well past what a person could reach. One game a
  day, off a hash of the date, is the disk of the day and pays in full a
  fourth time under the same cap. Eight hours after the last run of any game
  the pond is well rested and the next run pays a quarter more, still under
  that game's cap, with `ponds.played_at` keeping when.
  `frontend/src/arcade.js` is the frame round them, one file per game beside it.
  Pad putt's nine holes and its roll are in `frontend/src/course.js`, which
  never touches the page, so a script can play every hole through with bun.
  The server only bounds its score, so a change to how a hole pays needs
  `puttMost` in `putt.go` kept over the best a round can make.
  `frontend/src/play.js` is the shelves on Play. A disk is one line on its
  shelf there with its colour, its label art and its game, and the page makes
  the disk, the game's view and its place in the tabs from that line.
  It reads a game's `W` and `H` each time, since Crossing changes its `H` to
  fit the window between runs.
  `frontend/src/feel.js` is how a game feels, one made with `feel()` and set
  as the game's `feel`. `got` is a catch and `crash` a bonk, with the blip, the
  freeze, the bits and the popup in one call, `close` is a near miss,
  `squashed` draws the frog squashed, `buffer` and `take` keep an early press
  for 90ms, and the game calls `draw` where its popups belong. A game's
  `demo()` gives the ghost finger its first input.
- `aims.go` is each disk's two goals for the day, dealt off a hash of the
  date from `aimList` so every pond has the same ones. A run sends the counts
  its goals read as `counts`, Dive and bug sweep work theirs out on the
  server, and a goal met for the first time that day is a row in `aims` and
  pays `aimPebbles` with the run, inside the game's cap.
- `race.go` is the daily race, Dive on one course a day against the
  residents. `POST /api/race/start` deals the day's seed, which the page
  turns into the gates, and the board, each resident's gate count off a hash
  of the date with Rush second or better. The first run a day that finishes
  is the pond's place on the board, kept in `race`, and a podium place pays
  out of Dive's cap. Every run after it is practice.
- `bugsweep.go` is bug sweep, a grid of lily pads with wasps under some. The
  day's grid is the same in every pond and pays once, and every go after it
  is a fresh grid that pays nothing. `POST /api/sweep/start` hands over a go
  and `POST /api/sweep/tap` opens one pad of it. The board is never stored,
  the go's taps are kept in `bugsweep` and played back on each one.
  `frontend/src/sweep.js` draws what the server says opened, and makes the
  share card in the page the way the postcard is made.
- `fish.go` is the fish, each with its points, its tier and the days it bites
  on. `POST /api/arcade/start` deals fishing the day's table off the real
  day, so the page never decides what's in the water. The fish that bite any
  time are always in it. `frontend/src/fishes.js` has the same fish with how
  each looks and fights, and a test holds the two lists together.
  The finish says what a round landed by kind. The server checks those
  fish's points fit under the score, rolls a size for each and keeps the
  count, the biggest and the first day in `fish`. A kind that wasn't biting
  is dropped.
  The book is a shelf in the collection and counts in power at the hat rate
  by tier, as `fish_<kind>` wherever found things are kept together.
  Every fish has a pond record off a hash, held by a neighbour and mostly by
  Sedge. A pond holds one when its best is over it, so nothing more is
  stored, and the day it's beaten goes in `records` as `rec_<kind>` for the
  paper and the diary.
  One fish a round can be kept for the kitchen with `POST /api/fish/keep`,
  three a day, and it turns into small fry or a fish fillet. Both are on the
  end of `ingredients` with a `fish` flag, so they never grow, sell at the
  larder, go on the stall or to the wizard.
- `bond.go` is petting. A rub of the frog out front is `POST /api/frogs/pet`,
  which adds to that frog's bond up to a day's worth and never takes any
  away. A heart is a hundred points, and each frog has a favourite spot off
  its seed that counts double. `frontend/src/pet.js` is the rub, the hearts
  and what the frog does at each heart, drawn over and around the sprite.
- `favourite.go` is a frog's favourite dish and hat, settled at the greeting
  once it's a frog from what it ate and wore most, counted in `tastes`, or
  off its seed. They never change after, and its favourite dish keeps it
  full half as long again.
- `diary.go` is the frog's diary, a line a day in `diary` written at the
  greeting about the day before and kept for good. It reads `records`,
  `keepsakes`, `mementos`, `hats.first_day`, `neighbours.heart_day`, `frogs.heart_day`,
  `mastery.first_day` and `top_day` and the day's weather in `skies`, and picks a hand written line by the
  frog's nature that the last week hasn't used. `GET /api/diary` reads it.
- `neighbours.go` is the residents, friendship by dishes and a favour a day.
- `visitor.go` is the frog that comes by once a New York day while the pond
  is shut, dealt at the first greeting of the day and kept in
  `ponds.visitor` as JSON until the next one. It's another pond's frog out
  front when one was open that day or the day before, picked off the public
  ids, and otherwise a resident or a frog off a seed, and it leaves one
  common thing. `ponds.hidden` is the Stay home key, which keeps a pond's
  frog off every other pond. `frontend/src/company.js` draws it.
- `stamps.go` is the six stamps on a frog's share page. A pond with a cookie
  can stamp a page once a New York day and `stampsADay` pages in all, never
  its own, and a page holds `stampsHeld` unseen. The owner's next greeting
  hands them over with each stamper's frog, and the sweep clears a seen
  stamp after two days and any stamp after a month.
- `memento.go` is the one thing each resident gives a best friend, in the
  order of `residents`. They're rows in `mementos` with the day and never
  items, and count in power at half an epic hat each. A neighbour that was
  already a best friend hands its one over at the next greeting.
- `farbank.go` is the far bank, the one thing to buy with no end, kept as a
  count in `ponds.far`. A patch starts at 500 and each is 15% dearer, and it
  does nothing but stand one more piece of scenery on the bank along the top
  of the pond's glass. `frontend/src/farbank.js` draws the thirty pieces and
  places them, and past the thirtieth a sign counts the rest.
- `burrow.go` is the bank's burrows. Each level adds a pad that doesn't count
  toward the ones you buy, and a friendly neighbour can lodge in one and pay rent.
  It also keeps `ponds.paint`, the pieces of four pond paintings, one for
  each visitor and each stamp handed over at a greeting, twelve to a
  painting. The four are drawn in `company.js`.
- `market.go` is the pond market, in three sorts. Shares in the town's
  businesses, each moved by one line of work across every pond, goods that are
  bought into the bag and sold out of it by the lot, and Glowworm Coin. A
  pond's shares and coin are in `shares` at what it paid, each day's open,
  close and dividend a share in `market_days`, and dividends are paid from
  `ponds.div_day` on.
- `stores.go` is Sedge's bait and Marsh's postcard frames, two small shops
  that run through `shop.go` like the others. A bait is used up by the next
  round of fishing and only changes what bites, and each shop moves a share
  through a `shop:` flow on every sale.
- `black.go` is the market's back door, open 4pm to 8:30pm
  pond time. Three deals a window, the same for every pond, priced in coin
  and dealt with what each really is off an HMAC of the pepper. What a pond
  bought is in `black_buys`, and `black.js` draws the hooded frog over any
  frog's cells instead of a sprite of its own.
- `news.go` is the paper's Business story. A line of work whose day before
  was well over or under its week makes the paper, and the same full day of
  flow moves its share through the morning, so the story never leads a move
  nobody could have read.
- `prices.go` is how a price moves. Every ten minute slot of the day is a
  step off an HMAC of the pepper, and what every pond did is counted by the
  hour in `market_flow` by `addFlow` and weighed against the week before.
- `todo.go` is what's waiting right now and `goals.go` is what the pond has
  never tried, both sent with the view. A new pond's goals are the four first
  day steps in `starter.go`, and after their prize it's the first few down a
  list. `ponds.tried` has a bit for a thing that leaves no trace once done.
  A row's `to` is a hash the page opens, a view, a key in town or `market`,
  and `index.js` draws both lists as the Next strip.
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
- `power.go` is frog power, a gauge of everything a pond has done. Every
  part has a weight in `powerWeights` and the pond holds the share of it it
  has found, raised or played, so the whole game comes to 100,000. A game
  counts its best against the median best of the ponds that have played it
  a few times, which the hourly sweep keeps in `refs`, and never against
  less than its `refFloor`. The pond keeps the highest rung it has reached in
  `ponds.rank_top` and what it last saw of its power in `power_seen`, so a
  move can say what went up.
- `share.go` is a frog's public page at `/f/<name>-<code>` and the
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
  `work.js` is a frog's job screen at `#job`, lit under Works too, and the
  card that says what came in. `workNow` in `api.js` counts a basket on
  between fetches off the actions the server dealt ahead and nothing else.
- `frontend/src/power.js` is frog power on the pond, the plate on the
  screen's bezel, its breakdown sheet and the count up when a move says it
  rose. It holds a rise until the pond is in view with no reveal up.
- `frontend/src/postcard.js` is the camera key on the pond's screen. The
  card is drawn in the page off the scene canvas and opened from a blob in a
  new tab, or saved as a file when the browser won't open one.
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
- The race's gates are made in `dive.js` off the server's seed, a fixed
  number of draws a gate and in a band of the same depth on any window, so
  a change to how a gate is drawn there changes every pond's course at once.
  The server only holds a run to the gates the course could have given in
  its time.
- Every garden grows off one clock in the `growth` table, wound every ten
  minutes at the rate the real day gives (rain doubles it, frost stops it). A
  plot is sown at a reading of that clock, so nothing walks every garden.
- A dish's kind is `dish_<a>_<b>` by position in `ingredients`, so a new
  ingredient only ever goes on the end of that list.
- A round of fishing can be played all day, so the fish kept off it are
  capped at three a day and a fish dish only gets a better tier for two
  fish. A test holds what a day's kept fish cook into under half of
  fishing's cap, so run it after naming a fish dish or changing the keeps.
- Big Bean's prices have to stay above what a dish sells for, sale or not, or
  buying and cooking mints pebbles. A test holds it, so run it after touching
  a price.
- Big Bean pays a pond at most `sellRoom` a New York day for dishes, Moth as
  much for trinkets and Little Tully for hats, kept in the `sold` table, since
  jobs bring in things without end and the wizard rolls them into hats. The stall pays
  out the same room a day and what's sold past it waits on the stall, with
  its price brought down by what's been paid. Anything new that buys a
  pond's things pays through `payOut` or work turns straight into pebbles.
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
- Each box has a meter, `ponds.hat_pity` and `trinket_pity`, and ten plain
  boxes in a row make the next a rare or better. That box only lifts a common
  or uncommon to a rare. Rolled among the rares, epics and legendaries it put
  a box's spares over the box's price on the stall, so `boxWorth` in the tests
  counts the meter in and has to stay under 40.
- `want_fill` counts what Little Tully pays for spares whether or not a hat is
  asked for, and buying the hat only takes its bar off it. Nothing else may
  lower it, since the page tells the player the bar is never lost.
- A memento's kind starts `m_` and must never reach `addItem`, since an
  item can go on the stall or to the wizard. `handMemento` is the only thing
  that gives one. A new collection has to be a shelf in `powerInOf` with a
  weight in `powerWeights`, taken off another part's so the weights still
  come to the whole, and in `shelves` in the tests, `bestFinds`, `everHad`,
  `store.fill` and `collect.js`.
- A set or a badge is stored by its kind, so a new one only ever goes on the
  end of its list, and each needs its scenery or its pin on the page. A test
  holds the page to the lists.
- A badge only reads the `tally`, which is counted once off the view. One
  that needs something new adds it to `tallyOf` from what the view holds and
  never runs a query of its own, since the check runs after every move.
- A rub of the frog doesn't go through `act`, so a badge for hearts lands on
  the next move or greeting and not on the rub.
- The far bank's pieces stand between the hearts and the camera key, which
  at 360 wide reach about 100 and 310 across the 352 wide scene. Swimmers
  in the reeds drop below the bank when it's there, and set pieces, the
  floating trinket and the teacup draw over it.
- Scenery is placed by hand in `scenery.js` to clear a grown frog, the
  furniture, the trinket spots and both top corners of the glass, so look at
  the pond with everything out after moving any of it.
- A reveal that comes while another is up waits in a queue in `ui.js`, so
  nothing should hide `#reveal` itself.
- Nothing in the market reads dash. The first eight keys in `assets` once
  followed its markets, and a `market_days` row with no open is a real close,
  which is carried on from and never worked out again. Keys are kept for good
  since holdings are kept by them.
- Each step of a price is worth what it cost on average, so nobody can buy
  ahead of a move, and that's why the coin drifts down in the middle while
  its average holds. A good is held to a band round its base so two of it
  always cost more than any dish made only of goods, and the market pays for
  goods through `payOut` with a room of its own. `TestMarketStrategies`
  plays five ways of trading for 90 days, so run it after touching a fee, the
  yield or a tune.
- A back door deal's outcome must never be in the view before it's bought,
  and `TestDealsAreTheSameForEveryPondAndHidden` reads the JSON for it. Its
  hints only lean, and `TestTheBackDoorCostsInTheLongRun` holds every way of
  reading them under what it paid, so run it after touching `dealOdds`.
- A new thing ponds do that should move a business needs `addFlow` where it
  happens, inside the move so it's counted on the pond's day.
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
- The Badge key is `pond-badge` in localStorage, read by
  `frontend/src/badge.js`, which sets the installed icon's count off the
  Next strip's waiting rows whenever it's drawn. With no push and no service
  worker it only moves while a pond page is open.
- Tap to reel is `pond-tapreel` in localStorage, read by `fish.js`. With it
  on a press toggles the reel and a release does nothing, so anything that
  lifts the finger for the player, like a pause, leaves the reel as it was.
- A change that lets a game score faster needs its `rate` in `arcade.go`
  checked against a bot, since a run past `rate` points a second pays nothing.
- Bug sweep's day grid is dealt from the pepper and the date, so it can't be
  worked out from this repository. Nothing sent before a go is over may say
  where a wasp is, and a test reads every answer for it. A tap takes one pad,
  since a batch that stopped part way would say which of its pads was safe.
- A goal's count is held to the score by `aimWorth`, the least points one of
  it can be worth, so a new count needs a line there or it's only held to
  being sane. A goal is kept by its key, so a key never changes.
- A game with no `rate` in `arcade.go` is played out on the server and is
  refused by the plain start and finish. On the page it has `begin` and `end`
  in place of those two calls, and it's never the disk of the day.
- A first tap in bug sweep moves any wasp on it or next to it along the
  seed's shuffle. Moving them anywhere fixed, like the first free pad, would
  tell every player where the spare wasps went.
- Pebble drop's bounces are the page's, so the server only holds a score to
  `dropMost`, five pebbles in the middle pot. The day's peg rows go out on the
  view as that game's `deal`, and a change to the pots or the pebbles a round
  has to change `dropMost` with it.
- Chorus keeps time off the audio clock, which runs with sound off too, and
  falls back to the page's clock where the browser won't start one. It never
  freezes, since a freeze would put the frog off the beat, and its step only
  moves the song on by what the clock gave it, so a hidden page or a long
  frame leaves the song where it was. The frame calls a game's `pause` when
  the page hides, which is where chorus stops the rain. `croakAt` and `rain`
  in `sound.js` are its sounds.
- Chorus's ten songs are in `frontend/src/songs.js`, each frog with a part
  of the groove, the chords turning bar by bar and the croaks following them.
  A browser goes through them all in a shuffled order before one comes back.
  The player's part is heard, counted in and sung twice, grows one note at a
  time to two bars, and a part sung again goes from memory. A miss halves the
  run and nothing ends a song early, it's graded once it's done.
- `chorusMost` is `best` in `songs.js` for the song with the most in it, so a
  change to the songs, the rounds or the points has to change it too. A singer out
  front gets its quarter on the server, after the score is checked against
  `most` and `rate`, and the page is sent the plain score.
- `putt.go` deals pad putt's nine holes off a hash of the date, sent on
  `POST /api/arcade/start` as `putt` with the wind as it is when the run
  starts. Every hole is built so its flag can be reached by hops short enough
  that the windiest gust can't carry them off a pad, and par is the fewest
  hops at the longest reach less that hole's wind, so a frog that reads the
  wind well can now and then beat it. `puttMost` is twelve a hole, the most
  a hole under par pays, and the hops and splashes are the page's.
- The spawn jar's physics are in `frontend/src/jarworld.js`, which never
  touches the page, so a script can drop a whole run through it with bun.
  Each small step solves every touch for its push and friction a few times
  over, starting from the push it ended the last step on, and friction at
  the rim is what spins a ball. A touch slower than a set speed doesn't
  bounce, anything barely moving in a pile is let stop, and a ball is only
  redrawn once it's most of a pixel or a turn from where it was drawn.
  Those keep a resting pile from shivering or creeping, so look at a still
  jar after changing any of the numbers. Two pond kings pop and leave, so a
  run has no end of its own and `jarMost` only stops the impossible, with
  the rate holding the rest.
- `index.css` is one flat file and the share page loads it too, so look for a
  class name in it before using one. `place` is already Dive's standings and
  `who` and `scene` are on the share page as well as the pond.
- A closed place is only hidden, every move behind it still works. Anything
  that links somewhere, a todo row or a line in the gazette, has to ask
  `Open.has` first, and only `create` sets `staged`, so an older pond is open.
- A key's pip is lit off `pond-pips` in localStorage, what was waiting behind
  each key when it was last opened. A new kind of todo that comes round daily
  goes in `DAILY` in `index.js`, or its pip only lights again after the page
  has seen it gone.
- The page keeps what's open in localStorage as `pond-open` so the keys are
  right before the pond loads. With nothing kept it starts on four keys.
- The diary only writes what a table holds for that day. A new kind of line
  needs the day stored where the thing happens, the way `addHat` and
  `befriend` do it with the pond's `today`, and a slot in the test's list.
- The pet route is called as fast as a finger rubs. It has to stay one read
  for a frog that's had its day's worth, with no row added anywhere.
- The frog's screen is a button with `touch-action: pan-y`, and a touch that
  starts on the frog cancels its `touchstart` so the page can't scroll or
  zoom under a stroke. Taps are read off pointer events, since a cancelled
  touch never clicks, and only a click from a key goes through `click`.
  Anything new on the screen goes beside the button in `.frogscreen`, not
  inside it.
- Nothing about a basket is stored but the frog's target, when its unpaid
  actions began (`frogs.work_at`) and how many it was paid for before
  (`work_n`). Each action is dealt off an HMAC of the pepper, the frog, the
  target and that count, so reading the basket and collecting it always
  agree. Anything that changes what an unpaid action brings, a meal or a
  level, has to pay the basket out first, the way `feed` and `setJob` call
  `settle`, and collecting starts the next basket where the action in
  progress began. Mastery is only written when a basket is paid, so it never
  changes one on show. Crafters share the pond's materials in frog order.
- How long a level takes is set by `xpLadder` against each target's minutes
  and misses, for about level 10 the first day, 50 in a month and 99 in four
  to six. `TestTheJobsPace` plays an ordinary and a keen player through every
  job, so run it after touching a target, a recipe or the ladder.
- A crafter's basket is worked out against what the pond holds now, so
  listing or spending its materials makes the basket smaller until more
  come in. Collecting still pays what the page showed at that moment.
- A new game needs a `refFloor` in `power.go`, about what a decent bot
  scores, and its runs counted in `arcade.plays`, or it counts against
  nothing until enough ponds play it. Adding one takes a little off every
  pond's power, since `gameWeight` comes out of the whole.
- Power only reads the view. Badges count in it and the Lily lord badge
  reads it, so `fill` works it out once with the badges kept and again with
  the badges earned. A rung reached is kept, so a game's reference rising
  can lower a pond's total but never its rank, and the page never counts a
  fall.
- Share pages are cached at the edge, so nothing about who's looking can
  be in one. The stamp keys come off `GET /api/stamps` once the page is open,
  which is `no-store` like every answer under `/api/`.
- Another pond's frog only ever leaves the server as a `caller`, its name,
  seed, mods, stage, day and hat. Never its pond's id, public id or key, and
  never at all from a pond that keeps its frog home, so anything new that
  shows one frog to another pond goes through `frontOf`.
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
