package main

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
)

// Template helpers.
var templateFuncs = template.FuncMap{
	"dict":        dict,
	"json":        jsonBlock,
	"pct":         pct,
	"add":         func(a, b int) int { return a + b },
	"sub":         func(a, b int) int { return a - b },
	"hasPrefix":   strings.HasPrefix,
	"ms":          formatMS,
	"num":         formatNum,
	"levelClass":  levelClass,
	"statusClass": statusClass,
	"query":       queryString,
	"topCount":    topCount,
	"isLoopback":  isLoopback,
}

// topCount is the denominator the rank-list bars are drawn against: the largest
// value in the list rather than the sum. A list where one entry holds ninety
// percent would otherwise render every other bar as an invisible sliver, and
// the shape of the tail is the part worth seeing.
func topCount(items []LabelCount) int64 {
	var top int64
	for _, i := range items {
		if i.Count > top {
			top = i.Count
		}
	}
	if top == 0 {
		return 1
	}
	return top
}

// dict lets a partial take more than one value. Go templates have a single dot,
// so a fragment needing both a label and a list has no other way to receive
// them.
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

// pct renders count as a whole-number percentage of total, for the bar widths.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
}

// formatMS renders a duration the way somebody reads it rather than the way it
// is stored. Sub-millisecond work is most of what these sites do, and rendering
// 0.42ms as "0ms" would make every panel claim the server is instant.
func formatMS(v float64) string {
	switch {
	case v <= 0:
		return "—"
	case v < 1:
		return strconv.FormatFloat(v, 'f', 2, 64) + "ms"
	case v < 1000:
		return strconv.FormatFloat(v, 'f', 1, 64) + "ms"
	default:
		return strconv.FormatFloat(v/1000, 'f', 2, 64) + "s"
	}
}

// formatNum groups thousands. Log counts get large enough that 1284339 and
// 128433 look the same at a glance, which is the failure this avoids.
func formatNum(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// levelClass maps a slog level onto the palette: green for the ordinary, amber
// for a warning, terracotta for an error. One function so the level chip, the
// row tint and the chart legend cannot disagree.
func levelClass(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR":
		return "lv-error"
	case "WARN", "WARNING":
		return "lv-warn"
	case "DEBUG":
		return "lv-debug"
	default:
		return "lv-info"
	}
}

// statusClass does the same for an HTTP status. Zero means the record is not a
// request at all, which reads as no chip rather than as a 0xx.
func statusClass(status int64) string {
	switch {
	case status == 0:
		return ""
	case status >= 500:
		return "st-5xx"
	case status >= 400:
		return "st-4xx"
	case status >= 300:
		return "st-3xx"
	default:
		return "st-2xx"
	}
}

// queryString builds a link that keeps the current window while changing one
// thing, so a range picked on the overview survives a click into a source.
//
// It returns template.URL rather than a string because the value is assembled
// here and correctly escaped here; leaving it a string makes html/template
// escape the "&" separators into entities inside an href.
func queryString(pairs ...string) template.URL {
	if len(pairs)%2 != 0 {
		return template.URL("")
	}
	var parts []string
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		parts = append(parts, urlEscape(pairs[i])+"="+urlEscape(pairs[i+1]))
	}
	if len(parts) == 0 {
		return template.URL("")
	}
	return template.URL("?" + strings.Join(parts, "&"))
}

// urlEscape percent-encodes everything that is not unreserved. Written out
// rather than calling url.QueryEscape because that encodes a space as "+",
// which is right for a form body and wrong inside a query value a person will
// read in the address bar.
func urlEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
