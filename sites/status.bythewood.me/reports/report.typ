{{- /* Property report, rendered by text/template into Typst markup and then
       compiled to PDF by the typst CLI (see typst.go). Mirrors analytics'
       report.typ: monochrome, hairline rules, letterspaced uppercase section
       headers, mono for technical strings.

       Every value that came from a monitored site goes through typstMD, which
       escapes Typst's markup characters. That includes "/", because "//"
       starts a Typst line comment and a URL is full of them: without it, a
       tracked URL would silently swallow the rest of its line. */ -}}
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
      [Status · self-hosted · {{ typstMD .BaseURL }} · {{ typstMD .Property.Name }}],
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

// Header strip
#grid(
  columns: (1fr, auto),
  align: (left + top, right + top),
  text(size: 8.5pt, tracking: 0.9pt, fill: dim, upper("// Status · property report")),
  text(size: 8pt, fill: dim)[Generated {{ typstMD .GeneratedAt }}],
)
{{ with .Property }}
= {{ typstMD .Name }}

// Meta dl: 2-col with thin rules above and below.
#block(
  above: 8pt,
  below: 0pt,
  width: 100%,
  stroke: (top: 0.6pt + black, bottom: 0.6pt + black),
  inset: (top: 6pt, bottom: 6pt),
)[
  #grid(
    columns: (1fr, 1fr),
    column-gutter: 16pt,
    row-gutter: 4pt,
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Property ID")) \
      #text(font: mono, size: 8.5pt)[{{ typstMD .ID }}]
    ],
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("URL")) \
      #text(font: mono, size: 8.5pt)[{{ typstMD .URL }}]
    ],
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Alert state")) \
      #text(weight: "semibold")[{{ typstMD .AlertState }}]
    ],
    [
      #text(size: 7.5pt, tracking: 0.4pt, fill: dim, upper("Visibility")) \
      #text(weight: "semibold")[{{ if .IsPublic }}public{{ else }}private{{ end }}]
    ],
  )
]

== Live signals

#grid(
  columns: (1fr, 1fr, 1fr, 1fr),
  gutter: 4pt,
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[STATUS]
    #v(2pt)
    #text(size: 14pt, weight: "bold")[{{ .CurrentStatus }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[AVG RESPONSE]
    #v(2pt)
    #text(size: 14pt, weight: "bold")[{{ .AvgResponseTime }} ms]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[UPTIME]
    #v(2pt)
    #text(size: 14pt, weight: "bold")[{{ if .RecentUptimePct }}{{ pct1 .RecentUptimePct }}%{{ else }}—{{ end }}]
  ],
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[CHECKS]
    #v(2pt)
    #text(size: 14pt, weight: "bold")[{{ .TotalChecks }}]
  ],
)

== Lighthouse
{{ with .LighthouseScores }}
#grid(
  columns: (1fr, 1fr, 1fr, 1fr),
  gutter: 4pt,
  {{- range .Pairs }}
  rect(width: 100%, stroke: 0.6pt + black, inset: 6pt)[
    #text(size: 7.5pt, tracking: 0.4pt, fill: dim)[{{ typstMD (upper .Label) }}]
    #v(2pt)
    #text(size: 14pt, weight: "bold")[{{ .Score }}]
  ],
  {{- end }}
)
{{ else }}
#text(size: 8pt, fill: muted, style: "italic")[No lighthouse data yet.]
{{ end }}
{{- with .LighthouseDetails }}
{{- if .Metrics }}
=== Performance metrics

#block(breakable: false, table(
  columns: (auto, 1fr, auto),
  align: (left + top, left + top, right + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Metric")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Title")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Value")),
  ),
  {{- range .Metrics }}
  text(font: mono, size: 7pt, weight: "bold")[{{ typstMD .Acronym }}], text(size: 7.5pt)[{{ typstMD .Title }}], text(size: 7.5pt)[{{ typstMD .DisplayValue }}],
  {{- end }}
))
{{ end }}
{{- if .Opportunities }}
=== Top opportunities

#block(breakable: false, table(
  columns: (1fr, auto),
  align: (left + top, right + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Opportunity")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Savings")),
  ),
  {{- range .Opportunities }}
  text(size: 7.5pt)[{{ typstMD .Title }}], text(size: 7.5pt)[{{ typstMD (msSavings .SavingsMS) }}],
  {{- end }}
))
{{ end }}
{{- end }}

== Security

#block(breakable: false, table(
  columns: (1fr, auto),
  align: (left + top, right + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Check")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Result")),
  ),
  text(size: 7.5pt)[HTTPS], text(size: 7.5pt)[{{ if .IsHTTPS }}OK{{ else }}Issue{{ end }}],
  text(size: 7.5pt)[TLS certificate valid], text(size: 7.5pt)[{{ if .InvalidCert }}Issue{{ else }}OK{{ end }}],
  text(size: 7.5pt)[HSTS (≥1y max-age)], text(size: 7.5pt)[{{ if .HasHSTS }}OK{{ else }}Issue{{ end }}],
  text(size: 7.5pt)[HSTS preload], text(size: 7.5pt)[{{ if .HasHSTSPreload }}OK{{ else }}Issue{{ end }}],
  text(size: 7.5pt)[X-Frame-Options], text(size: 7.5pt)[{{ if .HasClickjackProtection }}OK{{ else }}Issue{{ end }}],
  text(size: 7.5pt)[X-Content-Type-Options], text(size: 7.5pt)[{{ if .HasContentSniffProtection }}OK{{ else }}Issue{{ end }}],
  text(size: 7.5pt)[Server header hidden], text(size: 7.5pt)[{{ if .HidesServerVersion }}OK{{ else }}Issue{{ end }}],
))
{{ end }}
== SEO insights
{{ if .InsightGroups }}
{{- range .InsightGroups }}
=== {{ typstMD .Type }}

#block(breakable: false, table(
  columns: (auto, 1fr, 1fr),
  align: (left + top, left + top, left + top),
  inset: (x: 3pt, y: 2pt),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + black) } else { (bottom: 0.3pt + rgb("#ddd")) },
  table.header(
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Severity")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("Issue")),
    text(size: 6.5pt, tracking: 0.3pt, fill: dim, weight: "bold", upper("URL")),
  ),
  {{- range .Items }}
  text(size: 7.5pt, weight: "bold")[{{ typstMD .Severity }}], text(size: 7.5pt)[{{ typstMD .Issue }}], text(font: mono, size: 7pt, fill: dim)[{{ typstMD .URL }}],
  {{- end }}
))
{{ end }}
{{- else }}
#text(size: 8pt, fill: muted, style: "italic")[No crawl results yet.]
{{ end }}
