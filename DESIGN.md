# DESIGN.md

What every site here looks like and why. Read this before changing anything
visual, and update it when a decision changes.

There is no shared stylesheet and there is not going to be one. Every site owns
its own copy of its CSS the same way it owns its own copy of `web/`, so keeping
these sites looking alike is something a person keeps true by following this
file rather than something a build step enforces. A change to the palette is a
change in every one of them.

## Two phosphors

Terminals before colour used one of two coatings, P3 amber and P1 green, and
that is the split the estate runs on.

- **dash is amber.** It is the only public page with no login, it is glanced at
  rather than read, and it is the one site allowed to be a costume. pond is a
  game and wears the same one.
- **analytics, auth, chat, llm, logging, repos and status are green.** They are
  tools you read for minutes at a time. Green is the primary, and amber is
  chrome only, so caps labels, the hero eyebrow, and warnings. Amber is never
  the primary on any of them.

Everything else is shared between the two: warm near black rather than pure
black, one monospace face, caps labels on a single tracking value, a structural
grid drawn in CSS, and no bitmap textures anywhere.

## The palette

`analytics`, `auth`, `logging` and `status` keep this in
`frontend/static_src/base/styles/_variables.scss`, and those four files are
**byte identical**. `repos` has no Sass variables file, so it carries the same
values as custom properties at the top of its `base.scss` and has to be edited
alongside them.

| Role | Value | Where it goes |
|---|---|---|
| Ground | `#0e0d0a` | `body`, the page behind everything |
| Sunken | `#090806` | The navbar, the hero panel |
| Panel | `#1c1a15` | Cards, inputs, dropdowns, list groups |
| Raised | `#24211a` | Hover on a panel |
| Text | `#ddd7cd` | Body copy |
| Muted | `#b3aba2` | Secondary copy, feature descriptions |
| Faint | `#8a8279` | Labels, timestamps, placeholders |
| Green | `#6b9e78` | Primary, borders, focus |
| Green bright | `#7db88c` | Links, stat values, the accent word in a headline |
| Green hot | `#95cca2` | Link hover |
| Amber | `#c9a84c` | Caps labels, eyebrows, warnings |
| Terracotta | `#c47055` | Errors, down, over a limit |
| Info | `#7eaab8` | The rare thing that is neither good nor bad |

dash keeps its own set, and amber `#ffb000` there is the primary rather than a
label colour.

**The border alphas are a floor, not a preference.** Every border on the estate
measured between 1.04 and 1.36 to 1 before 2026-09-01, and the panels themselves
sat at 1.04, which is invisible under a bright light because glare adds light to
every pixel and the bottom of a dark palette has nowhere left to go. Card
borders sit at `0.42` alpha and inputs at `0.5` for that reason. Do not take
them back down to make something look softer, and do not introduce a new
hardcoded grey where a token exists.

## Type

One face does nearly everything.

- **Monaspace Argon** for body, headings, labels, and code, at `0.92rem` base
  with `1.7` line height. It is set as both `$font-family-sans-serif` and
  `$font-family-monospace`, which is why the sites read as terminals without
  anything else having to say so.
- **Newsreader**, on `repos` alone, and only on repository names and the
  repository page heading. A serif earns its keep on a proper noun sitting above
  a wall of monospace and nowhere else here.
- Headings are weight 700 with `-0.01em` tracking. Hero titles run
  `clamp(2rem, 5vw, 3.2rem)`.
- Caps labels are `0.12em` tracked, weight 600, and amber. Every eyebrow, every
  feature label, every stat label, and the `// SECTION` headings in the footer.

## The grid

Every site draws a 32px grid on `body` in two CSS gradients and nothing else.
Green at `0.028` alpha on the seven green sites, amber at `0.022` on dash, which is
where the numbers differ because green reads fainter than amber at the same
value.

```scss
background-image:
  linear-gradient(rgba(107, 158, 120, 0.028) 1px, transparent 1px),
  linear-gradient(90deg, rgba(107, 158, 120, 0.028) 1px, transparent 1px);
background-size: 32px 32px;
background-position: center top;
```

32px because that is the padding rhythm the panels already use, so the grid
lines up with the layout rather than cutting across it.

**Scanlines and the vignette stay on dash.** They are a `multiply` blend over
the whole page, which darkens body text, and these sites are read rather than
glanced at. A section that wants the grid to show through sets
`background: transparent` rather than repainting the ground colour, which is why
`.stat-strip` is transparent.

## The home page

analytics, status, logging, chat and llm open the same way, and the home page is
an advert for running the thing yourself rather than a dashboard. It ends by
pointing at the source.

The skeleton, in order:

1. **Eyebrow.** A pill with a green dot, then version, stage, and `self-hosted`.
   A site with no version to give puts one fact about itself there instead, the
   repository count on repos, the model on chat and `openai compatible` on llm.
2. **Headline.** One line, on the pattern `<thing> for operators who host their
   own <noun>`, with the last two words in green.
3. **Hero paragraph.** Roughly 30 words. A list of what it does, then the
   sentence `Your data never leaves your infrastructure.`
4. **Terminal block.** Five or six lines of the thing actually happening, in the
   site's own vocabulary. This is the best thing on any of these pages.
