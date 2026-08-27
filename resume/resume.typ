// The resume, as Typst.
//
// It replaces a Chromium: the Next.js era rendered an HTML template through
// headless Playwright and printed it to PDF, which meant a browser, a Markdown
// parser and two npm parsing libraries in the build path of a one page
// document. Every other PDF in this repo is already Typst, so this is the one
// that was out of step.
//
// The design is a port, not a redesign. Every number below is the old
// stylesheet's, converted once: CSS pixels are 1/96in and Typst points are
// 1/72in, so a CSS px is 0.75pt and that factor is the whole translation. The
// original PDF embedded Liberation Sans, because "Helvetica Neue, Helvetica,
// Arial" resolved to it on the machine that generated it, so naming Liberation
// Sans directly is what keeps the metrics identical rather than merely close.

#let data = yaml("content.yml")

// One CSS pixel. Kept as a function so the stylesheet's numbers can be
// transcribed as they were written instead of pre-multiplied by hand.
#let px(n) = n * 0.75pt

// Liberation Sans, in em above and below the baseline. These are the font's
// own metrics and they are what makes the line maths below exact rather than
// tuned by eye.
#let ascent = 0.9053
#let descent = 0.2119

// CSS `line-height`, reproduced.
//
// This is the one place the two systems genuinely disagree and it is why a
// first attempt at this port came out vertically cramped on every line. A CSS
// line box is `line-height` tall and the glyphs sit in the middle of it, with
// the leftover split evenly above and below (half-leading). Typst's line box
// is the font's own ascender-to-descender extent, and `leading` adds space
// only BETWEEN lines, never above the first or below the last.
//
// So block heights came out short by the half-leading at both ends, and every
// margin measured from a block edge landed too tight. Setting the text edges
// to include the half-leading makes a Typst line box the same height as a CSS
// one, which lets `leading` go to zero and every margin below be the
// stylesheet's number unmodified.
#let line-box(line-height) = (
  top-edge: (ascent + (line-height - ascent - descent) / 2) * 1em,
  bottom-edge: -(descent + (line-height - ascent - descent) / 2) * 1em,
)

#let ink = rgb("#1a1a1a")
#let heading-ink = rgb("#111111")
#let summary-ink = rgb("#444444")
#let meta-ink = rgb("#666666")
#let bullet-ink = rgb("#333333")
#let accent = rgb("#2563eb")
#let sidebar-bg = rgb("#1e293b")
#let sidebar-ink = rgb("#e2e8f0")
#let sidebar-muted = rgb("#cbd5e1")
#let sidebar-label = rgb("#94a3b8")
#let sidebar-rule = rgb("#334155")
#let link-ink = rgb("#93c5fd")
#let contact-muted = rgb("#dbeafe")

#let sidebar-width = px(240)
#let page-width = px(816)
#let page-height = px(1056)

#set page(width: page-width, height: page-height, margin: 0pt)
#set text(
  font: "Liberation Sans",
  size: 9.5pt,
  fill: ink,
  // The stylesheet never hyphenated, because browsers do not unless asked.
  // Typst does, so leaving it on breaks words in the narrow right column that
  // the reference PDF keeps whole.
  hyphenate: false,
  ..line-box(1.35),
)
// Zero, because line-box above already carries the full line height. Anything
// here would be added on top of it.
#set par(leading: 0pt, spacing: 0pt)
#show link: it => it

// A sidebar section: a muted uppercase label over a hairline, then content.
#let sidebar-section(label, body) = block(width: 100%)[
  #block(below: px(3))[
    #block(below: px(3))[
      #text(size: 9.5pt, weight: "bold", tracking: px(0.5), fill: sidebar-label)[
        #upper(label)
      ]
    ]
    #line(length: 100%, stroke: px(1) + sidebar-rule)
  ]
  #body
]

