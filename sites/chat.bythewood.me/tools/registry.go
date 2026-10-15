package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Default is every tool the chat is offered, and the schemas and the dispatch
// table are both built from it so a tool can't exist in one and not the other.
func Default() *Registry {
	r := &Registry{}
	for _, t := range []Tool{
		WebSearch, WebFetch, News, Remember, Weather, Markets, SportsScores, Odds, MusicLookup, Convert, Calc, Now,
		Wikipedia, Dictionary,
		OrchardLogs, OrchardStatus, OrchardAnalytics, OrchardRepos, OrchardCode, OrchardDash,
		ChatHistory, XSearch,
		PropertyTool, Mortgage,
		Image,
	} {
		r.Add(t)
	}
	return r
}

type Registry struct {
	byName map[string]Tool
	order  []string
}

func (r *Registry) Add(t Tool) {
	if r.byName == nil {
		r.byName = map[string]Tool{}
	}
	r.byName[t.Name] = t
	r.order = append(r.order, t.Name)
	sort.Strings(r.order)
}

func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.byName[name]; return t, ok }
func (r *Registry) Names() []string              { return append([]string(nil), r.order...) }

// Without drops one schema from an offer. Taking a tool off the table stops
// something the harness can't afford twice, since a rule in the prompt is only
// a request.
func Without(schemas []map[string]any, name string) []map[string]any {
	out := make([]map[string]any, 0, len(schemas))
	for _, s := range schemas {
		if fn, ok := s["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); n == name {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// Schemas renders the registry as the OpenAI tools array.
func (r *Registry) Schemas() []map[string]any {
	out := make([]map[string]any, 0, len(r.order))
	for _, n := range r.order {
		t := r.byName[n]
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Schema,
			},
		})
	}
	return out
}

// Call returns a failed tool's error as content instead of ending the turn,
// because "the search engine is refusing us" is something the model should say
// out loud.
func (r *Registry) Call(ctx context.Context, turn *Turn, name string, raw json.RawMessage) Result {
	start := time.Now()
	res := Result{Name: name, Args: raw}
	t, ok := r.Get(name)
	if !ok {
		res.Err = fmt.Sprintf("no tool named %q", name)
		res.Content = map[string]any{"error": res.Err}
		res.Elapsed = time.Since(start)
		return res
	}
	var args map[string]any
	if len(raw) > 0 {
		// Running the tool on no arguments would blame a missing field, when
		// what the model has to fix is the length of the call.
		if err := json.Unmarshal(raw, &args); err != nil {
			res.Err = "the arguments were cut off or are not valid JSON, call it again with shorter arguments"
			res.Content = map[string]any{"error": res.Err}
			res.Elapsed = time.Since(start)
			return res
		}
	}
	out, err := t.Run(ctx, turn, args)
	if err != nil {
		res.Err = err.Error()
		res.Content = map[string]any{"error": err.Error()}
	} else {
		res.Content = out
	}
	res.Elapsed = time.Since(start)
	return res
}

// Only narrows an offer to the named tools, for a turn with one job to do where
// every other tool is a way to get lost.
func Only(schemas []map[string]any, names ...string) []map[string]any {
	var out []map[string]any
	for _, s := range schemas {
		if fn, ok := s["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); slices.Contains(names, n) {
				out = append(out, s)
			}
		}
	}
	return out
}
