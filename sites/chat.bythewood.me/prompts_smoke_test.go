package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"chat.bythewood.me/tools"
)

// Drives real turns against the real model and the real snapshot, so it runs
// only when told to. Set PROMPT_SMOKE=1, LLM_KEY, and optionally WIKI_URL.
func TestPromptsSmoke(t *testing.T) {
	if os.Getenv("PROMPT_SMOKE") == "" {
		t.Skip("set PROMPT_SMOKE=1 to drive the real model")
	}
	if base := os.Getenv("WIKI_URL"); base != "" {
		tools.WikiBase = base
	}
	llm := NewLLM(env("LLM_URL", "http://orchard-llm:8000"), env("LLM_MODEL", "local"), os.Getenv("LLM_KEY"))
	eng := NewEngine(llm, env("LLM_NAME", "Ornith 1.5 9B"))
	// main wires the markdown renderer, and the streaming path calls it.
	eng.Render = func(md string) string { return md }

	prompts := []string{
		"what is a goodyear welt",
		"what is kubernetes for",
		"tell me about photosynthesis",
		"what is postgresql",
		"who is kim jong un",
	}

	for _, p := range prompts {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		start := time.Now()
		reply, used, _, _, _, err := eng.Run(ctx, nil, p, "", "", func(Event) {})
		cancel()
		if err != nil {
			t.Errorf("%q: %v", p, err)
			continue
		}
		var calls []string
		for _, u := range used {
			s := u.Name
			if u.Err != "" {
				s += "(error: " + u.Err + ")"
			}
			calls = append(calls, s)
		}
		answer := strings.TrimSpace(reply.Content)
		t.Logf("\n--- %q  [%s]\ntools: %s\n%s\n",
			p, time.Since(start).Round(time.Millisecond),
			strings.Join(calls, ", "), truncate(answer, 700))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... [%d more chars]", len(s)-n)
}
