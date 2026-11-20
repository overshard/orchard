# DESIGN.md

What every site here looks like and why. Read this before changing anything
visual, and update it when a decision changes.

The tool sites, analytics, auth, cron, llm, logging, repos and status, share one
look through `suite/`, a Go package each of them carries a byte identical copy
of the same way it carries `web/`, and `make check` fails when a copy drifts. It
holds `suite.css`, the Geist fonts, the header, status line and home page
templates, `suite.js` for the home page pins, clickable and keyboard rows, the
crosshair, tooltips, toasts and the jump palette, and the spark drawing, all served by the site itself under `/suite/`. A change to it is made
in one copy and copied to the rest. Each site keeps a small stylesheet of its
own for what only it has, a diff, a log line or a code entry.

dash is the other look, amber and a costume on purpose, since it's glanced at
and not read. pond is a game with a night look of its own in its section below.
blog and the portfolio are separate identities and nothing here applies to them.

## The suite

- **Dark only, on cool charcoal.** Ground `#111315`, top bar `#0c0e10`, sunken
  `#090a0c`, panels `#171a1d` and `#1f2327`, lines `#262a2f`, `#1c1f23` and
  `#363b42`, text `#e9ebee`, muted `#979ca4`. Warm greys went muddy beside sage.
- **One meaning per colour.** Brass `#d9b45a` is an action, the primary button,
  the active tab, the `//` label and the soft tint behind a title's key word.
  Sage `#9fcbb0` means ok and nothing else, orange `#f0904b` warn and red
  `#f06b5b` bad. A colour that meant both click me and fine made the chrome read
  as state.
- **Geist for words, Geist Mono for anything that's code, a label or a
  number.** Headings are Geist 600 at `-0.035em`. Mono goes on caps labels,
  tile and table numbers, chips, code, times, the status line and the STAGED tag.
  No serif anywhere.
- **Square corners and subtle solid hairlines.** No radius on anything inside
  the page and no dashed borders. No background grid either.
- **Two rows of header.** The first is the orchard mark and every tool as a tab,
  this one in `[brackets]` in brass, with the account on the right. The second
  is this tool's own pages and a mono line of what matters right now. Inside a
  repository on repos the second row is that repository's pages.
- **No sidebar.** A working page runs as wide as dash, 1760px, and a home page
  keeps the narrower 1320px measure. The header rows line up with either, and
  the switch is any page with an `o-main` and no hero.
- **Use the space.** At every width the grids fill their rows, with tiles spread
  so the last row isn't short, lists packed in columns instead of a grid with
  holes, and a side column beside a form instead of empty page.
- **A status line along the bottom,** vim style, a brass block with the tool's
  name, then its state, then facts like the time zone on the right. Only facts
  that are true and nothing that pretends to be a key binding.
- **A page opens on a label and a title that says the state,** like `Four
  jobs, one failing.`, with the key words in italic over a soft tint, red for
  bad. Actions sit on the right and the primary one is brass.
- **Tiles carry dash's sparks,** drawn by `suite.Spark` the way dash draws them,
  a 1.25px line, a soft fill and a dotted rule at the median, bled to the tile's
  edges. A count that isn't a trend gets `o-ticks` instead, a row of ticks, one
  per check or site or hour, red or amber where it went wrong and as tall as its
  value when it has one. Nearly every tile has one or the other, and one with
  neither keeps the space so a row of tiles lines up. A spark drawn with
  `suite.SparkT` carries its values and a label per point, and in a strip where
  every series is the same length, hovering one moves a rule and a dot across
  all of them and each tile's number reads at that point, written the way the
  tile wrote it or the way its `<b data-f>` sample says. The label goes in a
  `data-xs-when` element beside the strip, or each tile's delta line without one.
- **Trouble sorts first.** A down property, a failing job or a source with
  errors is the top row and tinted red, so nobody has to read the list to find it.
- **A table marked `stack` becomes a list on a phone,** its `lead` cell on a
  line of its own and every number labelled by its cell's `data-l`, since the
  header that said what it was is gone. A row with `data-href` opens anywhere it's clicked, and the
  link inside it stays the real target for keyboards.
- **An empty state says what will show and how to get the first one,** never
  just nothing.
- **Undo instead of are you sure.** A form with `data-undo="Revoked Chrome"`
  strikes its row through and shows a toast with Undo for five seconds, and only
  posts once that runs out. Leaving the page inside the window still sends it,
  with a beacon. Only what can't be put back or ends your own session asks first
  with `data-confirm`, in words that say what it ends, and that one is still
  handled by each site's own script.
- **Toasts** come from an element marked `data-toast` on the page, say after a
  redirect, or from a site's own script dispatching an `o-toast` event on
  `document` with `{text, kind, ms}` in its detail.
- **Anything with `data-tip`** shows its text in the suite tooltip on hover or
  focus, which is how a tick or a day says what happened on it.
- **A table with `data-keys`** moves with j and k, opens a row with enter, which
  is its `data-href` or the `tr.o-drawer` under it, and picks a row's checkbox
  with x. `?` lists the keys and esc clears. `data-bulk="<form id>"` names a form
  that gets a hidden `id` input per picked row (`data-name` on the form changes
  the name) and shows as a bar with the count while anything is picked.
- **Jump or ask** opens on what needs you, then the tools. A leading `>` lists
  only actions, which are any button on the page marked `data-act`, a few built
  in ones and index items of kind `action`. An action item with `post` is posted
  when the palette is on that tool, and opens its `href` from anywhere else.
  Matching is in order letters as well as words, with the matched ones in brass.
- **The home page** is the same header signed out, a hero with the copy on the
  left and a staged window of the real page on the right with numbered pins in
  the margin and a legend under it, then feature rows each with a staged crop,
  run your own, the four column footer and the status line. Staged data uses
  example.com names and nothing real. No stat strip, since the counts aren't
  for advertising.
- **Copy says what is true now.** A home page describes what works today, and
  nothing promises a connector or an API that isn't built.

## Dashboards

The home page is an advert and the pages behind it are instruments. They are
scanned rather than read, so the craft moves from typography to information
design: the summary comes before the detail, and state is encoded in shape as
well as in number.

**The pieces, in the order a page uses them.** The page head, then `o-tiles` or
an `o-strip`, then `o-panel`s, each opening on an `o-ph2` with a title and a
label. `o-hbar` is a ranked row with a bar sized against the largest value, `o-chip` a state,
and `o-tbl` a table. Findings use a chip with a word in it, never colour alone.

**As dense as dash.** A working page runs at 13px with tighter tables, hbars and
panel heads, and a home page keeps 14px. The pieces that carry it:

- `o-strip` is six (`six`) or eight tiles on one 1px hairline grid, so the row
  reads as one instrument, four across on a tablet and two on a phone.
- `o-pm` goes in a panel head, mono key and value pairs like `30d 4 · down 2h`,
  so a panel says its summary before its rows. A `b` in it can be `ok`, `warn`
  or `bad`.
- `o-band` with `b2`, `b2r`, `b3`, `b3e`, `b4`, `b5` or `b6` is a weighted grid
  of panels in place of one wide table and a side column, one column under 900px.
- `o-ds` is fourteen days in a cell, a tick each, `w` or `b` on a bad day and
  `n` on none, so a quiet row still shows its fortnight.
- `o-foot` closes a panel with one caps line, like `5 of 31 countries`.
- `o-hbar` takes a trailing `.d` for its change, `ok`, `warn` or `bad` on the
  bar's `i`, and a `u` inside the bar as a marker at a target or last period.

### Charts

Every Chart.js chart draws from `chart_theme.js`, which is duplicated byte for
byte in analytics, logging and status the same way `shipper.go` is. Its ink is
the suite's and its font is Geist Mono.

**Series colours are punchier than the UI palette.** Chrome can be
muted because nothing depends on telling two borders apart, and a doughnut slice
does.

| Slot | Value | Job |
|---|---|---|
| 1 | `#57b378` | green, and `good` |
| 2 | `#d8a83e` | amber, and `warn` |
| 3 | `#dc6a4b` | red, and `bad` |
| 4 | `#63a9c9` | slate, and `info` |
| other | `#7d7469` | the folded tail, and `muted` |

**That order is measured, not chosen.** Every adjacent pair clears the normal
vision floor, and the worst pair sits at ΔE 6.7 for protanopia, which is inside
the 6 to 8 band that is only acceptable when something other than colour also
separates the series. That something is the legend, which is why every chart
here keeps one. Reordering these means measuring them again.

