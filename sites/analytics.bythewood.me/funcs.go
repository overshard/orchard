package main

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
)

var templateFuncs = template.FuncMap{
	"dict":         dict,
	"json":         jsonBlock,
	"pct":          pct,
	"num":          formatNum,
	"add":          func(a, b int) int { return a + b },
	"hasPrefix":    strings.HasPrefix,
	"listHeadline": listHeadline,
	"weekDelta":    weekDelta,
	"tileCols":     tileCols,
	"cardHeadline": cardHeadline,
	"seq":          func(n int) []struct{} { return make([]struct{}, n) },
	"axisLabels":   axisLabels,
	"permille":     func(n int64) string { return fmt.Sprintf("%.1f%%", float64(n)/10) },
	"ticks":        ticks,
	"sumCount":     sumCounts,
	"list":         func(v ...any) []any { return v },
	"tab":          cardTab,
	"topCount": func(items []LabelCount) int64 {
		var top int64 = 1
		for _, i := range items {
			top = max(top, i.Count)
		}
		return top
	},
}

// dict lets a partial take more than one value, since a Go template has a
// single dot.
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

// pct renders count as a whole-number percentage of total, and 0 when total is
// zero.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
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

// ticks sizes each minute of the live panel against the busiest one.
func ticks(vals []int64) []int64 {
	var top int64 = 1
	for _, v := range vals {
		top = max(top, v)
	}
	out := make([]int64, len(vals))
	for i, v := range vals {
		out[i] = 4 + v*96/top
		if v == 0 {
			out[i] = 4
		}
	}
	return out
}

// CardTab is one tab on a panel that holds several lists. Empty dims it, so a
// tab with nothing behind it reads as empty before it's clicked.
type CardTab struct {
	ID    string
	Label string
	Empty bool
}

func cardTab(id, label string, items any) CardTab {
	t := CardTab{ID: id, Label: label}
	if l, ok := items.([]LabelCount); ok {
		t.Empty = len(l) == 0
	}
	return t
}
