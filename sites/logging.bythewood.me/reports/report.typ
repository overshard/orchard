// The log report, rendered by text/template into Typst markup and compiled to
// PDF by typst.go. text/template rather than html/template, because "#" is
// Typst's function sigil and HTML escaping it would produce a document full of
// &num;. Escaping still happens, through typstMD, which knows what is dangerous
// in Typst rather than in HTML: the strings interpolated below are log messages
// and request paths written by other programs.

#let dim = rgb("#555")
#let muted = rgb("#888")
#let mono = ("JetBrains Mono", "DejaVu Sans Mono", "Liberation Mono")

#set page(
  paper: "a4",
  margin: (top: 14mm, bottom: 18mm, left: 14mm, right: 14mm),
  footer: context {
    set text(size: 7.5pt, fill: dim)
    grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      [Logging · self-hosted{{ if $.BaseURL }} · {{ typstMD $.BaseURL }}{{ end }} · {{ typstMD $.Title }} · {{ typstMD $.Dash.DateStart }} → {{ typstMD $.Dash.DateEnd }}],
      [Page #counter(page).display() of #counter(page).final().first()],
    )
  },
)

#set text(
  font: ("DejaVu Sans", "Liberation Sans", "Arial"),
  size: 9.5pt,
  fill: black,
)

#set par(leading: 0.5em, justify: false)

#show heading.where(level: 1): set text(size: 22pt, weight: "bold")
#show heading.where(level: 1): set block(above: 8pt, below: 4pt)

#show heading.where(level: 2): it => block(
  above: 16pt,
  below: 6pt,
  width: 100%,
  stroke: (bottom: 0.6pt + black),
  inset: (bottom: 3pt),
)[#text(size: 11pt, weight: "bold", tracking: 0.6pt, upper(it.body))]

#show heading.where(level: 3): it => block(
  above: 8pt,
  below: 3pt,
)[#text(size: 8.5pt, weight: "bold", tracking: 0.5pt, fill: rgb("#333"), upper(it.body))]

#grid(
  columns: (1fr, auto),
  align: (left + top, right + top),
  text(size: 8.5pt, tracking: 0.9pt, fill: dim, upper("// Logging · log report")),
  text(size: 8pt, fill: dim)[Generated {{ typstMD $.ReportedAt }}],
)

= {{ typstMD $.Title }}

#block(
  above: 8pt,
  below: 0pt,
  width: 100%,
  stroke: (top: 0.6pt + black, bottom: 0.6pt + black),
  inset: (top: 6pt, bottom: 6pt),
)[
  #grid(
    columns: (1fr, 1fr, 1fr),
    column-gutter: 16pt,
    row-gutter: 4pt,
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Window")) \
      #text(weight: "semibold")[{{ typstMD $.Dash.RangeName }}]
    ],
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Range · UTC")) \
      #text(weight: "semibold")[{{ typstMD $.Dash.DateStart }} → {{ typstMD $.Dash.DateEnd }}]
    ],
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Scope")) \
      #text(weight: "semibold")[{{ if $.Dash.Source }}{{ typstMD $.Dash.Source }}{{ else }}every source ({{ $.Dash.Totals.Sources }}){{ end }}]
    ],
  )
]

== Totals

// Counts come from the hourly rollups, which are kept forever, so this section
// is correct over any window. The percentiles below it are not: they read raw
// rows, which are kept for a fixed number of days, and the note says so rather
// than presenting a partial answer as a complete one.
#grid(
  columns: (1fr, 1fr, 1fr, 1fr),
  gutter: 4pt,
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[records]
    #v(2pt) #text(size: 14pt, weight: "bold")[{{ num $.Dash.Totals.Records }}]
    #v(1pt) #text(size: 7.5pt, fill: rgb("#333"))[{{ num $.Dash.Totals.Requests }} requests]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[error records]
    #v(2pt) #text(size: 14pt, weight: "bold")[{{ num $.Dash.Totals.Errors }}]
    #v(1pt) #text(size: 7.5pt, fill: rgb("#333"))[{{ num $.Dash.Totals.Warnings }} warnings]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[5xx rate]
    #v(2pt) #text(size: 14pt, weight: "bold")[{{ printf "%.2f" $.Dash.Totals.ErrorRate }}%]
    #v(1pt) #text(size: 7.5pt, fill: rgb("#333"))[{{ num $.Dash.Totals.Server5xx }} of {{ num $.Dash.Totals.Requests }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[direct hits]
    #v(2pt) #text(size: 14pt, weight: "bold")[{{ num $.Dash.Totals.DirectHits }}]
    #v(1pt) #text(size: 7.5pt, fill: rgb("#333"))[no CF-Ray header]
  ],
)