5. **Call to action.** Access dashboard, then Docs where there is one, then View
   source.
6. **Stat strip.** Three or four real numbers, comma grouped through `num`.
7. **Six feature cards.** An amber caps label, a title, and two sentences.
8. **Footer.** One paragraph, then `// Pages` and `// Operator`.

**The budget is about 250 rendered words** and status is the reference at 199.
Anything past 300 is too long. Explanation belongs on the documentation page,
which is what it is for.

`repos` shows its hero to signed out visitors only, because signed in that page
is a working repository list and an advert would be in the way every visit.
Repositories archived upstream sit below the rest under their own Archived
heading at 0.6 opacity, each card back to full on hover, so old code reads as old.

chat and llm have no Bootstrap and no Vite, so their landing pages write the
skeleton out by hand in a `.landing-body` block in `static/app.css`.
That block is byte identical between the two and carries the shared palette
rather than the app's own, so a change to it is made in both, and checked
against a Bootstrap site's computed styles rather than by eye.

## Dashboards

The home page is an advert and the pages behind it are instruments. They are
scanned rather than read, so the craft moves from typography to information
design: the summary comes before the detail, and state is encoded in shape as
well as in number.

**The pieces, in the order a page uses them.** A `section-label` in amber caps
introduces a band. `metric-tile` carries a label, a value and an optional delta
chip. `chart-panel` wraps a canvas with its own caps title. `rankList` is the
ranked table with a bar behind each row. Findings use a severity chip, never
colour alone.

**`.metric-label` grows only inside `.metric-header`.** The tile is a column
flex, so a bare label with `flex: 1` eats the height and drops the number to the
bottom of the tile. That is what the bot traffic tile on analytics did until
2026-09-01, and it only showed up next to a tall neighbour.

### Charts

Every chart on every site draws from `chart_theme.js`, which is duplicated byte
for byte in analytics, logging and status the same way `shipper.go` is. There is
no shared bundle to put it in, so it gets copied and kept in step.

**Series colours are punchier than the UI palette.** Chrome can be
muted because nothing depends on telling two borders apart, and a doughnut slice
does. Same hues, more chroma.

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
here keeps one. Reordering these means measuring them again rather than eyeballing
the result.

**Anything past the fourth category folds into `other`** rather than getting a
generated hue, because a fifth and sixth colour out of these hues is not
distinguishable from the four already used. `foldToPalette` does it.

**Status is a separate job from identity.** ERROR, WARN, 5xx and the rest are
keyed by name and never handed out in order, so a level keeps its colour
whatever order the server sent the rows in.

**One axis, ever.** No chart here has two y scales. Two measures of different
size are two charts.

**The map is a sequential ramp,** one hue from light to dark with no second
colour in it. Selection is a status rather than a step on that ramp, which is
why it is the one warm colour on the map.

**A narrow panel puts its legend at the bottom.** At a third of a row wide there
is not enough room beside a doughnut, and Chart.js silently truncates the labels
to fit, which is how "Downtime" rendered as "Downt".

### Direction, on a price or a reading

`dash` and `chat` both draw a line whose colour says which way a number went,
which is a different job from telling four series apart and does not use the
palette above. Two colours and a neutral, defined per site so each sits on its
own ground:

| Slot | dash | chat | Job |
|---|---|---|---|
| up | `#5ddc82` | `#7fc98f` | gained, or a reading inside its good band |
| down | `#ff5f4d` | `#d98063` | lost, or a reading past it |
| flat | `#8a8172` | `#9c8e7a` | no move worth colouring |

**Direction colours the move and the trace, never the figure itself.** The price
stays `--text`, because it is the number being read and the move is the
judgement about it. A chart that coloured both would have the reader chasing the
same information twice.

**The fill under the line is the same hue at about 9% alpha**, which reads as
the line having weight rather than as a second series.

**A dotted rule across the box is what the move is measured from.** Intraday
that is the previous close, so the shape means something on its own, and over
any longer span there is no rule because the first bar drawn is the baseline and
the left edge already says so.

**An svg stretched to the column width cannot carry a dot.** `preserveAspectRatio`
is `none` on these, so a circle comes out an ellipse and a cursor has to be a
vertical rule. Anything that has to stay round is an HTML element positioned
over the top.

## Components

These are measured values, not approximations. `repos` writes its CSS by hand
and the other four go through Bootstrap, so the only way to know they agree is
to read the computed style off a rendered page. They agree today.

**Button.** One object everywhere.

| | Value |
|---|---|
| Padding | `0.6rem 1.1rem`, and `0.25rem 0.5rem` at `.btn-sm` |
| Font size | `0.82rem`, and `0.805rem` at `.btn-sm` |
| Tracking | `0.04em`, and `0.02em` at `.btn-sm` |
| Radius | `0.25rem`, and `0.2rem` at `.btn-sm` |
| Ghost | 1px `rgba(221, 215, 205, 0.28)`, text `#ddd7cd` |
| Primary | 1px `#6b9e78` on `rgba(107, 158, 120, 0.16)`, text `#7db88c` |

Define the buttons at the top level of the stylesheet and not inside a page
section, or a button anywhere else falls back to a bare element rule.

