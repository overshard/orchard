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
  drop, chorus, pad putt and the spawn jar, which pay into the purse like Dive with a cap a day each. A score over a game's `most`
  pays nothing, so keep it well past what a person could reach. One game a
  day, off a hash of the date, is the disk of the day and pays in full a
  fourth time under the same cap. Eight hours after the last run of any game
  the pond is well rested and the next run pays a quarter more, still under
  that game's cap, with `ponds.played_at` keeping when.
  `frontend/src/arcade.js` is the frame round them, one file per game beside it.
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
- `memento.go` is the one thing each resident gives a best friend, in the
  order of `residents`. They're rows in `mementos` with the day and never
  items, and count in power at half an epic hat each. A neighbour that was
  already a best friend hands its one over at the next greeting.
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
  `work.js` is a frog's job screen at `#job`, lit under Works too, and the
  card that says what came in. `workNow` in `api.js` counts a basket on
  between fetches off the actions the server dealt ahead and nothing else.
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
- Big Bean pays a pond at most `sellRoom` a New York day for dishes and Moth
  as much for trinkets, kept in the `sold` table, since jobs bring in things
  without end. The stall pays
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
- `want_fill` counts every spare sold to Little Tully whether or not a hat is
  asked for, and buying the hat only takes its bar off it. Nothing else may
  lower it, since the page tells the player the bar is never lost.
- A memento's kind starts `m_` and must never reach `addItem`, since an
  item can go on the stall or to the wizard. `handMemento` is the only thing
  that gives one, and a new collection counted in power has to be added in
  `powerOf`, `bestFinds`, `everHad`, `store.fill`, `fullPower` in the tests,
  the share page's rows and `collect.js`.
- A set or a badge is stored by its kind, so a new one only ever goes on the
  end of its list, and each needs its scenery or its pin on the page. A test
  holds the page to the lists.
- A badge only reads the `tally`, which is counted once off the view. One
  that needs something new adds it to `tallyOf` from what the view holds and
  never runs a query of its own, since the check runs after every move.
- A rub of the frog doesn't go through `act`, so a badge for hearts lands on
  the next move or greeting and not on the rub.
- Scenery is placed by hand in `scenery.js` to clear a grown frog, the
  furniture, the trinket spots and both top corners of the glass, so look at
  the pond with everything out after moving any of it.
- A reveal that comes while another is up waits in a queue in `ui.js`, so
  nothing should hide `#reveal` itself.
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
- Tap to reel is `pond-tapreel` in localStorage, read by `fish.js`. With it
  on a press toggles the reel and a release does nothing, so anything that
  lifts the finger for the player, like a pause, leaves the reel as it was.
- A change that lets a game score faster needs its `rate` in `arcade.go`
  checked against a bot, since a run past `rate` points a second pays nothing.
- Bug sweep's day grid is dealt from the pepper and the date, so it can't be
  worked out from this repository. Nothing sent before a go is over may say
  where a wasp is, and a test reads every answer for it. A tap takes one pad,
  since a batch that stopped part way would say which of its pads was safe.
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
- `chorusMost` is every beat of the song right on time, so a change to the
  rounds, the beats a bar or the points has to change it too. A singer out
  front gets its quarter on the server, after the score is checked against
  `most` and `rate`, and the page is sent the plain score.
- `putt.go` deals pad putt's nine holes off a hash of the date, sent on
  `POST /api/arcade/start` as `putt` with the wind as it is when the run
  starts. Every hole is built so its flag can be reached by hops short enough
  that the windiest gust can't carry them off a pad, and par is the fewest
  hops at the longest reach less that hole's wind, so a frog that reads the
  wind well can now and then beat it. `puttMost` is twelve a hole, the most
  a hole under par pays, and the hops and splashes are the page's.
- The spawn jar's circles are the page's own physics in `jar.js`, small
  fixed steps of gravity and then each pair and the walls pushed apart a few
  times over. Only overlap past a small slop is corrected, the floor grips
  anything only nudged along it, and a ball drawn only moves once it's most
  of a pixel from where it was drawn. Those three keep a resting pile from
  shivering or creeping apart, so look at a still jar after changing any of
  the numbers. Elders never merge, so the jar always fills
  and a run always ends.
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
- The frog's screen is a button with `touch-action: pan-y`, so a sideways
  drag is a rub and the click after one is swallowed. Anything new on the
  screen goes beside the button in `.frogscreen`, not inside it.
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
- `web/` has to stay byte identical to every other site's copy, and
  `make check` at the root fails when it isn't.