== Volume over time

{{ if $.Dash.ChartPolyline }}
// Two-row grid: row 1 is the Y-axis labels beside the SVG line, row 2 is the
// X-axis strip aligned under the SVG only. The geometry matches chartPolyline
// in dashboard.go: change one and change the other.
#grid(
  columns: (auto, 1fr),
  column-gutter: 4pt,
  rows: (90pt, auto),
  align: (right + horizon, left + horizon),
  block(height: 90pt)[
    #place(top + right)[#text(size: 6.5pt, fill: dim)[{{ $.Dash.ChartPeakCount }}]]
    #place(bottom + right)[#text(size: 6.5pt, fill: dim)[0]]
  ],
  image(
    bytes("<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 600 100' preserveAspectRatio='none'><line x1='0' y1='99.5' x2='600' y2='99.5' stroke='black' stroke-width='0.4'/><polyline fill='none' stroke='black' stroke-width='0.9' stroke-linejoin='round' stroke-linecap='round' points='{{ $.Dash.ChartPolyline }}'/></svg>"),
    format: "svg",
    width: 100%,
    height: 90pt,
  ),
  [],
  grid(
    columns: (1fr, 1fr, 1fr),
    align: (left, center, right),
    text(size: 7.5pt, fill: dim)[{{ typstMD $.Dash.ChartLabelStart }}],
    text(size: 7.5pt, fill: dim)[peak {{ $.Dash.ChartPeakCount }} · {{ typstMD $.Dash.ChartPeakLabel }}],
    text(size: 7.5pt, fill: dim)[{{ typstMD $.Dash.ChartLabelEnd }}],
  ),
)
{{ else }}
#text(size: 8pt, fill: muted, style: "italic")[No records in this range.]
{{ end }}

== Request latency

#v(-4pt)
#text(size: 8pt, fill: dim)[From raw lines, which are kept {{ $.Dash.Retained }} days. A window reaching further back charts correctly above but has fewer samples here.]

{{ if $.Dash.Latency.Count }}
#grid(
  columns: (1fr, 1fr, 1fr, 1fr, 1fr),
  gutter: 4pt,
  rect(width: 100%, stroke: 0.6pt + black, inset: 5pt)[
    #text(size: 7pt, fill: dim)[samples] #v(2pt) #text(size: 11pt, weight: "bold")[{{ num $.Dash.Latency.Count }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 5pt)[
    #text(size: 7pt, fill: dim)[p50] #v(2pt) #text(size: 11pt, weight: "bold")[{{ ms $.Dash.Latency.P50 }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 5pt)[
    #text(size: 7pt, fill: dim)[p95] #v(2pt) #text(size: 11pt, weight: "bold")[{{ ms $.Dash.Latency.P95 }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 5pt)[
    #text(size: 7pt, fill: dim)[p99] #v(2pt) #text(size: 11pt, weight: "bold")[{{ ms $.Dash.Latency.P99 }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 5pt)[
    #text(size: 7pt, fill: dim)[max] #v(2pt) #text(size: 11pt, weight: "bold")[{{ ms $.Dash.Latency.Max }}]
  ],
)
{{ else }}
#text(size: 8pt, fill: muted, style: "italic")[No requests with a measured duration in this range.]
{{ end }}

// breakable: false keeps each column's table atomic across pages: a column
// either fits on the current page or moves to the next as a unit, rather than
// leaving an orphan row under a re-printed header.
#let count_table(items, label_header: "Label", count_header: "Count") = {
  block(breakable: false, table(
    columns: (1fr, auto),
    align: (left + top, right + top),
    inset: (x: 3pt, y: 2pt),
    stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
    table.header(
      text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper(label_header)),
      text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper(count_header)),
    ),
    ..items,
  ))
}

{{ if $.Dash.Sources }}
== Sources