**Borders, in three weights.** `0.2` for a rule between bands, `0.3` for the
navbar and the edges of a page section, and `0.42` for a card or panel. Nothing
sits below `0.2`, since anything fainter is invisible on this background.

**Caps labels are one solid amber,** `#c9a84c`, at `0.7rem` and `0.12em`
tracking. The footer headings ran at `0.55` alpha and the section labels at
`0.75`, which read as two different colours side by side.

**Focus and selection.** `:focus-visible` is a 2px `#6b9e78` outline at 2px
offset, and `::selection` is `rgba(107, 158, 120, 0.3)`. Every site sets both,
since without them tabbing through a form gets whatever Bootstrap does per
component.

**Citations are boxed.** `chat` boxes the number on its
own, 1px `rgba(65, 78, 65, 0.55)` on `--panel` at `0.62rem`, raised `0.32em`,
because it points off the site and a reader has to see it is a link worth
tapping. It sits at the end of the sentence it belongs to and never mid
clause.

**An unread answer is a dot, never a count.** `chat` marks a conversation whose
turn finished while the reader was somewhere else with a `0.4rem` circle in
`--green-bright` after the title, and the same dot at `0.35rem` on the button
that opens the sidebar, since on a phone the list it marks is off screen. A
number would be answering a question nobody asks: how many replies are waiting
does not change what you do about them, and the row already says which
conversation it was.

**A turn waiting its place in line says where it is.** `chat` runs one turn at
a time on one card, so a conversation with a turn going shows it in the list in
place of its time, `working` for the one on the card and `next up` or `queued #2`
for the rest, in amber, with a ring after the title where a working one has the
dot. The thread of a waiting turn says the same number in words, counting the
turn on the card as one ahead, so the list and the thread never disagree.

**A flagged conversation says so in place of its time.** `chat` marks one he asked
to come back to with `flagged` in amber where the time goes, the same slot `working`
and `queued #2` use, and opens its thread on a bordered note with an amber
`FLAGGED` label, his own words, and a `clear` button. His words are the reason for
the flag, so they are shown as he typed them.

**Notifications are a switch somebody presses.** A browser only asks for
permission from inside a press, and a public page asking on load is a prompt
nobody wanted, so `dash` has a `NOTIFY` readout in panel 00 beside `LINK` that
asks the first time and opens a menu of what to be told about after that, and
`chat` asks on the first question sent. Both keep quiet about anything the
reader is already looking at, apart from a severe weather warning on dash.

**dash's quick chat is for its one signed in reader and looks like the rest of
the page.** A `CHAT` readout joins `NOTIFY` and `LINK` in panel 00 and ctrl+k or
cmd+k opens the same box, an amber panel head, the question under a left rule the
way the CONDITIONS verdict sits, chat's answer in JetBrains Mono, and `OPEN IN
CHAT` once there is a conversation to open. The page ships it to everybody,
since it can come out of the edge's cache, and neither the readout nor the key
does anything until `/api/quick` says the visitor is signed in. A phone has no
ctrl+k, so the readout is the way in there and reads `OPEN`.

**dash's quick chat shows its work as a tape deck.** Every turn gets a deck
between the question and the answer: two reels and a strip of tape that run
while it works, the stage in amber caps with a block cursor, a fourteen segment
meter that lights whole segments on a stepped tick, jumps on each event, falls
back between them and holds its peak a moment, a `TAPE` counter of
tokens the model has written, and a clock. Under it a log of each model round,
tool call, lookup and gate check as it happens, the call with a moving strip
until it returns and a line under it saying what came back. The log folds to a
one line summary the moment the answer starts, since the answer is what gets
read then, and the summary opens it again. Everything that moves is transform
or opacity and all of it stops when the turn does.

**Content width** is Bootstrap's container scale, so 1140px and then 1320px past
a 1400px viewport. `repos` matches it with a media query rather than sitting
narrower on a wide screen.

## The footer

Four columns on every site, in this order, and the headings carry the `//`
prefix in amber caps.

1. **`// <SiteName>`** with one paragraph saying what the site is for. Around 25
   words, and it says nothing about how it is built or where it runs, which
   helps nobody and narrows an attacker's guesswork.
2. **`// Elsewhere`** with Portfolio, Blog and GitHub. The same three links on
   every site, so none of them ever links to itself.
3. **`// Pages`** with the site's own pages, ending on Source.
4. **`// Operator`** with the signed in pages, or Sign in when signed out.

Then a footer bar with `© <year> <author> · Some rights reserved` on the left
and the GitHub mark on the right, linking to that site's own source.

**The visitor's frog is a mark in the footer bar**, to the left of the GitHub
mark, 44 by 34 and drawn in one ink, the bar's own muted colour, so it reads as
another glyph there and not as a picture. A short line in the bar's `small`
sits to its left, that the frog came along, or that it found this site's hat on
the visit that finds it, or for the house frog that hatching one finds a hat
here. Only the mark links to the pond and nothing about it changes on hover.
`guest.js` asks pond for the frog. The bar is on every page, signed in or not,
so a hat is found wherever a visitor lands. chat's app has no bar, so its mark
is the last row of the sidebar, and llm's is in its one line foot. dash says the
hat was found in its pond panel.