// The dark right column. Placed rather than laid out in the flow, because it
// is full bleed to three edges and the main column has to sit beside it, which
// is what `position: absolute` bought in the stylesheet.
#place(
  top + right,
  block(width: sidebar-width, height: page-height, fill: sidebar-bg)[
    // `place(right)` sets the alignment for everything inside it too, so
    // without this every line in the column is flush right.
    #set align(left)
    #block(
      width: 100%,
      fill: accent,
      inset: (top: px(36), right: px(24), bottom: px(20), left: px(24)),
    )[
      #block(below: px(4))[
        #text(size: 11pt, weight: "bold", fill: sidebar-ink)[#data.title]
      ]
      #block(below: px(8))[
        #text(size: 9pt, fill: contact-muted)[#data.location]
      ]
      #block(below: px(2))[
        #text(size: 9pt, weight: "bold", fill: white)[#data.phone]
      ]
      #text(size: 9pt, fill: white)[
        #link("mailto:" + data.email)[#data.email]
      ]
    ]

    #block(inset: (x: px(24), y: px(14)), width: 100%)[
      // The stylesheet used flex `gap: 12px`, which is space between children
      // and not after the last one.
      #let sections = (
        sidebar-section("Links")[
          #for l in data.links [
            #block(below: px(1))[
              #text(size: 9pt, fill: link-ink)[#link(l.url)[#l.label]]
            ]
          ]
        ],
        sidebar-section("Skills")[
          #for s in data.skills [
            #block(below: px(1))[#text(size: 9pt, fill: sidebar-muted)[#s]]
          ]
        ],
        // Inline and comma joined, which the stylesheet did with
        // `display: inline` plus an `::after` comma on all but the last.
        sidebar-section("Technologies")[
          #text(size: 9pt, fill: sidebar-muted)[#data.technologies.join(", ")]
        ],
        sidebar-section("Education")[
          #for e in data.education [
            #text(size: 9pt, fill: sidebar-muted)[
              #text(weight: "bold", fill: sidebar-ink)[#e.institution,]
              #e.years \
              #e.degree
            ]
          ]
        ],
      )
      #for (i, s) in sections.enumerate() [
        #block(below: if i + 1 == sections.len() { 0pt } else { px(12) })[#s]
      ]
    ]
  ],
)

// The main column. `margin-right: 240px` in the stylesheet, so its box is the
// page less the sidebar and its own padding sits inside that.
#block(
  width: page-width - sidebar-width,
  inset: (top: px(36), right: px(32), bottom: px(36), left: px(38)),
)[
  #block(below: px(5))[
    #text(size: 26pt, weight: "bold", fill: heading-ink, ..line-box(1.1))[
      #data.name
    ]
  ]

  #block(below: px(14))[
    #text(size: 9.5pt, fill: summary-ink, ..line-box(1.4))[#data.summary]
  ]

  #for (i, section) in data.sections.enumerate() [
    // `margin-top: 12px`, except on the first heading.
    #block(above: if i == 0 { 0pt } else { px(12) }, below: px(8))[
      #block(below: px(2))[
        #text(size: 10.5pt, weight: "bold", tracking: px(0.5), fill: accent)[
          #upper(section.heading)#if "note" in section {
            text(size: 8.5pt, weight: "regular", tracking: 0pt)[ — #section.note]
          }
        ]
      ]
      #line(length: 100%, stroke: px(1) + accent)
    ]

    #for entry in section.entries [
      #block(below: px(8), width: 100%)[
        #block(below: px(1))[
          #text(size: 10.5pt, fill: heading-ink)[
            #text(weight: "bold")[#entry.org —]
            #text(weight: "regular", style: "italic")[#entry.role]
          ]
        ]
        #block(below: px(3))[
          #text(size: 8.5pt, fill: meta-ink)[#entry.meta]
        ]
        #set text(size: 9pt, fill: bullet-ink)
        #list(
          // `ul { padding-left: 16px }` with an outside marker, so the body
          // edge is 16px in and the disc hangs back into that space.
          indent: px(3),
          body-indent: px(6),
          spacing: px(1),
          marker: text(size: 9pt)[•],
          ..entry.bullets,
        )
      ]
    ]
  ]
]