#block(breakable: false, table(
  columns: (1fr, auto, auto, auto, auto, auto, auto),
  align: (left + top, right + top, right + top, right + top, right + top, right + top, left + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Source")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Records")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Requests")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("4xx")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("5xx")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("p95")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Last seen")),
  ),
  {{ range $.Dash.Sources }}
  text(size: 7.5pt)[{{ typstMD .Source }}],
  text(size: 7.5pt)[{{ num .Records }}],
  text(size: 7.5pt)[{{ num .Requests }}],
  text(size: 7.5pt)[{{ num .Client4xx }}],
  text(size: 7.5pt)[{{ num .Server5xx }}],
  text(size: 7.5pt)[{{ ms .P95 }}],
  text(size: 7.5pt, fill: dim)[{{ if .LastSeen }}{{ typstMD .LastSeen }}{{ else }}—{{ end }}],
  {{ end }}
))
{{ end }}

{{ if $.Dash.Slowest }}
== Slowest paths

#v(-4pt)
#text(size: 8pt, fill: dim)[Ranked by p95 rather than by mean, with a floor of five samples: the endpoint that is fast a thousand times and terrible twice is the one worth finding.]

#block(breakable: false, table(
  columns: (1fr, auto, auto, auto, auto),
  align: (left + top, right + top, right + top, right + top, right + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Path")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Hits")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("p95")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Max")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("5xx")),
  ),
  {{ range $.Dash.Slowest }}
  text(font: mono, size: 7pt)[{{ typstMD .Path }}],
  text(size: 7.5pt)[{{ num .Count }}],
  text(size: 7.5pt)[{{ ms .P95 }}],
  text(size: 7.5pt)[{{ ms .Max }}],
  text(size: 7.5pt)[{{ num .Errors }}],
  {{ end }}
))
{{ end }}

== Breakdowns

#grid(
  columns: (1fr, 1fr, 1fr),
  column-gutter: 10pt,
  row-gutter: 8pt,
  {{ if $.Dash.ByLevel }}
  [
    === Levels
    #count_table(
      label_header: "Level", count_header: "Records",
      (
        {{ range $.Dash.ByLevel }}
        text(size: 7.5pt)[{{ typstMD .Label }}], text(size: 7.5pt)[{{ num .Count }}],
        {{ end }}
      ),
    )
  ],
  {{ end }}
  {{ if $.Dash.StatusClasses }}
  [
    === Status classes
    #count_table(
      label_header: "Class", count_header: "Requests",
      (
        {{ range $.Dash.StatusClasses }}
        text(size: 7.5pt)[{{ typstMD .Label }}], text(size: 7.5pt)[{{ num .Count }}],
        {{ end }}
      ),
    )
  ],
  {{ end }}
  {{ if $.Dash.ByComponent }}
  [
    === Components
    #count_table(
      label_header: "Component", count_header: "Records",
      (
        {{ range $.Dash.ByComponent }}
        text(size: 7.5pt)[{{ typstMD .Label }}], text(size: 7.5pt)[{{ num .Count }}],
        {{ end }}
      ),
    )
  ],
  {{ end }}
)

{{ if $.Dash.TopPaths }}
== Busiest paths

#count_table(
  label_header: "Path", count_header: "Hits",
  (
    {{ range $.Dash.TopPaths }}
    text(font: mono, size: 7pt)[{{ typstMD .Label }}], text(size: 7.5pt)[{{ num .Count }}],
    {{ end }}
  ),
)
{{ end }}

{{ if $.Dash.Errors }}
== Latest problems

#v(-4pt)
#text(size: 8pt, fill: dim)[Warnings, errors and 5xx responses, newest first.]

#block(breakable: true, table(
  columns: (auto, auto, auto, 1fr),
  align: (left + top, left + top, left + top, left + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Time")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Level")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("Source")),
    text(size: 6.5pt, fill: dim, weight: "bold", upper("What")),
  ),
  {{ range $.Dash.Errors }}
  text(font: mono, size: 6.5pt)[{{ typstMD .Time }}],
  text(size: 7pt)[{{ typstMD .Level }}],
  text(size: 7pt)[{{ typstMD .Source }}],
  text(size: 7pt)[{{ if eq .Msg "request" }}{{ .Status }} {{ typstMD .Method }} #text(font: mono)[{{ typstMD .Path }}] · {{ ms .DurationMS }}{{ else }}{{ if .Component }}[{{ typstMD .Component }}] {{ end }}{{ typstMD .Msg }}{{ end }}],
  {{ end }}
))
{{ end }}