**A frog never moves the page.** Its slot is in the markup from the start with
the canvas at its final size and a stock line beside it, and nothing about it is
hidden and then shown. If pond can't be reached the slot draws the house frog,
so it's never left empty either. It goes where the page already has a place for
a mark, and takes its colour from the page.

## The 404

Identical on the four Bootstrap sites, and it uses the description the handler
already sets
rather than writing its own line:

```
404 · NOT FOUND        amber caps section label
(a lost frog)          216 by 180, a different frog each time
No such page           heading
That page does not exist. This frog took the same wrong turn and is still looking around.
← Home                 ghost button
```

The frog is `guest.js` again, off a random seed, so it needs nothing from pond.

`repos` has a second one for history and anything at a commit SHA asked for
signed out, the same skeleton with `404 · SIGNED IN ONLY`, a clone command and a
ghost button back to the repository.

Left aligned like every other page here.

## Rules that are easy to get wrong

**A flex row gives up the label, never the reading.** `chat`'s bar holds a
title, then the flags, the context meter, the tokens a second and the model. The
title is the only one that can lose width and still mean something, so
`.bar-right` is `flex-shrink: 0` and the title ellipses. Without that the
`NO SEARCH` flag appearing took its width out of the readings, and the tokens a
second wrapped a character at a time into a vertical column. Anything narrow
enough to wrap in that row needs `white-space: nowrap` of its own, which
`.model` had and `.tps` and `.meter` did not.

**The same row gives up the model name and the tokens a second on a phone.**
Under `34rem` the name goes and the dot and the eject stay, because the model
is the one thing in that bar the reader already knows and the two glyphs are
what they act on. The tokens a second goes with it, since incognito, no search,
the meter and the speed all showing at once ran the row 83px past a 360px
screen and pushed the model off the edge entirely.

**A reading that is also a control says so by growing an affordance, not a
slot.** `chat`'s model chip names the model, fills its dot when the weights are
on the card, and unloads them when pressed, and the amber eject glyph only
appears when there is something to eject. Adding a separate unload button
beside it would have cost the title another 40px on every phone.

**Nothing says where it runs.** A landing page footer once named the
repository, the machine and the tunnel in one sentence, which is three facts a
visitor cannot use and a stranger can. The footer rule above already said a site
says nothing about how it is built or where it runs, and it applies to the meta
description, the hero copy and the stat strip too, not only the footer. A stat
strip carries numbers, so `Go / stdlib, SQLite, one binary` was not a stat, it
was a stack disclosure sat in the slot where a number goes. The source link
stays, since a self-hosted thing that will not show its source is not one.

**`auto-fit` with four cards leaves a hole.** A landing grid of
`repeat(auto-fit, minmax(15rem, 1fr))` resolved at the container width to
three columns and put the fourth card alone beside an empty half row. Four cards
is `repeat(2, 1fr)` and one column on a phone. Count the cards before reaching
for `auto-fit`.

**A wordmark is not a logo.** A brand that is the site's name at body size
with a `▍` in front of it is smaller than the text beside it in the bar. Make it
a bordered key carrying a cursor block and the name at `0.95rem`.
The block does not blink, since a logo that moves pulls the eye away from the page
every second and a bar is the one thing on screen that should sit still.
The cursor is a drawn block rather than a glyph, since a block character renders
at a different width in every monospace face and came out as a hairline in some,
which is an empty box where the mark should be.

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

**Chrome width has to match content width.** logging's navbar and footer were
`container-fluid` while its home page was `container`, so the hero sat narrower
than the bar above it. All four use `container` now, and only logging's
dashboard pages go full width inside it. dash broke the same rule until
2026-10-09 with a rail and a status line running edge to edge past a capped
`main`.

**dash's chrome is panel 00 and panel 16.** The header is a panel like any other,
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
was compiled, how many stories went in and that a local model wrote it. Sources
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

**UPLINK goes at the bottom, full width.** It grew to more rows than any
readout beside it and stretched the whole local band to match, so it moved under
the news band on its own and runs its rows as many across as fit. That leaves the
local band six panels, six wide from 1900px where `main` widens, then three and
then two, so no width leaves a hole. Panel 16 under it doesn't list the sources again, since
UPLINK names them all. It says when the next brief is due and the next four
market moving dates, CPI, PPI, jobs, JOLTS, GDP, PCE and the Fed, with the label
in amber.

**POND, panel 16, sits beside UPLINK** in a column of its own, since adding a
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

pond is a game, and it's cassette futurism taken further than dash takes it,
pond as a piece of 80s kit, a tape deck crossed with a field instrument. It
keeps dash's warm near black, the amber, JetBrains Mono for everything read and
Space Grotesk for anything large, with dash's names and values for the ground,
the lines, the text and the amber, so a change to those on dash is made here
too. The direction is called Deck and was picked on 2026-11-10 over a tidier
version of the old look and a handheld pet shell.

- **Three surfaces and no others.** Dark moulded plastic for panels, with a
  very fine brushed grain, sunken glass windows for screens and readouts, and
  keycaps that stand proud of the panel. Every texture has a job, and
  scanlines go on screens only, never on a control.
- **Corners are 3px on plastic and keys and 4px on a window.** Round is for
  LEDs, the `?` key and nothing else.
