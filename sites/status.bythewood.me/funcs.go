package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"
)

// Template helpers.
//
// Short list on purpose. html/template is contextually aware, so the Rust
// era's hand-written Jinja2-faithful HTML formatter has no counterpart here:
// it existed only because minijinja escaped "/" to "&#x2f;" inside URLs, and
// all five Rust apps shipped a copy of it.
//
// The url_for function is gone too. It mapped twelve endpoint names to
// hardcoded path strings, which is indirection with nothing on the other side
// of it: the paths are still hardcoded, just one file further away, and a typo
// in an endpoint name became a runtime error instead of a broken link the
// reader could see. Templates now write "/properties" and mean it.
var templateFuncs = template.FuncMap{
	"dict":            dict,
	"json":            jsonBlock,
	"naturaltime":     naturalTime,
	"intcomma":        intcomma,
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
}

// dict lets a partial take more than one value.
//
// Go templates have a single dot, so a fragment needing both a label and a
// list has no way to receive them.
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

// jsonBlock renders a value for a <script type="application/json"> block,
// which is how the chart data reaches Chart.js.
//
// SetEscapeHTML(false) plus template.JS is the pair that makes this correct,
// and getting either half wrong is a bug the browser reports strangely.
// encoding/json escapes <, > and & into < and friends by default, which
// is protection against exactly this situation and is also unnecessary here
// because the value is being placed by html/template, which knows it is inside
// a script block and escapes for that context itself. Leaving both on produces
// doubly-escaped JSON that parses into corrupted strings.
func jsonBlock(v any) (template.JS, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return template.JS(strings.TrimSpace(buf.String())), nil
}

// naturalTime renders a timestamp the way Django's humanize filter did, which
// is what the templates were written against.
//
// A nil time renders as "never" rather than as an empty cell, because on this
// dashboard the difference between "no crawl has ever run" and "the field did
// not load" matters.
func naturalTime(t *time.Time) string {
	if t == nil {
		return "never"
	}

	d := time.Since(*t)
	// Future timestamps are normal here, not an error: next_run_at is one, and
	// the templates render it with the same filter.
	suffix := "ago"
	if d < 0 {
		d = -d
		suffix = "from now"
	}

	// Rounded, not truncated. A timestamp 48 hours in the future is 47h59m59.9s
	// away by the time this runs, and truncating that reports "1 day from now".
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

// intcomma groups thousands with commas.
func intcomma(v any) string {
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

// msSavings renders a Lighthouse saving as "1.2 s" or "420 ms".
//
// Zero renders as the empty string so the template can show a placeholder,
// which is the Django filter's contract the property template still relies on.
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

// urlPath reduces an absolute URL to its path, so the crawler insight table
// can list twenty findings without twenty copies of the same origin.
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

// pct1 renders a nullable percentage with one decimal place.
//
// The decimal is not decoration, it is parity: Rust's f64 Display always emits
// one, so the Rust dashboard reads "100.0%" where Go's default formatting of
// the same value reads "100%". Found by diffing the two reports on 2026-08-26.
// Matching matters because these numbers are compared across the cutover.
func pct1(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}

// seq is a counted loop, which Go templates otherwise have no way to express:
// range needs something to range over. Used only to draw placeholder ticks for
// a property that has never been probed.
func seq(n int) []struct{} { return make([]struct{}, n) }

// uptimeClass bands a recent-uptime percentage. Higher is better.
//
// 99 and 95 are the thresholds because they are the ones the operator reacts
// to: below 99 something happened this week, below 95 something is wrong now.
//
// It takes a pointer because the value is nullable and Go templates cannot
// rebind the dot inside an {{if}}: the template tests the pointer and passes
// the same pointer here, rather than dereferencing it in the markup.
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

// lighthouseClass bands the average of the four Lighthouse scores.
//
// The thresholds are 90 and 80, not Lighthouse's own 90 and 50: this is an
// average of four categories, and a site averaging 60 has something badly
// wrong even though 60 is "average" for a single category. scoreClass keeps
// Lighthouse's bands for the individual scores.
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

// countClass bands a finding count. Lower is better, which is why the
// comparisons run the opposite way to uptimeClass.
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

// metricClass bands one weighted performance metric's 0-to-1 score.
//
// A nil score means Lighthouse could not measure that metric on this page,
// which is not the same as measuring it as bad, so it renders neutral.
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
// colours on the dashboard match the colours in the tool the numbers came
// from.
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
