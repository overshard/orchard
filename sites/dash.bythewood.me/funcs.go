package main

import (
	"html/template"
	"slices"
)

var templateFuncs = template.FuncMap{
	// The sparkline paths are computed in spark.go and go straight into a d
	// attribute, so they need the type that says a template may write them
	// without escaping. Everything else on this page is text and stays escaped.
	"path": func(d string) template.HTMLAttr { return template.HTMLAttr(d) },

	// range gives a zero based index and the feed ranks read from one.
	"inc": func(i int) int { return i + 1 },

	// dict builds a map inline, which is the only way a template can pass a
	// named set of values to a shared define. Three gauges with six fields each
	// is the point at which repeating the markup three times stops being the
	// simpler option.
	"dict": func(pairs ...any) map[string]any {
		out := make(map[string]any, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			key, ok := pairs[i].(string)
			if !ok {
				continue
			}
			out[key] = pairs[i+1]
		}
		return out
	},

	// The ON AIR panel draws the week Monday first and lights the days he
	// usually streams, which the state carries as a list of names.
	"weekdays": func() []string { return []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"} },
	"in":       slices.Contains[[]string],

	// Valve's own review bands, so the colour on a rating matches the word the
	// store puts next to the same number.
	"band": func(pct int) string {
		switch {
		case pct >= 80:
			return "good"
		case pct >= 70:
			return "mixed"
		case pct >= 40:
			return "poor"
		default:
			return "bad"
		}
	},
}