- **A key is a cap with a top highlight and a dark lip** that travels 3px down
  when pressed. The one main key on a screen is amber plastic, the rest are
  dark. A shop price is a key, amber lettered when you can afford it and
  sunken and dim when you can't.
- **A number lives in readout glass**, a sunken window of very dark green with
  amber digits that glow a little. Pebbles, levels, rates and counts all sit
  in one, so a number never looks like a key. That green is the one colour
  Deck adds.
- **The Next strip is what to do, a key a row**, under the bubble on the
  pond. Each row is an icon and a line and goes straight to its place. A thing
  that's waiting has a blinking amber LED, and a goal, a part of the game the
  pond hasn't tried, has a green one, a ring until it's done and lit after,
  with any count in readout glass at the end of the row. The set and the badge
  nearest to done get a green row each and no more, over the rest when one is
  a step from done and under them otherwise. A new pond's goals are its four
  first day steps, and their prize is one amber key at the top. Three rows
  show, and a small `+N` key on the end of the third opens the rest.
- **A filter is a small raised key** that stays pushed in and amber while it's
  on, since a sunken one read as disabled. The places in town are a row of five
  keys with pixel art over the label like the tab bar, so they never wrap.
- **Every screen gets a picture in a window.** Works, the reeds, settings and
  the kitchen too, with their words under the picture and never instead of it.
  A screen window has a bezel and a screw at each top corner.
- **Works has one picture for all of it**, over its three keys. The bank side
  of the pond from left to right, the reeds' lodge, the garden shed and its
  beds, the gravel bar, the dye works and the burrows, each dimmed until it's
  built. A window or a lamp is lit yellow on whatever has something ready,
  and a tap on a building opens its place.
- **Panel labels are printed on.** Small caps in warm grey like the lettering on
  a faceplate, with section heads in amber as below.
- **Empty spots are hazard striped sunken trays** with a dim outline of what
  goes there. Stripes mean empty or locked and nothing else. An empty spot you
  can fill, like a stall spot, is a key with a dashed edge and a plus instead.
- **Every game is a floppy disk** on Play, a 3.5 inch disk in its own colour
  with a metal shutter, a write protect tab and the game's picture on a cream
  label. A disk that still pays in full has a blinking amber LED, the rest are
  faded, and the pay tag and best score sit under it. One disk a day is the
  disk of the day, with an amber sticker stuck on crooked. Fishing is the
  flagship and its disk takes the whole first row, with a big amber title and
  a line about it beside the disk.
- **Play is shelves.** Under the flagship the disks sit on an Arcade shelf for
  the quick games with the deepest first, a Puzzles shelf for the slower
  thinking ones, and a Classics shelf for retired games that still play but pay
  nothing, each under a section head. The wheel and the old log stay as panels under the shelves.
- **A side game fills a tall phone.** The field sits in the middle and the
  room over and under it is a flat colour the game names, or its top and
  bottom rows stretched where that carries the scene on. Crossing takes more
  rows in place of either. A run pauses when the page is hidden, and the title
  panel says PAUSED until it's tapped.
- **Every disk has two goals a day**, the same for every pond. They're two
  small green LEDs on the disk's plastic by the shutter, ringed until met and
  lit after, and lines in readout glass on its title card with the same LED
  each. A run that meets one gets the whole results, with that line lit green.
- **The side games end like Dive.** Their title and results sit in a panel
  with a screen window, and the strip under every game is readout glass and an
  EJECT key.
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
- **A run that beat nothing ends on a short card**, the score in readout glass
  over an amber AGAIN key, sat a third of the way down so the crash still
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
- **The market is a board.** A ticker scrolls along the top in readout glass,
  still under reduced motion, with the day's story from the paper first and
  then the six biggest movers either way, green up and pink down. The
  readouts under it are one key that opens the holdings, every share and coin
  held with what it's made, the dividends ever paid and the best of them. A
  business in the paper wears an IN THE PAPER tag on its card, and a holding
  that has a big day pops a reveal once that day. Each business is a pixel
  shopfront in its own awning colour with its price in glass, and a day down
  turns that price pink. The chart in a business's sheet is a stepped pixel
  line in glass, amber over thirty days that ended up and pink when down.
  Four filter keys pick Shares, Goods, Coin or the back door. A good is its bag icon with
  what's in the bag under its name and its price a lot, and Glowworm Coin is
  a pixel gold coin over a taller chart. Dividends waiting are a row under the
  tiles like a Next strip row, with what the shares pay a day in glass and an
  amber COLLECT key, dark while there's nothing to take.
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
- **A press is felt.** Keys travel, LEDs blink, and with sound on there's a soft
  clunk and click made in code like the croak. Sound is off until turned on, and so is a buzz on a bonk, both as keys in settings that stay in that browser.
  Tap to reel is a third key beside them, for anybody who'd sooner not hold a
  finger down. With it on a tap starts the fishing reel and another stops it,
  and the reel says REELING or STOPPED under the strain meter. Badge is the
  fourth, off too, which puts the count of waiting rows on an installed
  pond's home screen icon, with a line under the keys saying so, and it's
  hidden where the browser can't do it. There's never a push.
