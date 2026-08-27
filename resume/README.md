# resume

Isaac's resume: one page, Letter, compiled by the Typst CLI.

It sits at the repo root rather than inside `sites/isaacbythewood.com/` because
it is a document rather than part of a website. That site is only where it
happens to be published today, at `/static/pdfs/resume-isaac-bythewood.pdf`,
linked from the About page.

```
content.yml   every word of it, as data
resume.typ    the layout
```

## Building it

```
make resume                          # from the repo root
make build SITE=isaacbythewood.com   # runs it as part of the site build
```

Either writes `sites/isaacbythewood.com/frontend/public/pdfs/resume-isaac-bythewood.pdf`,
which Vite copies into `dist/` with the favicons. The output is gitignored: the
two files above are the source. In the image it is a build stage, so no
compiler ships at runtime and nothing has to be committed.

`make resume` skips with a warning if `typst` is not installed, the same way
the blog treats its post PDFs. Only that one file is missing when it does.

## What this replaced

A headless Chromium. The Next.js era kept `resume/content.md` with YAML
frontmatter, rendered the Markdown body with `marked`, substituted it into
`template.html`, and printed the result to PDF through Playwright. The template
had to guess what each paragraph meant, recognising a date line by matching a
regular expression for a capitalised month name, and an entry heading by where
it sat between other headings. `content.yml` names those fields instead, so
nothing is inferred from how a line happens to be written.

Every other PDF in this repo was already Typst. This was the one that was out
of step, and it carried Playwright, `marked` and `gray-matter` on its own.

## Keeping the design

It is a port. The old stylesheet's numbers are transcribed rather than
reinterpreted, with one conversion applied throughout: a CSS pixel is 1/96in
and a Typst point is 1/72in, so a CSS px is 0.75pt. `resume.typ` keeps a `px()`
helper so those numbers still read as they were written.

Two things are worth knowing before editing it:

- **The font is named `Liberation Sans` deliberately.** The stylesheet asked
  for `"Helvetica Neue", Helvetica, Arial`, and on the machine that generated
  the published PDF that resolved to Liberation Sans, which is what
  `pdffonts` reports it embedding. Naming it directly is what makes the metrics
  identical rather than merely similar. The Docker stage installs
  `ttf-liberation` for the same reason.

- **CSS `line-height` and Typst `leading` are not the same quantity**, and this
  is the one thing that will silently ruin the layout. A CSS line box is the
  full `line-height` with the glyphs centred in it; Typst's is the font's own
  ascender-to-descender extent, and `leading` only adds space *between* lines.
  A first pass that mapped one to the other came out cramped on every line and
  short on every margin. `line-box()` in `resume.typ` puts the half-leading
  back into the text edges, which lets `leading` be zero and every margin be
  the stylesheet's number unchanged.

Checked against the published PDF after porting: the section rules land within
one pixel at 96dpi, the contact block and sidebar edges are exact, and the
extracted text is the same words in the same order.
