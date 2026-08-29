package main

import (
	"fmt"
	"html/template"
)

// templateFuncs is one function, so a partial can take more than one value. Go
// templates have a single dot, so a fragment needing both a label and a list
// has no other way to receive them.
var templateFuncs = template.FuncMap{"dict": dict}

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
