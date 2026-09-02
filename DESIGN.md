# DESIGN.md

What every site here looks like and why. Read this before changing anything
visual, and update it when a decision changes.

There is no shared stylesheet and there is not going to be one. Every site owns
its own copy of its CSS the same way it owns its own copy of `web/`, so keeping
these sites looking alike is a thing people do on purpose by following this
file, not a thing a build step does. A change to the palette is a change in
five places.

## Two phosphors

Terminals before colour used one of two coatings, P3 amber and P1 green, and
that is the split the estate runs on.

- **dash is amber.** It is the only public page with no login, it is glanced at
  rather than read, and it is the one site allowed to be a costume.
- **analytics, auth, logging, repos and status are green.** They are tools you
  read numbers off for minutes at a time. Green is the primary, and amber is
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
Green at `0.028` alpha on the five tools, amber at `0.022` on dash, which is
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

All five tools open the same way, and the home page is an advert for running the
thing yourself rather than a dashboard. It ends by pointing at the source.

The skeleton, in order:

1. **Eyebrow.** A pill with a green dot, then version, stage, and `self-hosted`.
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

**Series colours are punchier than the UI palette, on purpose.** Chrome can be
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

`repos` defined its buttons only inside `.hero-ctas` until 2026-09-01, so the
one on its 404 fell back to a bare element rule and matched nothing.

**Borders, in three weights.** `0.2` for a rule between bands, `0.3` for the
navbar and the edges of a page section, and `0.42` for a card or panel. Nothing
sits below `0.2`. The navbar was on `0.06` and the footer bar on `0.04` until
2026-09-01, which is invisible rather than subtle.

**Caps labels are one solid amber,** `#c9a84c`, at `0.7rem` and `0.12em`
tracking. The footer headings ran at `0.55` alpha and the section labels at
`0.75`, which read as two different colours side by side.

**Focus and selection.** `:focus-visible` is a 2px `#6b9e78` outline at 2px
offset, and `::selection` is `rgba(107, 158, 120, 0.3)`. Only `repos` had either
until 2026-09-01, so tabbing through a form on the other four got whatever
Bootstrap did per component.

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

## The 404

Identical on all four, and it uses the description the handler already sets
rather than writing its own line:

```
404 · NOT FOUND        amber caps section label
No such page           heading
That page does not exist.
← Home                 ghost button
```

Left aligned like every other page here. `logging` centred its own version with
a different sentence and a filled button until 2026-09-01, and `repos` had a
bare `404` with an inline link.

## Rules that are easy to get wrong

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
home page. status called it `intcomma` until 2026-09-01.

**A template listed with no file behind it crash-loops.** Deleting a page means
deleting it from `pageTemplates` in the same commit. `web.NewRenderer` resolves
the list at boot and not at build, so the image builds fine and then dies on
start with `pattern matches no files`.

**Chrome width has to match content width.** logging's navbar and footer were
`container-fluid` while its home page was `container`, so the hero sat narrower
than the bar above it. All four use `container` now, and only logging's
dashboard pages go full width inside it.

**Do not cite this file, or anything under `code/memory`, from a comment.** This
repository is public and the vault is not.

## Per site

| Site | Phosphor | Notes |
|---|---|---|
| `analytics` | green | Bootstrap. Home, docs, dashboards. Four stat tiles |
| `status` | green | Bootstrap. No docs page yet, which is the one gap in the family |
| `logging` | green | Bootstrap. Dashboards run full width inside the container |
| `auth` | green | Bootstrap. Carries the starfield on `/login`, which the grid sits under |
| `repos` | green | Hand written CSS, no Bootstrap, since it is dense text. Newsreader on repository names |
| `dash` | amber | Hand written CSS. Scanlines, vignette, JetBrains Mono and Space Grotesk |
| `blog`, `isaacbythewood.com` | neither | Separate identities. Nothing here applies to them |
