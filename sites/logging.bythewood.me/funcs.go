package main

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
)

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

// topCount is the denominator for the rank-list bars: the largest value rather
// than the sum, so a dominant entry does not flatten the tail into slivers.
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

// dict lets a partial take more than one value, which a single dot cannot.
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

// pct renders count as a whole-number percentage of total.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
}

// formatMS keeps two decimals under a millisecond, since most work here is
// sub-millisecond and would otherwise all render as "0ms".
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

// levelClass maps a slog level onto the palette, in one place so the chip, the
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

// statusClass does the same for an HTTP status; zero means not a request, and
// renders as no chip.
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
// thing. It returns template.URL because html/template would otherwise escape
// the "&" separators into entities inside an href.
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

// urlEscape percent-encodes everything that is not unreserved. Not
// url.QueryEscape, which encodes a space as "+".
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
