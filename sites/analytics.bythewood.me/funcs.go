package main

import (
	"fmt"
	"html/template"
	"strings"
)

var templateFuncs = template.FuncMap{
	"dict":      dict,
	"json":      jsonBlock,
	"pct":       pct,
	"add":       func(a, b int) int { return a + b },
	"hasPrefix": strings.HasPrefix,
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

// pct renders count as a whole-number percentage of total, and returns 0 rather
// than dividing by zero.
func pct(count, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return count * 100 / total
}