- **Colour lives on the screens, the icons and rarity.** The frog, the water,
  the wheel, Dive, the stall and the wizard are colour pixel art inside a
  window. Past that the only colour is the 8 by 8 pixel icons, a thing's tier,
  the readout green and the green LED on a goal, and everything else is amber,
  off white and black.
- **It plays like a game and reads like one.** Words are kept to a line, and
  anything that explains a place sits behind the `?` key on its head. Things
  are pictures in slots, not rows of text.
- The water palette is for canvases: `#123a3d` down to `#0a1b1f`, a `#7fd6c2`
  surface, `#fff6dc` cream, `#ffe27a` yellow and `#e2567a` pink.
- **Panel 00 is the faceplate**, POND in Space Grotesk, a `MODEL FR-01 / SERIES
  ORCHARD` plate that drops out on a narrow phone, and the readouts as tiles on
  a hairline grid, `PEBBLES` and a square key with a pixel gear and no word, which opens the
  settings (the name, the pond key, frog power).
- A section head is an amber index chip, a caps title and a rule out to the
  edge, the same as a dash panel head.
- **Five places are transport keys along the bottom**, each a pixel icon over
  a word, where a thumb is: Pond, Play, Bag, Works and Town. The lit one stays
  pushed in like a tape deck's PLAY, and a key with something new behind it
  has a small amber LED. Dive and the wheel open from Play and light it.
  The content column stops at `34rem` on a desktop.
- **A new pond opens up over its first days.** It starts with four keys,
  Pond, Play, Bag and Town, and every disk, the wheel and the log are in Play
  from the start. Works gets its key when the egg hatches a day in, and the
  bank's Market key shows three days after that. The gazette says so that day,
  as an Opening today notice over the lead and as the line on the folded
  strip. A link to a place that isn't open lands on the pond, and a pond made
  before this has everything.
- **Works is the reeds, the garden and the bank** behind a row of three keys
  like the places in town, and it opens on the one that was open last. A key
  with something new has the same LED as a tab, and the market stays
  behind the bank's own pair of keys.
- **An LED pip means new, never just waiting.** It lights when a thing turned
  up since that key was last opened and goes out on opening it, on the tabs
  and on the keys inside Works and Town alike. A daily thing is new once a
  day, the stall each time it sells more, and the gravel bar and the rent
  only once they're full. What was seen is kept in the browser. The lit
  windows in the Works picture are the other half, lit for anything ready.
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
  Sedge, and a slot says RECORD in amber once the pond has beaten it. The
  round that does it gets a line from Sedge under the catch, in his own
  words and not in caps, and a line in the paper's Records the day after.
- **A box has a meter.** The hat box and the trinket box each have a bar of
  the same lit notches as the collection's, under the odds, with one line
  saying how many boxes until a rare or better is sure. It fills a notch a
  plain box and empties on a rare, and the odds line says the rule.
- **One hat can be wanted.** A hat not found yet has a key on its sheet to
  ask Little Tully for it, and its slot says WANTED in amber. Over the hats
  sits a panel like a box's, the hat in the dark, a bar of what the pond's
  spares have sold to him for and one line under it. When the bar's full the
  line names his price and an amber SHOP key goes there, where the hat is on
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
  many are found in readout glass, and its four members are slots under it.
  One not found is a shadow like any other, and its sheet says where it
  comes from and which set is waiting on it, here and on its own shelf.
- **A finished set is on the water for good.** A mushroom ring, a second
  lantern on a post, a little bridge, a scarecrow, a weathervane and a wind
  chime hung from the top of the glass, each in a place of its own behind
  the frog that clears the furniture, the trinket spots and both top
  corners. The chime and the vane move with the wind and the lantern is lit
  after dark. Reduced motion holds them still.
- **The far bank is a strip of land along the top of the glass.** It's
  bought a patch at a time in Works, under the burrows, with its middle in
  a window and the count and the next price in glass, and each patch stands
  one more small piece of scenery on it, a tree, a cottage, a windmill, a
  deer, in 2px cells like the swimmers since it's far off. The back row is
  hazed with the water's colour. Thirty pieces fill it from the middle out,
  clear of the hearts and the camera key, and past that a small wooden sign
  at the right end says how many more. Nothing on the bank does anything.
- **Badges are enamel pins on a board**, a sunken tray on the Badges shelf.
  A pin is a pixel disc with a gold rim, a picture and enamel in the colour
  of its part of the game. One not earned is its shape in the dark with how
  far along it is under it, and a tap says what earns it.
- **A badge or a set pops up when it lands**, a badge with green rays and a
  set with amber ones. More than two at once share one card with a row of
  small pins, so a pond that had earned a lot already isn't walked through
  them one at a time.
- What the frog says is a speech bubble pointing up at it, with the Next strip
  under it.
- **Days at the pond are a count and never a streak.** It's in settings in
  readout glass with a line saying when the next keepsake comes, and nothing
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
  a day pops it up as a reveal with the frog drawn the way `frog.js` draws
  any frog and the common thing it left by its feet, and a row in the
  greeting's list says who. A small muddy footprint sits on the front edge
  of the pad, by the frog's left foot, until the next day's visitor. A Stay
  home key in settings keeps a pond's own frog off everybody else's.
