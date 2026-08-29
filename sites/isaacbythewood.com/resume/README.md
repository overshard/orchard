# resume

One page, Letter, compiled by the Typst CLI and published at
`/static/pdfs/resume-isaac-bythewood.pdf`, linked from the About page.

```
content.yml   every word of it, as data
resume.typ    the layout
```

## Building it

```sh
make resume                          # from this site's directory
make build SITE=isaacbythewood.com   # runs it as part of the site build
```

Either writes `frontend/public/pdfs/resume-isaac-bythewood.pdf`, which Vite
copies into the bundle alongside the favicons. The output is gitignored; the two
files above are the source. In the image it is a build stage, so no compiler
ships at runtime.

`make resume` skips with a warning when `typst` is not installed, the same way
the blog treats its post PDFs. Only that one file is missing when it does.

## Two things to know before editing it

**The font is named `Liberation Sans` on purpose.** The design it was ported
from asked for `"Helvetica Neue", Helvetica, Arial`, which resolved to
Liberation Sans on the machine that produced the published PDF. Naming it
directly is what keeps the metrics identical rather than merely similar. The
Docker stage installs `ttf-liberation` for the same reason.

**CSS `line-height` and Typst `leading` are not the same quantity**, and this is
the one thing that will quietly ruin the layout. A CSS line box is the full
`line-height` with the glyphs centred in it; Typst's is the font's own
ascender-to-descender extent, and `leading` only adds space *between* lines.
Mapping one to the other directly comes out cramped on every line and short on
every margin. `line-box()` in `resume.typ` puts the half-leading back into the
text edges, which lets `leading` be zero and every margin be the original
number unchanged.

The measurements are transcribed rather than reinterpreted, with one conversion
applied throughout: a CSS pixel is 1/96in and a Typst point is 1/72in, so a CSS
px is 0.75pt. `resume.typ` keeps a `px()` helper so those numbers still read as
they were written.
