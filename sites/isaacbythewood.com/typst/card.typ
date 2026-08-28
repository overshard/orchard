// The social card, compiled to a 1200x630 PNG at build time.
//
// A card is a build artifact exactly like the resume PDF beside it: nothing
// about it can change while the process runs, so there is no reason to render
// it per request, and the runtime image stays a FROM scratch binary with no
// typst in it.
//
// PNG rather than SVG. The design would have been happier as vector, but
// Facebook, X, LinkedIn, Slack, iMessage and Discord all refuse
// image/svg+xml for og:image, so a vector card is a card nobody ever sees.
//
// At 72 ppi one Typst point is one pixel, so this page is literally the
// 1200x630 that og:image:width and og:image:height promise.

#set page(width: 1200pt, height: 630pt, margin: 0pt, fill: rgb("#20232e"))
#set text(font: ("Geist", "Liberation Sans"), fill: white)

// The site's own accent, and the same left rule the menu uses.
#place(dx: 80pt, dy: 80pt, rect(width: 5pt, height: 160pt, fill: rgb("#ff2d2d")))

#place(dx: 110pt, dy: 96pt, text(size: 82pt, weight: "bold", tracking: -2pt)[Isaac Bythewood])
#place(dx: 110pt, dy: 200pt, text(size: 34pt, fill: rgb("#9aa3b2"))[Senior Solutions Architect])

#place(dx: 80pt, dy: 470pt, line(length: 1040pt, stroke: 1pt + rgb("#3a3f4d")))
#place(dx: 80pt, dy: 508pt, text(size: 26pt, fill: rgb("#c9cede"))[isaacbythewood.com])
#place(dx: 80pt, dy: 552pt, text(size: 22pt, fill: rgb("#6f7789"))[Elkin, NC])
