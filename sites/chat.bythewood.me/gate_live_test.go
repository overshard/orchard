package main

import (
	"context"
	"os"
	"testing"
	"time"

	"chat.bythewood.me/tools"
)

// Drives the real gate against the real model and snapshot.
func TestGateLive(t *testing.T) {
	if os.Getenv("PROMPT_SMOKE") == "" {
		t.Skip("set PROMPT_SMOKE=1")
	}
	if base := os.Getenv("WIKI_URL"); base != "" {
		tools.WikiBase = base
	}
	llm := NewLLM(env("LLM_URL", "http://orchard-llm:8000"), env("LLM_MODEL", "local"), os.Getenv("LLM_KEY"))
	eng := NewEngine(llm, "Ornith 1.5 9B")

	cases := []struct {
		name, question, draft string
		wantBack              bool
	}{
		{"wrong origin", "what is a goodyear welt",
			"A goodyear welt is named after the Goodyear tyre company, which invented it in 1920 to make shoes resoleable.", true},
		{"right origin", "what is a goodyear welt",
			"A goodyear welt is a strip of leather running around the outsole. The machine behind it was invented in 1862 by Auguste Destouy and improved by Daniel Mills, both working for Charles Goodyear Jr.", false},
		{"wrong definition", "what is postgresql",
			"PostgreSQL is a proprietary NoSQL document database made by Oracle, and it does not support SQL or transactions.", true},
		{"right definition", "what is postgresql",
			"PostgreSQL is a free and open-source relational database management system that emphasises extensibility and SQL compliance, with ACID transactions.", false},
	}

	for _, c := range cases {
		bg := eng.background(context.Background(), c.question)
		if bg == "" {
			t.Errorf("%s: no background found for %q", c.name, c.question)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		nudge, _ := eng.gate(ctx, c.question, c.draft, nil, nil, func(Event) {})
		cancel()
		sentBack := nudge != ""
		status := "let through"
		if sentBack {
			status = "sent back: " + nudge
		}
		if sentBack != c.wantBack {
			t.Errorf("%s: sentBack = %v, want %v (%s)", c.name, sentBack, c.wantBack, status)
		} else {
			t.Logf("%s: %s", c.name, status)
		}
	}
}