**Anything past the fourth category folds into `other`** rather than getting a
generated hue. `foldToPalette` does it.

**Status is a separate job from identity.** ERROR, WARN, 5xx and the rest are
keyed by name and never handed out in order, so a level keeps its colour
whatever order the server sent the rows in.

**One axis, ever.** No chart here has two y scales. Two measures of different
size are two charts.

**The map is a sequential ramp,** one hue from light to dark with no second
colour in it.

**A narrow panel puts its legend at the bottom,** since Chart.js silently
truncates labels that don't fit beside a doughnut.

### Direction, on a price or a reading

`dash` draws a line whose colour says which way a number went, which is a
different job from telling four series apart and does not use the palette
above. Two colours and a neutral:

| Slot | dash | Job |
|---|---|---|
| up | `#5ddc82` | gained, or a reading inside its good band |
| down | `#ff5f4d` | lost, or a reading past it |
| flat | `#8a8172` | no move worth colouring |

**Direction colours the move and the trace, never the figure itself.** The price
stays `--text`, because it is the number being read and the move is the
judgement about it.

**The fill under the line is the same hue at about 9% alpha**, which reads as
the line having weight rather than as a second series.

**A dotted rule across the box is what the move is measured from.** Intraday
that is the previous close, and over any longer span there is no rule because
the first bar drawn is the baseline. The suite's sparks put the rule at the
median instead, since a tile's trend has no close.

**An svg stretched to the column width cannot carry a dot.** `preserveAspectRatio`
is `none` on these, so a circle comes out an ellipse and a cursor has to be a
vertical rule. Anything that has to stay round is an HTML element positioned
over the top.

## Components

The values live in `suite.css` and nowhere else, so these are the rules behind
them.

**Button.** Geist 600, square, a 1px `#363b42` edge on nothing for an ordinary
one, solid brass with `#1b1505` text for the primary, and a red edge for one
that deletes. One primary per page head at most.

**Caps labels** are Geist Mono at `0.7rem`, `0.12em` tracking, brass, with `// `
in front. A label inside a crowded row can drop the slashes with `plain`.

**Focus and selection.** `:focus-visible` is a 2px brass outline at 2px offset,
and `::selection` is brass at 30%.

**Fields** sit on the sunken colour with a `#363b42` edge, and the focused one
turns brass with a soft ring. On a phone they're 16px so iOS doesn't zoom.

**Notifications are a switch somebody presses.** A browser only asks for
permission from inside a press, and a public page asking on load is a prompt
nobody wanted, so `dash` has a `NOTIFY` readout in panel 00 beside `LINK` that
asks the first time and opens a menu of what to be told about after that.

## The footer

Signed in, a slim line of copyright, the frog and the source link, then the
status line. Signed out on a home page, four columns in this order with `//`
caps headings.

1. **`// <site>`** with one sentence saying what the site is for. It says
   nothing about how it is built or where it runs, which helps nobody and
   narrows an attacker's guesswork.
2. **`// elsewhere`** with Portfolio, Blog and GitHub.
3. **`// pages`** with the site's own pages, ending on Source.
4. **`// operator`** with Sign in and All tools.

Then a bar with `© <year> <author> · Some rights reserved`, the frog and a
link to that site's own source.

**The visitor's frog is a mark in the footer**, 44 by 34 and drawn in one ink,
the muted colour, so it reads as another glyph and not as a picture. A short
line sits to its left, that the frog came along, or that it found this site's
hat. Only the mark links to the pond. `guest.js` asks pond for the frog. dash
says the hat was found in its pond panel.

**A frog never moves the page.** Its slot is in the markup from the start with
the canvas at its final size and a stock line beside it, and nothing about it is
hidden and then shown. If pond can't be reached the slot draws the house frog.

## The 404

The same on every tool site, using the description the handler already sets:

```
// 404 · NOT FOUND     caps label
(a lost frog)          216 by 180, a different frog each time
No such page           heading
That page does not exist. This frog took the same wrong turn and is still looking around.
← Home                 button
```

The frog is `guest.js` again, off a random seed, so it needs nothing from pond.
`repos` has a second one for history and anything at a commit SHA asked for
signed out, with a clone command and a way back to the repository.

## Rules that are easy to get wrong

**A reading that is also a control says so by growing an affordance, not a
slot.** A separate button beside a reading costs the title another 40px on
every phone, so the control goes on the reading and shows itself only when
there is something to do.

**Nothing says where it runs.** A landing page footer once named the
repository, the machine and the tunnel in one sentence, which is three facts a
visitor cannot use and a stranger can. The footer rule above already said a site
says nothing about how it is built or where it runs, and it applies to the meta
description and the hero copy too, not only the footer. The source link stays,
since a self-hosted thing that will not show its source is not one.

**`auto-fit` with four cards leaves a hole.** A landing grid of
`repeat(auto-fit, minmax(15rem, 1fr))` resolved at the container width to
three columns and put the fourth card alone beside an empty half row. Four cards
is `repeat(2, 1fr)` and one column on a phone. Count the cards before reaching
for `auto-fit`.

**The orchard mark is a drawn glyph, not a character.** A block or a symbol
character renders at a different width in every face and comes out as a
hairline in some. It sits still, since the bar is the one thing that should.

**No changelog.** analytics and status each had one and both are gone as of
2026-09-01. The newest entry was four months old, the oldest was from 2022 and
described a Django build that no longer exists, and neither was maintained. The
source link carries the real history. Do not add one back.

**The copy has to say what the thing is now.** The analytics home claimed
"Built in Rust, ultralight" for months after the Go rebuild, and its View source
button pointed at an archived repository. Both were invisible because nobody
reads their own marketing copy. When a stack changes, grep the templates.

**`num` is the thousands separator, on every site.** It takes `any`, because a
template handing an `int` to a function declared `int64` fails at render time
with `wrong type for value`, which compiles, passes every test, and 500s the
home page.

**A template listed with no file behind it crash-loops.** Deleting a page means
deleting it from `pageTemplates` in the same commit. `web.NewRenderer` resolves
the list at boot and not at build, so the image builds fine and then dies on
start with `pattern matches no files`.

**Chrome width has to match content width.** The suite's header rows, page
and slim footer all sit in the same 1320px column, with only their backgrounds
running edge to edge. dash broke this until 2026-10-09 with a rail and a status
line running past a capped `main`.

**dash's chrome is panel 00 and panel 19.** The header is a panel like any other,
DASH large with a boxed `MODEL BW-07 / SERIES ORCHARD` plate under it and the
readouts as tiles on the market cards' hairline grid, the two blocks exactly the
same height so their edges line up. The footer is a rating plate, the sticker on
the back of old gear, starting at `SER. NO. 0830-26` (the day dash was built) and
then next brief, the dates ahead, guard and source. MODEL is on the 00 plate only.
Past 1900px both fold into a 236px spine down the left, DASH running up a void
strip with the plate and four amber stripes under it. The spine sits on main's
rows, SYSTEM beside MARKETS and the market band and STATUS beside the local band,
so every gutter on the left lines up with one on the right, and it's sticky when
the window is at least 1180px tall so its bottom can always be reached. It is one
piece of markup: `.console` is `display: contents` below 1900px, which makes the
two panels the frame's first and last rows, and a subgrid column above it with
main as `display: contents`, so nothing is duplicated per breakpoint. Below 1900 the chrome scrolls away with the page.

**A row of unequal strings is a stack, not a flex line.** dash's severe weather
alerts were an event name that could not wrap, an NWS headline of 114 characters
and an end time, all on one line, and on a phone that came out as five wrapped
lines with the time dangling underneath. They are a card now, the event and a
severity chip, then a row of labelled values that wrap as pairs, and the stripe
down the left carries the severity as well as the colour does, since amber
against red is one distinction a reader may not be able to make.

**dash's on air banner sits under the alerts and borrows their card.** It
renders nothing while he's offline, for the same reason the alerts do, and the
red is kept to the pip and the `LIVE ON` chip because a red stripe down the left
already means severe weather. A title that opens with `🔴LIVE |` loses it, since
the chip says so.