- **A frog's diary is in settings**, under its favourites. One line a day in
  the frog's own words about the day before, the day in printed caps down the
  left and the line beside it, newest first with a key for older days. It
  only says what happened and a day nobody came by reads as a quiet day.
- **A camera key makes a postcard.** A small dark key with an amber pixel
  camera sits over the top right corner of the glass. It draws the screen as
  it stands at twice the size on a cream card, with the frog's name in Space
  Grotesk, the pond's date and the gazette's weather word in JetBrains Mono
  and a pebble postmark, and opens it as a picture in a new tab. The hearts
  and the key aren't on the card.
- **The frog can be rubbed.** A drag back and forth over it shuts its eyes and
  throws pink pixel hearts, more of them on its favourite spot, and an egg
  only wobbles. Its bond is five small hearts over the top left corner of the
  glass, the one being worked on filled from the left. Nothing else is drawn
  in that corner or the top right one, and the bottom middle is the power plate's.
- **It does a thing for each heart.** It waves when the pond opens at one, and
  a tap croaks as ever and then does the newest thing it knows, a hop at two,
  a spin at three and at four something that suits its nature, a song, a big
  leap, a blush, a yawn with stars, a fly off its tongue or a bubble. At five
  it comes after a finger held on the water. All of it is the scene moving
  the same sprite, so `frog.js` stays as it is. Reduced motion gets the shut
  eyes and whatever is drawn over the frog, with no hop, tip or spin.
- **The pond fits a phone with no scroll.** The frog's screen with its power
  plate, its name with
  the rarity beside it, the bubble and the Next strip all sit above the keys
  at 412 by 915. Under 780 tall the screen narrows to `19rem` and the rows
  drop from 40px to 34px to keep them there. The folded gazette, Today at the pond and anything longer go below.
- **Dive is the whole window, on a phone and a desktop.** It covers the bar and
  the keys, the pond takes its depth and length from the window, and a strip
  under the water holds three readout tiles and `EJECT`. Ten ghost frogs swim
  each run behind your frog at 45% alpha, with their names over them where
  the field is wide enough to read ten. A ghost goes out on the stems or
  the rock of the gate it was dealt, in a burst of bubbles under a pink
  OUT, and is carried back with that gate as it fades.
- **The daily race is a card under the Arcade disks**, like the wheel's,
  with a chequered flag over the water, and it opens Dive on the day's
  course with the twelve residents as the ghosts and their names on the
  standings. Its title says whether this run goes on the board, and its
  results are the board, the whole finishing order with you in amber and
  a line saying whether the run was on the board or practice.
- **Dive is a race.** Three red lights and GO start it, a standings column
  down the left drops each frog to the bottom as it goes out, your place is a
  big italic number in the bottom right that pops as it climbs, with how
  many are racing and how many are still in under it in mono, and who just
  went out is called at the top. The results are a podium in gold, silver and
  bronze, the whole finishing order with you in amber, and the winnings
  counting up, with confetti for a podium place. All of it is HTML over the
  canvas, animates transform and opacity only, and takes no taps.
- The game page has no footer. The copyright and the source link are on the
  key page.
- Every sprite is drawn per pixel from a seed in `frontend/src/frog.js`. There
  are no image files, and a canvas is always `image-rendering: pixelated`.
- No pixel font. A bitmap face at body size was hard to read, so words on a
  canvas are the same two faces as the page.
- **Anything read is 14px or more.** 10px is for the labels on readout glass,
  the tab keys and a tier's box, and 12px for a key's legend and the caption
  under a picture in a slot or a disk, which would wrap at 14.
- Rarity is a word in a box as well as a colour: common grey, uncommon green,
  rare blue, epic purple, legendary amber.
- **The real day is on the pond's screen.** Night darkens it with fireflies,
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
  its favourite with a tag, In bag in amber, Can cook in green, or Need and
  the ingredient short, or Recipe at two hearts while it's a secret, then
  what it's worth. On the right is one key, Give and the points for the best
  dish in the bag for that neighbour, amber when it's the favourite, Cook to
  the kitchen with the favourite's two picked when there's nothing to give,
  or a green Fed tick once it's eaten today. The ones that can be fed come
  first and the fed ones last, dimmed, with filter keys over the list for
  not fed, a favourite ready, and all.
- **A neighbour's sheet is its hearts first.** Under the name it says the
  title and points, then a newsprint tag when the gazette names it, its
  thought as an italic cloud and what it says as a speech bubble. Then the
  ladder, a row for each heart with what it brings and the points it starts
  at, a tick on the ones had and the next one outlined in amber, then every
  dish in the bag with what it's worth to this neighbour and a Give key,
  then the facts in boxes.
- **A gift always answers.** A gift or a favour opens the neighbour's sheet
  on a reply in readout glass, the line it says, the points in amber, the
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
- **The reeds** are every frog as a card, the one out front first with an
  amber border and OUT FRONT, the rest with a key to swap. The egg bench and
  the pads sit under them. The register of looks is in the bag's collection.
