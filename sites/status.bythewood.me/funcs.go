package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"
)

var templateFuncs = template.FuncMap{
	"dict":            dict,
	"json":            jsonBlock,
	"naturaltime":     naturalTime,
	"num":             formatNum,
	"msSavings":       msSavings,
	"urlPath":         urlPath,
	"pct":             pct,
	"pct1":            pct1,
	"metricClass":     metricClass,
	"scoreClass":      scoreClass,
	"uptimeClass":     uptimeClass,
	"lighthouseClass": lighthouseClass,
	"countClass":      countClass,
	"seq":             seq,
	"add":             func(a, b int) int { return a + b },
	"upper":           strings.ToUpper,
	"hasPrefix":       strings.HasPrefix,
	"listHeadline":    listHeadline,
	"count":           count,
	"secChecks":       secChecks,
	"secPass":         secPass,
	"insightTitle":    insightTitle,
	"lower":           strings.ToLower,
	"ints":            ints,
	"ring":            ring,
	"uptimeCell":      uptimeCell,
	"deref64f": func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	},
	"certKindP": func(d *int) string {
		if d == nil {
			return ""
		}
		return certKind(*d)
	},
	"secFirstFail": secFirstFail,
	"pushText":     pushText,
	"slowBy":       func(base int64) int64 { return max(20, base/2) },
	"signed":       func(n int64) string { return fmt.Sprintf("%+d", n) },
	"msClock":      func(ms int64) string { return time.UnixMilli(ms).UTC().Format("15:04:05") },
	"phaseText":    phaseText,
	"certClass":    certClass,
	"tickHeight":   func(v float64) int { return max(6, min(100, int(math.Round(v)))) },
	"pct2":         func(v *float64) string { return strconv.FormatFloat(*v, 'f', 2, 64) },
	"deref64": func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	},
}

// listHeadline is the properties page title, the state in a sentence.
func listHeadline(n, down int) template.HTML {
	sites := "sites"
	if n == 1 {
		sites = "site"
	}
	head := template.HTMLEscapeString(spell(n) + " " + sites)
	switch {
	case n == 0:
		return template.HTML("Nothing watched yet.")
	case down == 0:
		return template.HTML(head + ", <em>all up</em>.")
	case down == n:
		return template.HTML(head + `, <em class="bad">all down</em>.`)
	}
	return template.HTML(head + `, <em class="bad">` + strings.ToLower(spell(down)) + ` down</em>.`)
}

// ints widens an int64 series for the spark drawing.
func ints(v []int64) []float64 {
	out := make([]float64, len(v))
	for i, n := range v {
		out[i] = float64(n)
	}
	return out
}

// dict lets a partial take more than one value, a template has a single dot.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments (%d)", len(pairs))
	}
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i, pairs[i])
		}
		out[key] = pairs[i+1]
	}
	return out, nil
}

// jsonBlock renders a value for a <script type="application/json"> block. Marshal's
// HTML escaping is all that keeps a stored "</script>" from closing the element,
// since html/template can't help inside a non-JavaScript script type.
func jsonBlock(v any) (template.JS, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return template.JS(buf), nil
}

// naturalTime renders a timestamp as a relative phrase. A nil time is "never",
// which reads differently from a field that failed to load.
func naturalTime(t *time.Time) string {
	if t == nil {
		return "never"
	}

	d := time.Since(*t)
	// Future timestamps are normal here, next_run_at is one.
	suffix := "ago"
	if d < 0 {
		d = -d
		suffix = "from now"
	}

	// Rounded, since truncating turns 47h59m59.9s into "1 day from now".
	switch secs := int64(math.Round(d.Seconds())); {
	case secs < 60:
		if suffix == "ago" {
			return "just now"
		}
		return "in a moment"
	case secs < 3600:
		return plural(secs/60, "minute", suffix)
	case secs < 86_400:
		return plural(secs/3600, "hour", suffix)
	case secs < 86_400*30:
		return plural(secs/86_400, "day", suffix)
	case secs < 86_400*365:
		return plural(secs/(86_400*30), "month", suffix)
	default:
		return plural(secs/(86_400*365), "year", suffix)
	}
}

func plural(n int64, unit, suffix string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s %s", unit, suffix)
	}
	return fmt.Sprintf("%d %ss %s", n, unit, suffix)
}

// formatNum groups thousands with commas.
func formatNum(v any) string {
	var n int64
	switch t := v.(type) {
	case int:
		n = int64(t)
	case int64:
		n = t
	case *int64:
		if t == nil {
			return "0"
		}
		n = *t
	case float64:
		n = int64(t)
	default:
		return fmt.Sprint(v)
	}

	s := strconv.FormatInt(n, 10)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var b strings.Builder
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	if negative {
		return "-" + b.String()
	}
	return b.String()
}