**dash's ON AIR panel says when he usually is, and the banner says when he is.**
It sits in the local band beside STREAMING with a status row, NEXT tagged
`SCHEDULED` or `GUESS`, USUAL with the week lit on the days he streams, LAST
(which reads NOW while he's on) and fourteen days of bars against a twelve hour
ceiling with a rule at six. Red stays on the pip and the word LIVE, the same as
the banner. The bars fade toward their floor, because fourteen solid amber
columns outweighed every figure on the page.

**dash's model written text always says it was.** The markets line under the
strip and the TODAY panel each end on a dim caps line naming the slot, when it
was compiled, how many stories went in and which model wrote it. Sources
are short amber tags after the text, the way the wire's were, and TODAY takes
the wider column of the news band since it is prose and HN and Lobsters are
headlines. TODAY is numbered like the feeds, but by impact. The markets lines
say `UP 0.22%` beside a session that happened and `LEANS LOWER` beside one that
hasn't, green, red or amber either way, so a fact and a guess never look alike.

**TODAY opens on a read of the day before the ranked headlines.** A dim caps
line says `MAJOR STORY`, `NORMAL DAY` or `QUIET DAY` and how many outlets the top
story is on, then one short headline per most carried story at 13px over a
hotter rule, with a two word opener from Go only on a major or quiet day. The level is
counted from the outlets and never the model's call, and only `MAJOR STORY`
lights amber, so the panel can't cry wolf on a normal Tuesday. HACKER NEWS and
LOBSTERS each open on a pulse the same way, `BLOWING UP`, `BIG THREAD`, `BUSY`
or `QUIET` in caps (with the row that's taking off when one is), then a read from
the last brief of what the whole front page is about and what the commenters make
of it, with links named by row. Only `BLOWING UP` is amber, and nothing in the
pulse repeats the top row back.

**The news band is three panels that line up row for row.** Each opens on the
same top block (a caps level line, three clamped lines of read, a line of links)
and then ten rows of one ellipsised headline and one meta line at fixed heights,
blank rows filling out a short list, and a one line footnote. Below 1340px TODAY
takes the full width and the two feeds sit side by side under it, and on a phone
it's one column.

**Cut text opens in a readout card, never a `title`.** The browser's own tooltip
is small, slow, unstyled and missing on a phone. `tip.js` keeps one card for the
page, void black with the four amber corner brackets, a plate naming where the
text came from (`TODAY | 04`, or `BBC | TODAY` on a source tag, which shows that
article's title), the full text at 13px, and a short amber leader back to the
line. It paints in with a four frame stepped wipe and the brackets flash once,
none of it under reduced motion, and there's no shadow.

**A news row's headline opens the card and the ↗ at its end opens the article.**
Headlines and reads in the news band are `data-card` lines, a tap, a click or
Enter opens the card and the same again closes it, and on a mouse a cut one also
previews on hover. The card carries the row's way out as full width rows, the
article and the comments for a feed, and every source with its headline for
TODAY. A headline is never a link and nothing waits on a long press, since a
phone keeps long press for its own link menu and the two raced. Nothing else on
a feed row is a link either, `CMT` included, so a tap anywhere on a row but the
↗ opens its card. The ↗ is the full height of the row and 32px wide so a thumb
finds it. A card opens on TL;DR when there is one, of the article on a feed row
and of every outlet's report on TODAY, and a feed row's card then carries
COMMENTERS, the brief's read of the thread, and TOP COMMENTS, each under a small
amber head, with the links last. When that runs
taller than the room the card's body scrolls, never the card itself, since the
brackets and the leader sit just outside it. `data-full` text
elsewhere opens only when it's actually cut, and a source tag's `data-tip`
shows its headline on hover and focus and is just a link on a phone.

**ADVISORIES and INCIDENTS are a band of their own between the news band and
UPLINK**, the advisories wide since their titles run long and the incidents a
rows list like UPLINK. An advisory row is a pip, the product, the title over a
meta line (tag, id, source, age) and the status on the right, which is the part
that says whether to do anything. Amber is to do, green patched, faint not
affected, and dim a fix that's out where dash can't see the version. Patched
and the host's own versions only show signed in. Each provider under
INCIDENTS has a strip of fourteen day cells under it, amber for a day an
incident touched this setup. A fresh
exploited one that wants doing also gets a banner in the NWS alert style above
ON AIR for 72 hours. On a phone the title wraps under the product instead of
cutting off.

**UPLINK goes at the bottom, full width.** It grew to more rows than any
readout beside it and stretched the whole local band to match, so it moved under
the news band on its own and runs its rows as many across as fit. That leaves the
local band six panels, six wide from 1900px where `main` widens, then three and
then two, so no width leaves a hole. Panel 19 under it doesn't list the sources again, since
UPLINK names them all. It says when the next brief is due and the next four
market moving dates, CPI, PPI, jobs, JOLTS, GDP, PCE and the Fed, with the label
in amber.

**POND, panel 18, sits beside UPLINK** in a column of its own, since adding a
seventh to the local band would leave a hole at every width. It's the visitor's
frog from pond, or the house frog when they have none, drawn from a copy of
pond's `frog.js` at 3px a cell in amber with dim amber for the belly and legs,
so the colour stays pond's reason to be opened. The frog is on a screen across
the panel, a void ground with a waterline under it, its name on the glass in
Space Grotesk and its stage, day and odds in the other corner. It hops once as
it loads, breathes and blinks, and croaks only when tapped. Under the screen is
a plate in STATUS's hairline cells, pebbles, power and a count of what's waiting
that links to the first of them, and for the house frog its morph and rarity.
The plate is one row at every width, where STATUS's own goes to a column or a grid.
The link to the pond is the panel's meta. It replaced a thumbnail with a label
over value list beside it, which no other panel used, and a list of links that
made the panel taller than UPLINK. On a tablet and phone it drops under UPLINK
full width.

**Say a thing once.** That NWS headline was the event name and the end time
again in a sentence, so the longest string on the page was mostly a repeat of
the two short ones beside it. The row shows what is not already there instead,
which is the counties, the forecast office and how long is left.

**Do not cite this file, or anything outside this repository, from a comment.**
A reader only ever has the repo.

## pond

pond is a game, and it's a night pond lit by lanterns, pixel art scenes over
dark navy cards, with Inter for everything and Pixelify Sans only on the `pond`
wordmark. The direction is called Lantern and was picked on 2026-11-19 over the
Deck look, a tape deck crossed with a field instrument, which had moulded
plastic, keycaps, readout glass, screws and hazard stripes. It doesn't share
dash's palette any more, so a change on dash isn't carried over. The values
below live as tokens at the top of `frontend/src/index.css`.

- **The palette.** The page is night navy `#0b1222`, a card is `#121c33`, a
  card inside a card or a sheet row is `#18243f`, and the 1px line round a
  card is `#22304f`. Text is `#eef0e6` and muted text `#9aa6c2`.
- **Lamp yellow is for what to press.** `#ffcf6a` is the main key on a screen
  and the lit tab or place, on a lip of `#c98a2a`, and a lit key's text. A
  number is plain text and never lamp yellow, so the eye goes to the key.
- **Green means ready or new.** `#6fd08c` is the edge on a row that's ready,
  the LEDs, a pip on a tab, a meter's fill and anything that went up. Pink
  `#f29bb8` is hearts and bond, and `#ff8a7a` is a bad turn or a price down.
- **Inter for everything but the wordmark.** Headings, section labels, keys,
  pills, tabs, numbers and readouts are Inter semibold or bold with tabular
  figures, and a sentence is Inter regular. Pixelify Sans was on all of those
  and was hard to read small, so it's only the `pond` wordmark now, in its 700
  weight. Both come from `@fontsource` and are served from the site.
- **A card is 10 to 12px round with a 1px line**, and a small tag or a rarity
  box is 4px. Round is for pills, LEDs, the gear and the `?` key.
- **The main key is lamp yellow on a 3px darker lip** and travels down onto it
  when pressed. Every other key is a ghost, see through with a line round it.
  A shop price is a ghost key lettered in lamp yellow, and a key that can't be
  pressed loses its colour.
- **A pill** is a fully round chip with an icon and a number, and a filter is a
  pill that's lit lamp yellow on a dark amber while it's on.
- **A section label** is Inter semibold caps, small, letterspaced and muted,
  with its count or a `?` key on the right. There's no rule out to the edge.
- **A row that's ready has a green left edge**, 3px inside its card. A goal
  has a green LED, a ring until it's done and lit after.
- **LEDs glow.** A round dot with a soft glow of its own colour, green for new
  and ready. Only the new pip and a game's goals use one.
- **What the frog says is a cream speech bubble** with a dark outline and a
  tail pointing up at it, `#f4ecd0` on `#1d1a14`, the one light thing on the
  pond page apart from the gazette.
- **The top bar** is the `pond` wordmark in Pixelify Sans on the left, and on the
  right a Bag pill with how many things are in the bag, the pebbles pill and a
  small round gear key for settings (the name, the pond key, frog power, sound).
  The bag opens from its pill and the pill lights while it's open. Over a
  scene it fades from dark to nothing with no line under it, and it fills back
  in once the scene has scrolled up under it. On the share and key pages the
  bar is the wordmark and the gear, always filled.
- **The dock is five places by verb** along the bottom where a thumb is, each
  a pixel icon over a word. Pond is the frog and what to do next. Frogs is the
  reeds, every frog's card, and a frog's job screen. Play is the games. Make
  is the garden and kitchen, the Bank (the gravel bar, the dye works, the
  burrows with the paintings, and the far bank) and the wizard, behind a row
  of three keys with the shore scene over them.
  Town is the neighbours, the shops, the stall under the word Sell and the
  market, behind a row of four keys. The lit tab is lamp yellow on a card.
  Old links like `#reeds`, `#works`, `#bank`, `#market` and `#stall` still
  land on the right tab. The content column stops at `34rem` on a desktop.
- **An LED pip means new, never just waiting.** It's a green dot that glows
  on a dock key, a key inside Make or Town, or the Bag pill. It lights when a
  thing turned up since that key was last opened and goes out on opening it.
  A daily thing is new once a day, the stall each time it sells more, and the
  gravel bar and the rent only once they're full. What was seen is kept in
  the browser. The lanterns hung in the Make scene are the other half, one
  over anything ready.
- **A new pond opens up over its first days.** It starts with Pond, Play,
  Make and Town, and Make has only the wizard. Frogs and the garden and the
  Bank under Make come when the egg hatches a day in, and the Market key in
  town three days after that. The gazette says so that day, as an Opening
  today notice over the lead and as the line on the folded strip. A link to a
  place that isn't open lands on the pond, and a pond made before this has
  everything.
- **The Next strip is what to do, a card a row**, under the bubble on the
  pond. Each row is an icon and a line and goes straight to its place. A thing
  that's waiting has the green edge, and a goal, a part of the game the pond
  hasn't tried, has its LED, with any count in a small box at the end of the
  row. The set and the badge nearest to done get a row each and no more, over
  the rest when one is a step from done and under them otherwise. A new
  pond's goals are its four first day steps, and their prize is one lamp key
  at the top. Three rows show, and a small `+N` key on the end of the third
  opens the rest.
- **Every place has a painted scene across the top**, edge to edge and up
  under the top bar, drawn in `frontend/src/scenes.js` on the painters in
  `frontend/src/paint.js`. Each is painted once at two CSS pixels a pixel (the
  job three) and laid on its canvas at a whole number of device pixels a
  pixel, so it's crisp at any width, laid out 195 wide and centred with the sky
  and water carried on past the sides. Each canvas has a fixed height in the
  CSS so nothing moves the page. Only fireflies, lamp flicker and rain move, a
  few times a second while the scene is on screen, and reduced motion gets
  them still. The pond is the night pond with the frog on its pad, the moon
  and its path on the water and a lantern with its pool of light. Frogs is
  every frog on a pad in two staggered rows with a lamp over the one out
  front and the empty pads bare. A frog's page is that frog close up at twice
  the size on its pad by the lantern, and laying an egg is the parent on a pad
  with its frogspawn glowing on the water beside it. A job is the frog at work under a lamp. Make
  is the shore with the reed lodge on stilts, the garden beds and the cloche,
  the gravel bar's sluice and wheel, three dye vats that steam their colours
  while a batch boils and the burrow mound with a lit door each, everything
  dimmed until it's built, and a tap on a building opens it. Town is stilt
  huts and the mushroom house along a jetty under string lights with three of
  the residents out on it, a different three each day. Shops is a shopfront
  with a striped awning and its keeper, one over every shop, Sell is the
  stall with the frog behind the counter and what's up on it, Market is the
  boardwalk with a chalk board of how the goods and the coin moved, Play is a jetty with a
  row of firefly jars, and the bag is the satchel on a table by a lamp with
  the pond's own things spilled round it. The kitchen pot and the garden are
  smaller pictures in a dark frame in the same night look.
- **Night is night and day is dusk.** Every scene reads the real day, a
  starry navy sky with the moon at night and a dusk sky with a low sun by
  day, so the lamps still read, and rain greys both and falls over them.
- **Empty spots have a dashed edge** with a dim outline of what goes there. An
  empty spot you can fill, like a stall spot, is a key with a dashed edge and a
  plus.
- **What's coming.** The frogs in the scenes get a warm rim on the side the
  nearest lantern is on.
- **The market is a night market board.** Under the boardwalk scene, whose
  chalk board shows three goods and the coin, comes the moonmelon cart as a
  row like the Next strip's, lit with a lamp Buy key on a Sunday and green
  edged on the Saturday they spoil. Then the board, a card a thing with its
  12 by 12 icon, its name over one muted line saying why it's moving, a spark
  line in green or red and the price in plain text over its change. Then the
  tape, the neighbours' latest trades in a dark well, each a small lit face,
  the name in bold and the time on the right. What's held is a row of tiles,
  and the back door is a row whose key opens the hooded frog under it.
- **Colour lives in the pictures, the icons and rarity.** The frog, the water,
  the wheel, Dive, the stall and the wizard are colour pixel art. Past that
  the colour is the 12 by 12 pixel icons, a thing's tier, lamp yellow on keys,
  green for ready and pink for hearts.
- **Icons are 12 by 12 and lit from the top.** Each is a filled shape with a
  dark `#0a0d16` edge round every filled cell and the top edge of a shape a
  quarter lighter, all added in code so every icon is lit the same way. They
  draw at whole scales only, 24px in the dock, the pills, the tabs and the
  Next strip, 36 in a slot and 48 or 60 on a reveal, and a scene draws them
  at its own pixel size. A dye keeps its colour per kind, a pattern is a
  little block with its face cut in the pattern it prints, and each game has its own icon for
  its Next row.
- **Every game is a firefly jar** on Play, a small card with a glass jar,
  the game's icon inside, its name and one short line. A jar that still pays
  in full is lit, with a glow that flickers a little and the line in green
  saying how many runs are left, and a spent one is dark with the line in
  muted text, Quarter pay or Paid out. The day's game, one a day that pays a
  run more, has a lamp yellow TODAY sticker stuck on crooked and a dark amber
  edge. Fishing is the flagship and has a card of its own on top, its picture,
  name, line and pay beside a lamp Cast key, which goes ghost once it's spent.
- **Play is shelves.** Under fishing the jars sit four to a row, five on a
  desktop, on an Arcade shelf for the quick games with the deepest first, a
  Puzzles shelf for the slower thinking ones, and a Classics shelf for retired
  games that still play but pay nothing, each under a section head. Under the
  jars, Every day has the race, the wheel and the old log as rows like the
  Next strip's, with a green edge and a lamp key while there's something to
  do, and the three logs to pick from inside the log's row.
- **A side game fills a tall phone.** The field sits in the middle and the
  room over and under it is a flat colour the game names, or its top and
  bottom rows stretched where that carries the scene on. Crossing takes more
  rows in place of either. A run pauses when the page is hidden, and the title
  panel says PAUSED until it's tapped.
- **Every game has two goals a day**, the same for every pond. They're two
  small green LEDs in the top corner of its jar, ringed until met and
  lit after, and lines in a box on its title card with the same LED
  each. A run that meets one gets the whole results, with that line lit green.
- **The side games end like Dive.** Their title and results sit on a card
  over the field, and the strip under every game is its numbers in boxes and an
  Eject key.
- **A catch and a crash are felt.** A catch stops the game for two or three
  frames with a burst of square bits and the points popping up over it, and a
  crash stops it for six and shakes the field a few pixels. The frog squashes
  when it lands or catches. Reduced motion gets no shake and no squash, and
  every game takes this from `feel.js` so they all feel the same.
  A near miss says CLOSE in the surface green.
- **The older arcade games have a moment every ten seconds or so**, shown
  before it lands. In Dive the heron leans over the water ahead with a dotted
  amber line down to where its beak reaches, and stabs as it passes over the
  frog. In crossing one road lane blinks amber at the end it comes from and
  says CONVOY, and three yellow tractors run down it fast. In tongue flick a
  golden fly twinkles at its edge of the field and zips across for 8. In
  raft run a whirlpool turns in the river ahead, pale rings round a dark eye,
  and drags the leaf in. Its eye is a bump and getting past inside its rings
  says SPUN for 3.
- **Pebble drop is a shower.** Ten pebbles go as fast as the thumb lets go,
  a little apart, so several bounce down through the reeds at once, and the
  pegs sit a little differently each day. Each pebble climbs its own stretch
  of a pentatonic scale a reed at a time, so a shower chimes, and a reed it
  touches turns amber and stays lit for the round. Lighting every one pays
  10, and once all is down the lit reeds pop out in a ripple from the middle
  pot. The player's own frog sits on a lily pad drifting over the pots and
  a pebble landing on it comes back with a croak, three at most before it
  slips under. A pink bud or two on the day's board splits a pebble in two,
  three times a round at most. The last pebble lined up on the 8 pot in the
  bottom rows falls at 40% with a slight zoom, and a run of landings in the
  middle three pots sends rings out over them and pays a point or two more.
  The 8 pot wobbles when that pebble lands in it, nothing flashes, and
  reduced motion gets no zoom or wobble.
- **Chorus is four frogs on a log at night**, the player's frog on the right
  with an amber mark on the log under it, and the rain coming down harder each
  round. Under the log is a lane in readout glass, a column under each frog,
  and every note rises up it to a cream line at the log about a bar and a half
  ahead, so nothing is ever sprung on the player. The other frogs' notes are
  small marks in their own colours so the groove can be seen, the player's
  part is open amber rings while it's heard and solid amber bars while it's
  theirs to sing, and a part sung from memory fades out on its way up. A bar
  line is brighter than a beat line. What the log is doing sits over the
  frogs' heads, the song's name, LISTEN, the count in as amber digits one a
  beat, SING, FROM MEMORY and the grade at the end. Nothing is lost on a miss
  and the frog only ever sings or sits.
- **Pad putt is mini golf on the mud banks**, seen from above. The greens
  are moss walled in by logs with cut rings at the ends, and the cup is a
  dark hole with the pink pennant on a reed. A round is nine holes dealt
  fresh from a pool of 23, three easy, three middling and three hard, in
  that order. The pool has banks, slopes, mushrooms, a waterwheel, the pond
  with a drifting pad, a stream, hollow logs, a reed gate, a drifting cup
  pad off a ramp, mud, a turtle pacing a gap, a dozing bullfrog that flicks
  the ball back while it's awake, reeds that fold one way, lily pads that
  sink under a ball that stops on them, plank bridges over water and gusts
  across a gap, and the harder holes mix them. Anything that pushes the ball
  draws which way, chevrons walking downhill on a slope, streaks running
  down a stream and leaves blowing across while the wind is up, with a few
  stirring just before it comes. The bullfrog's reach is a dotted ring that
  goes amber while it's awake, and it blinks just before it wakes. The frog
  sits behind the ball facing the putt and flicks it with its tongue, and
  hops after it once it stops. A drag back draws a band behind the ball
  going yellow, amber and red with the power, a power bar in the bottom
  strip, and dots out to the first thing the ball meets or as far as it
  rolls on the flat, so a bank shot's second leg is the player's part. Each
  hole opens on a thin strip under the top readout saying the hole and its
  name, and the top strip has a pip a stroke out of six. Water is a splash
  ring and a stroke, and the ball goes back where it was played from. The
  results have a scorecard, a glass box a hole with its par small over its
  strokes, amber under par.
- **The spawn jar is a glass jar on a shelf by the pond**, the frog over it
  from above like pebble drop's, the next one in the top corner, a faint
  guide down to where it'll land, and a dashed line across the glass that
  blinks red while something rests over it. Each stage is round so it stacks
  and rolls, and the ball itself is never turned, only its eyes, spots and
  belly go round with it, since pixel art turned at an angle breaks up. An
  egg is a pale ball of jelly with a dark yolk, a tadpole in its jelly, a
  froglet green with a tail stub, a frog in the player's frog's colours, a
  bullfrog olive with a yellow throat and brown ears, an elder in brown and
  cream with white whiskers and brows, a great frog deep teal with gold
  spots and orange eyes, and a pond king bright green with a gold crown.
- **Tongue lash is dusk over the far bank**, the frog on a pad between two
  reed beds and bugs tossed up out of the reeds in arcs. A swipe drags the
  tongue along the finger, a fat pink line from the mouth through every spot
  it went, and lifting reels it in from the tip with whatever stuck riding on
  it, all gulped at once. Flies, gnats in threes, moths that sway, dragonflies
  that dart across and beetles that come up fast and drop are drawn on a grid
  of 2 like the frog. A swarm's reed bed glows amber and says SWARM before it
  goes up together, and a golden beetle twinkles where it'll come up. A wasp
  costs one of three lilies in the corner and ten gulps in a row grow one back.
- **Swarm season is midsummer on one big lily pad**, seen from above in the
  middle of dark water with its notch and veins. A drag anywhere moves the
  frog by as much as the finger moved, so the thumb never sits on it, and the
  tongue snaps the nearest bug in reach by itself, a pink line out and back
  with the bug riding it home. Gnats, flies and moths are harmless and get
  shouldered aside, a wasp or a beetle costs one of three lilies with a
  blink of safety after, and a beetle stops and draws a dotted amber line
  before it runs it. A wave's side of the water shows pale chevrons first.
  Bugs drop dew drops, the frog laps up any within a short reach, and the dew
  fills a pale blue bar along the top with the level beside the score. A full
  bar pushes the bugs back in a yellow ring, dims the field and brings up
  three perk cards on whichever half the frog isn't on, each with a pixel
  icon, what its next level does and three pips, and a pick slows the pond
  down for a moment. The first bar fills in a few seconds and they come
  slower after, a dozen or so a round. A perk that changes a reach draws its
  new ring for a moment. Two perks at their third level make a combo, a card
  with a cream edge and COMBO on it that comes first at the next pick, and a
  frog down to one lily is always offered one back. Taken perks and combos
  sit as small icons in the bottom corner. A beetle about to run puts an
  amber mark over itself as well as its dotted line.
- **Whirlpool is the pond from above with a little eddy in it**, a dark eye
  with three pale arms going round. A drag anywhere moves it by as much as the
  finger moved, like swarm season, and it swallows anything smaller than
  itself and grows, duckweed and petals first, then leaves, twigs and bugs,
  lily pads, the reeds, ducks, logs, and at the very end the moored boats, the
  jetty and the willow island. There are eight bands of half a dozen things
  or so, all drawn from above on a grid, a few of them rare with a twinkle on
  them. The camera backs off as it grows so the next band comes into view, and
  crossing a band pops its name with a little push in that reduced motion
  doesn't get. Things go round and down the middle turning as they go and the
  arms wind tighter after each gulp, and anything alive only goes round for a
  spin and is flung clear with a ring of stars to paddle off. Something a
  little too big just blocks the way, and a hard knock on something well over
  says BUMP and spits one thing back out. The size reads in CM and then M in
  the top corner over the band, and the last few catches tick up the bottom
  corner with a small picture each. The last ten seconds say LAST SWIRL and
  pull from further off, and the end backs off to the whole pond with
  everything that went down thrown up in a spout under a rainbow. The results
  are the haul as a box a kind with its count, biggest first and the rare ones
  edged in amber.
- **Cattail sling is the pond side on, a bent cattail on the near bank** and
  acorns stacked on logs, rafts and rocks out on the water under a sky that
  changes each level, morning, noon, dusk or a frosty one. A pull anywhere
  draws the frog back into the cattail's band, the two stalks bend toward it
  and the band goes amber at full stretch, with cream dots over the first
  part of the path, and the last fling's whole path stays behind faintly.
  Letting go twangs the stalks. Reeds, driftwood, mud, ice and stone are each
  drawn and break in their own colours with their own sound, crack before they
  go, and anything in the pond is cleared with a splash ring, acorns and wood
  bobbing off and stone sinking. A log bobs and rocks and a raft drifts too,
  and what's on them rides along and sleeps once still, so a stack never
  creeps. The player's own frog comes first and a new one most levels, a tree
  frog that splits in three, a dart frog that zips straight, a bullfrog that
  drops like a stone and a puff toad that blows things off, each with its name
  and trick on the level's banner. A flung frog that stops hops into the pond
  and swims back to the bank. Three pieces breaking at once says TIMBER and
  goes slow a moment, which reduced motion doesn't get. A level ends on three
  stars popping in one at a time with frogs to spare hopping off for 100
  each, and the results are a row a level with its stars and score.
- **Bug sweep is the day's puzzle.** Eight pads by ten on flat water, a
  closed pad green and an open one dark with its number in a colour of its
  own, pale blue for one up to red. A marked pad has a pink bud on it, and a
  two part key under the grid, LOOK and MARK, says what a tap does, with the
  lit half amber. The wasps only show once the go is over, the one stepped on
  over dark red.
- **The day's result is a share card.** A SHARE key on the results and in the
  paper opens a cream card like the postcard, with the date, the frog's name,
  the time or the pads cleared, the taps, and a row of ten pads lit for each
  tenth of the grid. It never shows where a wasp was.
- **A fish is a shadow until it's landed.** In the water and on the line it's
  one dark shape, so its size is all there is to go on, and it comes up in
  its own colours with its name over it as it's landed.
- **A run that beat nothing ends on a short card**, the score over a lamp
  Again key, sat a third of the way down so the crash still
  shows under it, and one tap is the next run. A new best or a place on Dive's
  podium gets the whole results.
- **A round of fishing ends on its catch**, a slot a kind with the biggest
  one's length, and a new fish or a pond record gets the whole results. While
  there are keeps left today the slots are keys, and a tap on one keeps that
  fish for the kitchen. The short card has the same fish as small slots over
  AGAIN, so a round that keeps nothing is still one tap from the next.
- **A game's first open has a ghost finger and no title.** A cream pixel hand
  does the game's first input over the field, a tap, a hold or a drag, until
  the player has done it once. It shows once a browser.
- **A tool is a line on its keeper's shelf**, after the day's stock, one a
  job they sell for: Moth's nets and goggles, Big Bean's garden things and
  Tansy's blades and spades. It shows the next tier the pond hasn't got,
  with the job and level under its name and what it cuts, a price key, a
  dim LEVEL tag until a frog is good enough, and YOURS once the top one is
  bought. Each job has one picture, in plain wood, then iron, then brass.
- **A frame is a whole postcard.** Once a pond has one of Marsh's frames the
  camera key opens a sheet of small copies of the card in each frame it owns,
  the plain one first, and a tap makes that one. A frame only changes the
  card round the picture and the colour of the words on it.
- **The market is a board.** A ticker scrolls along the top in a dark strip,
  still under reduced motion, with the day's story from the paper first and
  then the six biggest movers either way, green up and pink down. The
  readouts under it are one key that opens the holdings, every share and coin
  held with what it's made, the dividends ever paid and the best of them. A
  business in the paper wears an IN THE PAPER tag on its card, and a holding
  that has a big day pops a reveal once that day. Each business is a pixel
  shopfront in its own awning colour with its price in a box, and a day down
  turns that price pink. The chart in a business's sheet is a stepped pixel
  line, amber over thirty days that ended up and pink when down.
  Four filter keys pick Shares, Goods, Coin or the back door. A good is its bag icon with
  what's in the bag under its name and its price a lot, and Glowworm Coin is
  a pixel gold coin over a taller chart. Dividends waiting are a row under the
  tiles like a Next strip row, with what the shares pay a day and a
  lamp Collect key, dark while there's nothing to take.
- **The back door is an alley.** A fourth filter key on the market opens a
  screen of brick wall and a hanging lantern. While it's open a frog in a
  black hood stands behind a crate under the lit lantern, its eyes and belly
  the only colour on it, and while it's shut the door is closed with a card
  hung crooked saying when it's back. Each deal is a shelf line with the name
  as it's written on the sign, spelling and all, the hooded frog's patter in
  quotes under it and a price key in COIN with what it comes to in pebbles
  under that. A line under the screen says plainly that a deal is a gamble.
  A bought deal says A STEAL in green, FAIR, or STUNG or FAKE in pink, and
  its reveal is a joke for a fake, never a sad one.
- **The day has a mood.** A faint wash over the water per mood, grey on a down
  market, pale on a frost, warm on a bright one, and the frog has a tiny TV
  or handheld on the pad on a day it's watching or playing something.
- **A press is felt.** The main key travels onto its lip, LEDs glow, and with
  sound on there's a soft clunk and click made in code like the croak. Sound
  is off until turned on, and so is a buzz on a bonk, both as keys in settings
  that stay in that browser. Tap to reel is a third key beside them, for
  anybody who'd sooner not hold a finger down. With it on a tap starts the
  fishing reel and another stops it, and the reel says REELING or STOPPED
  under the strain meter. Badge is the fourth, off too, which puts the count of
  waiting rows on an installed pond's home screen icon, with a line under the
  keys saying so, and it's hidden where the browser can't do it. There's never
  a push.
- **It plays like a game and reads like one.** Words are kept to a line, and
  anything that explains a place sits behind the `?` key on its head. Things
  are pictures in slots, not rows of text.
- The water palette is for canvases: `#123a3d` down to `#0a1b1f`, a `#7fd6c2`
  surface, `#fff6dc` cream, `#ffe27a` yellow and `#e2567a` pink.
- **A slot is how a thing is drawn**, a rounded square with its picture, a
  count in the corner and its tier as the border colour and as one to five
  small diamonds in the other corner, so tier isn't colour alone. One not found yet is
  its shape in the dark with `???` and a dashed border, so a collection shows
  how much there is without giving it away. Tapping a slot opens a bottom
  sheet with the thing big and what can be done with it.
- **The fish book is a shelf in the collection.** A slot a fish, drawn in its
  colours as big as the slot allows, with how many in the corner and the
  biggest one's length under its name. One not landed yet is its dark shape,
  and its sheet says when it bites, so the book gives away the day to try and
  never the fish. Each fish has a pond record a neighbour holds, mostly
  Sedge, and a slot says RECORD in green once the pond has beaten it. The
  round that does it gets a line from Sedge under the catch, in his own
  words and not in caps, and a line in the paper's Records the day after.
- **A box has a meter.** The hat box and the trinket box each have a green bar
  like the collection's, under the odds, with one line
  saying how many boxes until a rare or better is sure. It fills a notch a
  plain box and empties on a rare, and the odds line says the rule.
- **One hat can be wanted.** A hat not found yet has a key on its sheet to
  ask Little Tully for it, and its slot says WANTED in lamp yellow. Over the hats
  sits a panel like a box's, the hat in the dark, a bar of what the pond's
  spares have sold to him for and one line under it. When the bar's full the
  line names his price and a lamp Shop key goes there, where the hat is on
  his shelf in its colours with WANTED under its name.
- **A wanted thing has a lamp.** A slot in the bag that somebody is after, a
  neighbour, today's favour or the one named dish it could cook next, has a
  small amber lamp under its pips and an amber glow round it that breathes,
  opacity only.
  Reduced motion gets the lamp and no glow. Its sheet says who wants it, a
  line each with the same lamp, over the keys.
- **A prize pops up in the middle** with slow rays behind it in its tier's
  colour, and stays until it's tapped away. A bad turn of the wizard's gets no
  rays. One that comes while another is up waits its turn.
- **A set is a panel on the Sets shelf.** What it puts on the water is on
  the head, dim until the set is finished, with its name, one line and how
  many are found in a box, and its four members are slots under it.
  One not found is a shadow like any other, and its sheet says where it
  comes from and which set is waiting on it, here and on its own shelf.
- **A finished set is on the water for good.** A mushroom ring, a second
  lantern on a post, a little bridge, a scarecrow, a weathervane and a wind
  chime hung from the top of the picture, each in a place of its own behind
  the frog that clears the furniture, the trinket spots and both top
  corners. The chime and the vane move with the wind and the lantern is lit
  after dark. Reduced motion holds them still.
- **The far bank is a strip of land along the top of the picture.** It's
  bought a patch at a time on the Bank under Make, under the burrows, with its
  middle in a picture and the count and the next price in boxes, and each patch stands
  one more small piece of scenery on it, a tree, a cottage, a windmill, a
  deer, in 2px cells like the swimmers since it's far off. The back row is
  hazed with the water's colour. Thirty pieces fill it from the middle out,
  clear of the hearts and the camera key, and past that a small wooden sign
  at the right end says how many more. Nothing on the bank does anything.
- **Badges are enamel pins on a board**, a card on the Badges shelf.
  A pin is a pixel disc with a gold rim, a picture and enamel in the colour
  of its part of the game. One not earned is its shape in the dark with how
  far along it is under it, and a tap says what earns it.
- **A badge or a set pops up when it lands**, a badge with green rays and a
  set with amber ones. More than two at once share one card with a row of
  small pins, so a pond that had earned a lot already isn't walked through
  them one at a time.
- **Days at the pond are a count and never a streak.** It's in settings in a
  box with a line saying when the next keepsake comes, and nothing
  anywhere says how many days were in a row or that one was missed.
- **Well rested is a row and a sparkle.** After eight hours with no game
  played the Next strip gets a waiting row saying the next game pays a quarter
  more, the frog's Zs drift off it and four pixel sparkles twinkle round it
  until a game is played. The result of that run says well rested beside the
  pebbles. It takes no room of its own on the pond.
- **A frog's favourites are two slots in settings**, its dish and its hat
  drawn the way the bag draws them, under its name. The bubble names one on
  about a day in three in place of the day's line, so it never gets longer,
  and the key that feeds a frog says so when the dish is its favourite.
- **A visitor comes once a day while the pond is shut.** The first visit of
  a day pops it up as a reveal with the frog drawn the way the pond draws
  any frog and the common thing it left by its feet, and a row in the
  greeting's list says who. A small muddy footprint sits on the front edge
  of the pad, by the frog's left foot, until the next day's visitor. A Stay
  home key in settings keeps a pond's own frog off everybody else's.
- **A frog's diary is in settings**, under its favourites. One line a day in
  the frog's own words about the day before, the day in printed caps down the
  left and the line beside it, newest first with a key for older days. It
  only says what happened and a day nobody came by reads as a quiet day.
- **A camera key makes a postcard.** A small dark key with a pixel camera
  sits over the top right corner of the frog's picture. It draws the screen as
  it stands at twice the size on a cream card, with the frog's name in
  Inter bold, the pond's date and the gazette's weather word in Inter and a
  pebble postmark, and opens it as a picture in a new tab. The hearts and the
  key aren't on the card.
- **The frog can be rubbed.** A drag back and forth over it shuts its eyes and
  throws pink pixel hearts, more of them on its favourite spot, and an egg
  only wobbles. Its bond is five small hearts over the top left corner of the
  picture, the one being worked on filled from the left. Nothing else is drawn
  in that corner or the top right one, and the bottom middle is the power pill's.
- **It does a thing for each heart.** It waves when the pond opens at one, and
  a tap croaks as ever and then does the newest thing it knows, a hop at two,
  a spin at three and at four something that suits its nature, a song, a big
  leap, a blush, a yawn with stars, a fly off its tongue or a bubble. At five
  it comes after a finger held on the water. All of it is the scene moving
  the same sprite, so `frog.js` stays as it is. Reduced motion gets the shut
  eyes and whatever is drawn over the frog, with no hop, tip or spin.
- **The pond fits a phone with no scroll.** The frog's picture with its power
  pill, its name with the rarity beside it, the bubble and the Next strip all
  sit above the dock at 412 by 915. The pond is drawn 352 by 272 in its own
  units with the frog 8 to a cell, a cell a whole even number of device
  pixels and about 5 CSS pixels on a phone, 6 on a desktop. The frog sits on
  its lily pad, which comes forward of its feet onto the water under it, with
  a stretch of water behind it up to the far trees, the reeds and cattails at
  both sides and the lantern standing in the water on the right. All of that
  sits on the bottom of a 372px scene with the sky above. Under 780 tall the
  scene is 340px with less sky and the rows drop from 40px to 34px. A desktop
  gets 480px. The folded gazette, Today at the pond and anything longer go below.
- **Today is a row of four tiles** under the Next strip, each an icon, a
  number and a word, for the game runs that still pay in full, what the stall can
  still sell at the full rate, the neighbours who haven't had a dish, and the
  free spins or the time to the next one. The section label says when the new
  day comes. A tile with nothing left dims. Every number comes from the server.
- **Dive is the whole window, on a phone and a desktop.** It covers the bar and
  the dock, the pond takes its depth and length from the window, and a strip
  under the water holds three number boxes and Eject. Ten ghost frogs swim
  each run behind your frog at 45% alpha, with their names over them where
  the field is wide enough to read ten. A ghost goes out on the stems or
  the rock of the gate it was dealt, in a burst of bubbles under a pink
  OUT, and is carried back with that gate as it fades.
- **The daily race is a row under the jars**, like the wheel's,
  with a chequered flag over the water, and it opens Dive on the day's
  course with the twelve residents as the ghosts and their names on the
  standings. Its title says whether this run goes on the board, and its
  results are the board, the whole finishing order with you in amber and
  a line saying whether the run was on the board or practice.
- **Dive is a race.** Three red lights and GO start it, a standings column
  down the left drops each frog to the bottom as it goes out, your place is a
  big italic number in the bottom right that pops as it climbs, with how
  many are racing and how many are still in under it in small caps, and who just
  went out is called at the top. The results are a podium in gold, silver and
  bronze, the whole finishing order with you in amber, and the winnings
  counting up, with confetti for a podium place. All of it is HTML over the
  canvas, animates transform and opacity only, and takes no taps.
- The game page has no footer. The copyright and the source link are on the
  key page.
- Every sprite is drawn per pixel from a seed in `frontend/src/frog.js`. There
  are no image files, and a canvas is always `image-rendering: pixelated`.
- **A frog is lit from the top left** on every pond page, a lighter cell
  where nothing is above it or to its left, a darker one where nothing is
  below or to its right, and a white shine in each eye. That's
  `frontend/src/lit.js`, so `frog.js` stays the plain drawing other sites copy.
  The side view sprites in Dive, pebble skip and lily stack are flat, and so
  is the share card PNG the server draws.
- **Inter bold on a canvas.** Words a game draws are Inter bold at the
  canvas's own pixel size, scores and readouts included.
- **Anything read is 14px or more.** 10 to 12px are for section labels, the
  rarity box, a key's legend under an icon and the caption under a picture in
  a slot or a jar, all in Inter semibold caps, which would wrap at 14.
- Rarity is a word in a box as well as a colour: common grey, uncommon green,
  rare blue, epic purple, legendary amber.
- **The real day is on the pond's screen.** Night brings the stars and fireflies,
  rain falls over it, the water runs clear with mayflies or murky with
  beetles on the market, ripples quicken with the VIX, and a cold day has
  frost on the pad and the frog dozing. Each thing gets a line under Today at
  the pond saying why, so nothing on the water is a mystery.
- **The Pond Gazette is a little newspaper**, the one thing printed on paper.
  On the pond it's folded, a strip of cream newsprint under what to do next
  with the masthead, the issue and the lead on one line, a turned corner and
  the other pages showing under it, tilted a touch. A tap opens the front
  page in a bottom sheet, in the system serif, with a masthead between double
  rules, a dateline with the issue and the weather, a lead story beside a grey
  photo of a neighbour, two stories in columns and a Business story. Under
  the stories a ruled box has the day's bug sweep, a grey picture of the grid
  and a line on how the pond stands with it, and a tap on it opens the game.
  Never a number past the date and issue. A dream is a row in the greeting's list on
  the first visit of a day, and a neighbour's gossip is an italic line at the
  top of its sheet.
- **Neighbours are a row each**, under today's favour, so the list reads
  without opening anyone. A row is the frog small, its name and title with
  lodger after it, its hearts with a thin rail and how many points to go, and
  its favourite with a tag, In bag in lamp yellow, Can cook in green, or Need and
  the ingredient short, or Recipe at two hearts while it's a secret, then
  what it's worth. On the right is one key, Give and the points for the best
  dish in the bag for that neighbour, lamp yellow when it's the favourite, Cook to
  the kitchen with the favourite's two picked when there's nothing to give,
  or a green Fed tick once it's eaten today. The ones that can be fed come
  first and the fed ones last, dimmed, with filter keys over the list for
  not fed, a favourite ready, and all.
- **A neighbour's sheet is its hearts first.** Under the name it says the
  title and points, then a newsprint tag when the gazette names it, its
  thought as an italic cloud and what it says as a speech bubble. Then the
  ladder, a row for each heart with what it brings and the points it starts
  at, a tick on the ones had and the next one outlined in lamp yellow, then every
  dish in the bag with what it's worth to this neighbour and a Give key,
  then the facts in boxes.
- **A gift always answers.** A gift or a favour opens the neighbour's sheet
  on a reply in a box of its own, the line it says, the points in green, the
  rail filling from where it was to where it is in a lighter pink, how many
  to the next heart and what that heart brings. A dish that isn't the
  favourite gets a line naming the favourite and its worth. A present that
  comes with a heart pops over it as before. Reduced motion gets the rail
  where it ends up and no fill.
- **A best friend gives a memento.** At five hearts a neighbour hands over
  one thing of its own, in a reveal with epic rays and what it says as the
  line. It shows as a Memento box on its sheet and in a Mementos shelf in
  the collection, where one not given yet is a dark shape that only says
  who it comes from.
- **Frogs is a roster**, a card a frog in two columns, the one out front first
  and outlined in lamp. A card is the frog on its own little pad scene with
  its tool beside it at work, then its name, its rarity box and one line with
  an LED, basket full or ready to lay lit green, a job with its level and
  basket count on a lamp LED, and hatches in, full for or lays again in on a
  dark one. A pad nobody's on is a dashed card, and the next pad to buy is a
  dashed card with a lamp plus and its price. A tap on a card opens that
  frog's page. A row over the cards takes every basket at once. The register
  of looks is in the bag's collection.
- **A frog has a page of its own** at `#frog-<id>`, under the Frogs key, with
  a back key to the roster. The frog is close up over the page, then its
  name, its rarity box and its 1 in N, and a line of stage, day, colour,
  nature and back. Four tiles say its job level, its basket, how long it's
  full for and when it can next lay. Under them are the keys, Lay an egg in
  lamp only when it can, Feed, which opens the dishes in the bag, and Out front.
  Then its work, the job and level over a bar to the next, the basket as a row
  with a Take key, and keys to the job screen and to change the job. The frog
  out front also has its hearts for today as five tiles, each with the trick
  it learns there, since bond belongs to whoever is out front. Then its
  favourite dish and hat as slots and the last line of its diary.
- **Laying an egg is its own screen** at `#lay-<id>`, from any grown frog.
  Under the parent and its frogspawn are three slots, Colour, Pattern and
  Eyes, dashed with a lamp plus while empty and lit with the thing once one
  is picked from a sheet of what's in the bag. A line says what the frog is
  full of and how often each takes. What could hatch is a card an outcome,
  the frog drawn lit in that colour and back over its odds, with a green NEW
  tag on a colour or back the register hasn't got, then anything else, and a
  dashed card for odd eyes or what eye drops would give. The odds come from
  the server off the bench's own rules. Lay the egg is the lamp key along the
  bottom, and the egg pops up as a reveal on the roster.
- **A job is its own screen**, opened off a frog's page. The frog
  works in a picture drawn for the job, a net and bugs, a sickle in the reeds,
  a spade in the mud, a watering can in the beds, a dive for pebbles, a
  hammer at the craft bench, and it squashes as each action lands while the
  thing it got hops into a basket in the corner. Under the picture is the
  action bar with the time to the next one, then the basket
  as slots with counts and a lamp key to take it, the level with a bar to
  the next and what that level opens, and the targets as keys with their
  time, xp, yield and misses. Each open target has a thin mastery bar under
  its numbers with its mastery beside it, the bar dimmed while the
  frog's level holds it at the cap. Under the level sits the tool the frog
  works with, its picture, name and what it cuts, with a Shop key while
  there's a better one to buy, and a shaded one with who sells it before the
  pond has any. A locked target is its dark shape behind a dashed
  edge with the level that opens it. The bar only fills and the basket
  only gains what the server dealt, and a full basket stops the bar and says
  so in green and as a Next strip row.
- **Coming back is one card.** Taking baskets, and the greeting when one is
  full or half way there, opens a card in the bare sheet with a block a frog,
  how many actions and misses, what came in as slots, and the xp beside the
  level. A level up pops a reveal after it with amber rays,
  the jump over a slot for everything it opened, a target,
  a recipe, a find, a tool in town or the next step of mastery, each with
  what it is under its name. A pet or a 99 still pops its reveal after.
- **A working frog shows it.** In the Frogs scene a frog at a job has its
  tool beside it.
- **Frog power is a pill on the pond's picture.** It sits on the bottom edge
  of the frog's picture, half over it, with the rank in small caps, the
  number, and a green hairline along its foot for how far to the next rung.
  It takes no height of its own. A tap opens the breakdown in the bare sheet,
  the number big in a box with the rank, a rail to the next rung, and every
  part as a row under Finds, Frogs and work and Games, its name, a line like 12
  of 60 or best 40 of 80, its points and a bar along its foot for how much of
  that part is had.
- **Power only counts up.** When it rises the pill counts up to the new number
  once the pond is in view and no prize is over it, with a small green line
  over it saying what from, like +120, a new hat, that fades after a moment. A
  fall, like a game's reference rising, just shows the new number. A new rung
  is a reveal with amber rays, the rung's name and the number in a box, after
  the count. Reduced motion gets the new number and the line with no count.
- **A frog's public page** is the frog in its picture, its power as one big
  number in a box with the rank beside it, the parts that make it up
  in the same three groups as the sheet, and a QR code for the link. The
  ranks are a ladder of nine under the number, the top rung first and green up
  to where the frog stands, the same in settings. It ends on getting a frog of
  your own.
- **Visitors and stamps paint pictures.** Each one is a piece of a pond
  painting, twelve to a painting and four in all, the pond at dawn, in the
  rain, by lantern light and in the snow, each a small pixel scene in a
  wooden frame. Under the burrows the four sit in a row, a piece showing as
  a square of the picture, the one under way with a dark hole for each piece
  still to come and the others bare canvas, and the frame gilding round from
  the top left as it fills. A finished one pops up as a reveal and hangs on
  the burrow mound in the Make scene as a small gold frame.
- **A frog's page has six stamps** under its name, a heart, a fly, a wave, a
  star, a leaf and a pebble, as a row of square keys with the stamp's pixel
  art on each. A visitor with a pond of its own presses one a day and it
  latches down, and the line under the row says what a visitor can do. The
  owner's next greeting pops up the stamps as a reveal, each stamper's frog
  with its stamp at its feet, and a frog kept home stamps as a dark shape.
  Each stamp is a 12 by 12 icon like the rest.
- **The stamp book is under Frogs**, a row at the foot of the roster. Its
  scene is the book open on a desk under a hanging lamp, its pages holding
  the faces of the latest stamps, with an ink pad and a rubber stamp. Under
  it is the painting under way, big, with its holes, beside a Share page key,
  then the frogs who stamped the page as perforated cream stamps in three
  crooked columns, each the frog on a coloured ground with the stamp over its
  corner and its name and the day under it. Spots still empty are blank
  stamps with a faint frog shape and "not yet", and the last row is a link
  to a random pond's page with the stamps left to give today.
- The card a share link unfurls with is a 1200 by 630 PNG drawn on the server,
  the screen on the left and the name, rarity and power on the right in
  JetBrains Mono, since that's the face the server has a TrueType file of.

## Per site

| Site | Phosphor | Notes |
|---|---|---|
| `analytics` | suite | On the suite, no Bootstrap. The list carries a fortnight of page views per property, and menus are details elements |
| `status` | suite | On the suite, no Bootstrap. Properties down first, a property page that doubles as the public status page |
| `logging` | suite | On the suite, no Bootstrap. The overview, a page per source and search share one set of panels |
| `auth` | suite | On the suite, with no Bootstrap left. The home page signs you in from the hero, and the starfield is gone |
| `repos` | suite | On the suite. Its own stylesheet keeps the tree, diff and README styles and maps its colour names onto the suite's. No serif any more |
| `llm` | suite | On the suite. Overview, calls and keys, and the home page says whether Claude or the card is answering |
| `cron` | suite | On the suite. Jobs, runs, a job form and keys, with `app.css` holding only what is cron's own |
| `dash` | amber | Hand written CSS. Scanlines, vignette, JetBrains Mono and Space Grotesk. Panel 00 and panel 16 for chrome, a spine past 1900px. A `NOTIFY` switch in panel 00, and a `BRIEFS` rerun beside it that only appears signed in |
| `pond` | none | Hand written CSS. A game in the Lantern direction, a night pond with lamp yellow keys, Inter with a Pixelify Sans wordmark, and colour pixel art in its pictures. Dive is full screen on a phone |
| `blog`, `isaacbythewood.com` | neither | Separate identities. Nothing here applies to them |

