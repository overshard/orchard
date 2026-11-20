{{- /* The Markdown report. Rendered by text/template, not html/template, so a
       "*" stays a "*" instead of becoming an entity. See report.go.

       The whitespace control markers ({{- and -}}) are doing real work here in
       a way they are not in the HTML templates: Markdown is whitespace
       sensitive, and an {{if}} on its own line would otherwise leave a blank
       line that splits a list in two. */ -}}
{{- with .Property -}}
# {{ .Name }}

`{{ .URL }}`

## Snapshot

- Current status: **{{ .CurrentStatus }}**
- Average response: **{{ .AvgResponseTime }} ms**
- Recent uptime: {{ if .RecentUptimePct }}**{{ pct1 .RecentUptimePct }}%**{{ else }}n/a{{ end }}
- Total checks logged: **{{ .TotalChecks }}**
{{- with .CertDays }}
- TLS certificate: **{{ . }} days** left
{{- end }}

## Uptime

| Window | Uptime |
|---|---|
| 24h | {{ if .H.Up24 }}{{ pct2 .H.Up24 }}%{{ else }}n/a{{ end }} |
| 7d | {{ if .H.Up7 }}{{ pct2 .H.Up7 }}%{{ else }}n/a{{ end }} |
| 30d | {{ if .H.Up30 }}{{ pct2 .H.Up30 }}%{{ else }}n/a{{ end }} |
| 90d | {{ if .H.Up90 }}{{ pct2 .H.Up90 }}%{{ else }}n/a{{ end }} |
{{ if .H.HasSince }}
History is kept from {{ .H.Since }}.
{{- end }}

## Incidents

{{ if .H.Incidents -}}
| Started (UTC) | For | Cause | Failed checks |
|---|---|---|---|
{{ range .H.Incidents }}| {{ .Start.Format "2006-01-02 15:04" }} | {{ .Length }}{{ if not .End }}+{{ end }} | {{ .Code }}, {{ .Cause }} | {{ .Fails }} |
{{ end }}
{{- else -}}
_No incidents on record._
{{ end }}
## Lighthouse

{{ with .LighthouseScores -}}
{{ range .Pairs }}- {{ .Label }}: **{{ .Score }}**
{{ end }}
{{- else -}}
_No lighthouse data._
{{ end }}
## Security headers

| Header | State |
|---|---|
| HTTPS | {{ if .IsHTTPS }}yes{{ else }}no{{ end }} |
| HSTS (1y+) | {{ if .HasHSTS }}yes{{ else }}no{{ end }} |
| HSTS preload | {{ if .HasHSTSPreload }}yes{{ else }}no{{ end }} |
| X-Frame-Options | {{ if .HasClickjackProtection }}set{{ else }}missing{{ end }} |
| X-Content-Type-Options | {{ if .HasContentSniffProtection }}nosniff{{ else }}missing{{ end }} |
| Server header hidden | {{ if .HidesServerVersion }}yes{{ else }}no{{ end }} |

## SEO insights

{{ if .CrawlerInsights -}}
{{ range .CrawlerInsights }}- [{{ .Severity }}] [{{ .Type }}] {{ .Issue }}: `{{ .URL }}`
{{ end }}
{{- else -}}
_No crawl results yet._
{{ end }}
{{- end }}
