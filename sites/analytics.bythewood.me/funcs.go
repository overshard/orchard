package main

import (
	"fmt"
	"html/template"
	"strings"
)

// Template helpers.
//
// Short list on purpose. html/template is contextually aware, so the Rust
// era's hand-written Jinja2-faithful HTML formatter has no counterpart here:
// it existed only because minijinja escaped "/" to "&#x2f;" inside URLs, and
// all five Rust apps shipped a copy of it.
var templateFuncs = template.FuncMap{
	"dict":      dict,
	"json":      jsonBlock,
	"pct":       pct,
	"add":       func(a, b int) int { return a + b },
	"hasPrefix": strings.HasPrefix,
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

// pct renders count as a whole-number percentage of total, for the bar widths
// in the reports. Total is already floored at one by sumCounts, so the guard
// here is for any other caller.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
}
