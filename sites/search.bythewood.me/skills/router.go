package skills

import (
	"context"
	"fmt"
	"strings"
)

// None is the routing answer meaning no skill claims this, go and search.
const None = "none"

// Model is the slice of the LLM the router needs. Narrow on purpose, so the
// package does not depend on the client and a test can hand over a stub.
type Model interface {
	Structured(ctx context.Context, system, user string, maxTokens int, schema any, out any) error
}

// The two things a question can want. This is the first half of the decision
// and it is an enum rather than free text, which is the whole trick: a 4B asked
// to write its reasoning writes a paragraph that argues its way to one answer
// and then emits another, and capping the paragraph only truncates it before it
// concludes. Both halves constrained means the second is conditioned on a token
// the model actually committed to rather than on prose it can contradict.
const (
	WantsValue = "a current value, price, reading, or the score or result of a match"
	WantsFact  = "a fact, definition, explanation, opinion or set of instructions"
)

// Route is the router's verdict.
type Route struct {
	Wants string `json:"wants"`
	Skill string `json:"skill"`
}

// Why is what the decision came down to, for the log line.
func (r Route) Why() string { return r.Wants }

// Decide picks the skill for a question, or None.
//
// This is the first model call in the pipeline. Classification is the thing a
// 4B is genuinely good at, and the enum is enforced in the grammar rather than
// asked for in the prompt, so the model cannot name a skill that does not
// exist. What it can still do is pick the wrong one, which is what the
// negative triggers on each card are for.
func (r *Registry) Decide(ctx context.Context, m Model, question string) Route {
	names := append(r.Names(), None)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wants": map[string]any{"type": "string", "enum": []string{WantsValue, WantsFact}},
			"skill": map[string]any{"type": "string", "enum": names},
		},
		// wants first, so the model commits to what the question is before the
		// handler names are in front of it.
		"required":             []string{"wants", "skill"},
		"additionalProperties": false,
	}

	var out Route
	if err := m.Structured(ctx, r.systemPrompt(), question, 160, schema, &out); err != nil {
		return Route{Skill: r.match(question), Wants: "routing call failed, matched on keywords"}
	}
	out.Skill = strings.TrimSpace(out.Skill)
	if out.Skill == "" || (out.Skill != None && r.Get(out.Skill) == nil) {
		return Route{Skill: r.match(question), Wants: "routing returned an unknown skill"}
	}
	// The two fields can still disagree, and when they do the first one is the
	// one to trust, because it was decided before the handler names were in
	// front of it.
	if out.Wants == WantsFact {
		out.Skill = None
	}
	return out
}

// systemPrompt is built from the cards rather than written out, so a skill
// cannot be added without the router learning about it.
func (r *Registry) systemPrompt() string {
	var b strings.Builder
	b.WriteString("You route a question to the one handler that answers it from a live source, or to none.\n\n")
	b.WriteString("Handlers:\n\n")
	for _, s := range r.skills {
		c := s.Card()
		fmt.Fprintf(&b, "%s: %s\n", c.Name, c.Does)
		if len(c.Fires) > 0 {
			fmt.Fprintf(&b, "  picks up: %s\n", strings.Join(quoteAll(c.Fires), "; "))
		}
		if len(c.NotFor) > 0 {
			fmt.Fprintf(&b, "  not this one: %s\n", strings.Join(quoteAll(c.NotFor), "; "))
		}
		b.WriteString("\n")
	}
	b.WriteString(None + ": everything else, which is most questions. Anything needing explanation, background, instructions, opinion or more than one source.\n\n")
	b.WriteString(strings.Join([]string{
		"First say what the question wants.",
		"\"" + WantsValue + "\" is a question a handler above can answer outright, and it is the minority of questions.",
		"\"" + WantsFact + "\" is everything else, and it always means " + None + ".",
		"Naming a thing a handler deals in is not enough. Which currency a country uses is a fact and not a conversion, how many calories are in a banana is a fact and not arithmetic, and how betting odds work is a fact and not a price.",
		"Then name the handler.",
		"Choose one only when it answers the whole question on its own, and when the question matches one of its \"not this one\" examples choose " + None + ".",
		"When two could fit, choose " + None + ".",
	}, " "))
	return b.String()
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = `"` + s + `"`
	}
	return out
}

// match is the offline fallback. It runs when the model is unreachable, and
// being blunt is the point: a wrong skill here is recoverable because every
// skill still verifies it can actually answer before claiming the question.
func (r *Registry) match(question string) string {
	l := strings.ToLower(question)
	for _, s := range r.skills {
		c := s.Card()
		for _, k := range c.Keywords {
			if strings.Contains(l, k) {
				return c.Name
			}
		}
	}
	return None
}

// Run routes and then executes, and reports which skill answered.
//
// A skill returning no result is not an error, it is a skill declining, and
// the caller falls through to the web. That happens when the router was right
// about the subject and the upstream had nothing, which is common enough that
// treating it as a failure would show an error for a question the pipeline can
// still answer.
func (r *Registry) Run(ctx context.Context, m Model, question string, d Deps) (*Result, string) {
	route := r.Decide(ctx, m, question)
	if route.Skill == None || route.Skill == "" {
		return nil, None
	}
	s := r.Get(route.Skill)
	if s == nil {
		return nil, None
	}
	res, err := s.Run(ctx, question, d)
	if err != nil || res == nil {
		return nil, route.Skill
	}
	return res, route.Skill
}
