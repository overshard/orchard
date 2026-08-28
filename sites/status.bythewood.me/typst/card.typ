// The social card, compiled to a 1200x630 PNG at build time.
//
// PNG rather than SVG because every social platform refuses image/svg+xml for
// og:image, so a vector card is a card nobody ever sees. At 72 ppi one Typst
// point is one pixel, so this page is literally the size og:image:width and
// og:image:height promise.
//
// This is the only Typst in the repo that is not a report: the same compiler
// the PDF route already needs at runtime, used once at build.

#set page(width: 1200pt, height: 630pt, margin: 0pt, fill: rgb("#0d1117"))
#set text(font: ("Geist", "Liberation Sans"), fill: rgb("#f0f6fc"))

#place(dx: 80pt, dy: 80pt, rect(width: 5pt, height: 130pt,
  fill: gradient.linear(rgb("#0e3ff4"), rgb("#842bff"), angle: 90deg)))

#place(dx: 110pt, dy: 92pt, text(size: 76pt, weight: "bold", tracking: -2pt)[Status])
#place(dx: 110pt, dy: 190pt, text(size: 30pt, fill: rgb("#9aa3b2"))[Self-hosted uptime monitoring])

#place(dx: 80pt, dy: 470pt, line(length: 1040pt, stroke: 1pt + rgb("#30363d")))
#place(dx: 80pt, dy: 508pt, text(size: 24pt, fill: rgb("#c9d1d9"))[status.bythewood.me])
#place(dx: 80pt, dy: 550pt, text(size: 20pt, fill: rgb("#6f7789"))[Uptime, response times, Lighthouse audits and crawl findings])