- **A job is its own screen**, opened off a frog's card in the reeds. The frog
  works in a window drawn for the job, a net and bugs, a sickle in the reeds,
  a spade in the mud, a watering can in the beds, a dive for pebbles, a
  hammer at the craft bench, and it squashes as each action lands while the
  thing it got hops into a basket in the corner. Under the window is the
  action bar in readout glass with the time to the next one, then the basket
  as slots with counts and an amber key to take it, the level with a bar to
  the next and what that level opens, and the targets as keys with their
  time, xp, yield and misses. Each open target has a thin mastery bar under
  its numbers with its mastery in glass beside it, the bar dimmed while the
  frog's level holds it at the cap. Under the level sits the tool the frog
  works with, its picture, name and what it cuts, with a Shop key while
  there's a better one to buy, and a shaded one with who sells it before the
  pond has any. A locked target is its dark shape on hazard
  stripes with the level that opens it. The bar only fills and the basket
  only gains what the server dealt, and a full basket stops the bar and says
  so in amber and as a Next strip row.
- **Coming back is one card.** Taking baskets, and the greeting when one is
  full or half way there, opens a card in the bare sheet with a block a frog,
  how many actions and misses, what came in as slots, and the xp in readout
  glass beside the level. A level up pops a reveal after it with amber rays,
  the jump in readout glass over a slot for everything it opened, a target,
  a recipe, a find, a tool in town or the next step of mastery, each with
  what it is under its name. A pet or a 99 still pops its reveal after.
- **A working frog shows it.** In the reeds strip a frog at a job has its
  tool beside it, and in the Works picture the frog by the lodge holds its
  tool with the job's thing at its feet.
- **Frog power is a plate on the pond's screen.** A small glass plate sits
  on the bottom bezel of the frog's screen, half over the glass, with the
  rank in printed caps, the number in amber and a hairline of lit glass
  along its foot for how far to the next rung. It takes no height of its
  own. A tap opens the breakdown in the bare sheet, the number big in glass
  with the rank, a rail to the next rung, and every part as a row under
  Finds, Frogs and work and Games, its name, a line like 12 of 60 or best 40
  of 80, its points in amber and a bar along its foot for how much of that
  part is had.
- **Power only counts up.** When it rises the plate counts up to the new
  number once the pond is in view and no prize is over it, with a glass
  line over the plate saying what from, like +120, a new hat, that fades
  after a moment. A fall, like a game's reference rising, just shows the new
  number. A new rung is a reveal with amber rays, the rung's name and the
  number in a glass readout, after the count. Reduced motion gets the new
  number and the line with no count.
- **A frog's public page** is the frog in its screen, its power as one big
  amber Space Grotesk number with the rank beside it, the parts that make it
  up in the same three groups as the sheet, and a QR code for the link. The
  ranks are a ladder of nine under the number, the top rung first and amber
  up to where the frog stands, the same in settings. It ends on getting a
  frog of your own.
- **Visitors and stamps paint pictures.** Each one is a piece of a pond
  painting, twelve to a painting and four in all, the pond at dawn, in the
  rain, by lantern light and in the snow, each a small pixel scene in a
  wooden frame. Under the burrows the four sit in a row, a piece showing as
  a square of the picture over bare canvas and the frame gilding round from
  the top left as it fills. A finished one pops up as a reveal and hangs on
  the burrow wall in the Works picture as a small gold frame.
- **A frog's page has six stamps** under its name, a heart, a fly, a wave, a
  star, a leaf and a pebble, as a row of square keys with the stamp's pixel
  art on each. A visitor with a pond of its own presses one a day and it
  latches down, and the line under the row says what a visitor can do. The
  owner's next greeting pops up the stamps as a reveal, each stamper's frog
  with its stamp at its feet, and a frog kept home stamps as a dark shape.
- The card a share link unfurls with is a 1200 by 630 PNG drawn on the server,
  the screen on the left and the name, rarity and power on the right in
  JetBrains Mono, since that's the face the server has a TrueType file of.

## Per site

| Site | Phosphor | Notes |
|---|---|---|
| `analytics` | green | Bootstrap. Home, docs, dashboards. Four stat tiles |
| `status` | green | Bootstrap. No docs page yet, which is the one gap in the family |
| `logging` | green | Bootstrap. Dashboards run full width inside the container |
| `auth` | green | Bootstrap. Carries the starfield on `/login`, which the grid sits under |
| `repos` | green | Hand written CSS, no Bootstrap, since it is dense text. Newsreader on repository names |
| `chat` | green | Hand written CSS, no Bootstrap. A conversation, so it reads at the longest length on the estate. Citation pills and a source row under each answer, and widget panels above one when a ticker or a forecast was looked up. A picture draws behind a panel listing its stages, each with a count and a guess from the last few, and replaces it once it has loaded. An unread dot on a conversation that finished a turn off screen, and the place in line of every turn still waiting |
| `llm` | green | Hand written CSS, no Bootstrap. Behind the landing is a dashboard of keys, usage and the call log |
| `dash` | amber | Hand written CSS. Scanlines, vignette, JetBrains Mono and Space Grotesk. Panel 00 and panel 16 for chrome, a spine past 1900px. A `NOTIFY` switch in panel 00, and a `CHAT` readout beside it that only appears signed in |
| `pond` | amber | Hand written CSS. A game in the Deck direction, 80s hardware with keycaps, readout glass and colour pixel art in its screen windows. Dive is full screen on a phone |
| `blog`, `isaacbythewood.com` | neither | Separate identities. Nothing here applies to them |