// msSavings renders a Lighthouse saving as "1.2 s" or "420 ms". Zero renders as
// the empty string, so the template can show a placeholder instead.
func msSavings(v any) string {
	var ms float64
	switch t := v.(type) {
	case float64:
		ms = t
	case int64:
		ms = float64(t)
	case int:
		ms = float64(t)
	default:
		return ""
	}

	switch {
	case ms <= 0:
		return ""
	case ms >= 1000:
		return fmt.Sprintf("%.1f s", ms/1000)
	default:
		return fmt.Sprintf("%.0f ms", ms)
	}
}

// urlPath reduces an absolute URL to its path and query.
func urlPath(raw string) string {
	u, err := parseHTTPURL(raw)
	if err != nil {
		return raw
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path
}

// pct renders count as a whole-number percentage of total.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
}

// pct1 renders a nullable percentage with one decimal, keeping the column aligned.
func pct1(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}

// seq is a counted loop, which a template can't otherwise express since range
// needs something to range over.
func seq(n int) []struct{} { return make([]struct{}, n) }

// uptimeClass bands a recent-uptime percentage, higher is better. It takes a
// pointer because the value is nullable and a template cannot rebind the dot
// inside an {{if}}.
func uptimeClass(pct *float64) string {
	if pct == nil {
		return "muted"
	}
	switch {
	case *pct >= 99:
		return "ok"
	case *pct < 95:
		return "down"
	default:
		return "warn"
	}
}

// lighthouseClass bands the average of the Lighthouse scores at 90 and 80 instead
// of Lighthouse's 90 and 50, since an average of 60 across four categories is
// worse than 60 in one. scoreClass keeps Lighthouse's bands.
func lighthouseClass(score *int64) string {
	if score == nil {
		return "muted"
	}
	switch {
	case *score >= 90:
		return "ok"
	case *score >= 80:
		return "warn"
	default:
		return "down"
	}
}

// countClass bands a finding count. Lower is better, so the comparisons run the
// opposite way to uptimeClass.
func countClass(n, warnAbove, downAbove int) string {
	switch {
	case n > downAbove:
		return "down"
	case n > warnAbove:
		return "warn"
	default:
		return "ok"
	}
}

// metricClass bands one weighted metric's 0-to-1 score. A nil score means
// Lighthouse could not measure it, which is not the same as measuring it bad.
func metricClass(score *float64) string {
	switch {
	case score == nil:
		return "muted"
	case *score >= 0.9:
		return "green"
	case *score >= 0.5:
		return "amber"
	default:
		return "danger"
	}
}

// scoreClass maps a Lighthouse score to Lighthouse's own three bands, so the
// dashboard colours match the tool the numbers came from.
func scoreClass(score int64) string {
	switch {
	case score >= 90:
		return "good"
	case score >= 50:
		return "average"
	default:
		return "poor"
	}
}

// ring draws one Lighthouse score as a dial, coloured by the band it falls in.
func ring(label string, score int64) template.HTML {
	kind := "ok"
	switch {
	case score < 50:
		kind = "bad"
	case score < 90:
		kind = "warn"
	}
	const c = 150.8
	return template.HTML(fmt.Sprintf(`<div class="ring %s"><svg width="64" height="64" viewBox="0 0 58 58" aria-hidden="true">`+
		`<circle cx="29" cy="29" r="24" class="track"/><circle cx="29" cy="29" r="24" class="arc" stroke-dasharray="%.1f %.1f" transform="rotate(-90 29 29)"/>`+
		`<text x="29" y="34" text-anchor="middle">%d</text></svg><span>%s</span></div>`,
		kind, float64(score)/100*c, c, score, template.HTMLEscapeString(label)))
}

// uptimeCell is a percentage in a table cell, amber under the target.
func uptimeCell(v *float64) template.HTML {
	if v == nil {
		return `<span class="o-muted">&mdash;</span>`
	}
	cls := ""
	if *v < sloTarget*100 {
		cls = "o-warn"
	}
	return template.HTML(fmt.Sprintf(`<span class="%s">%.2f%%</span>`, cls, *v))
}

// certClass colours a cert's days left, amber under two weeks and red under one.
func certClass(days *int) string {
	if days == nil {
		return "o-muted"
	}
	switch certKind(*days) {
	case "bad":
		return "o-bad"
	case "warn":
		return "o-warn"
	}
	return ""
}

// pushText says whether ntfy took an incident's alert.
func pushText(sent *bool) string {
	switch {
	case sent == nil:
		return "pending"
	case *sent:
		return "sent"
	}
	return "failed"
}

// phaseText is a check's four phases in one cell, a dash for any that never ran.
func phaseText(c Check) string {
	parts := make([]string, 0, 4)
	for _, v := range []*int64{c.DNSMS, c.TCPMS, c.TLSMS, c.TTFBMS} {
		if v == nil {
			parts = append(parts, "-")
			continue
		}
		parts = append(parts, strconv.FormatInt(*v, 10))
	}
	return strings.Join(parts, "/")
}
