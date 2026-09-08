package tools

// Searching earlier conversations.
//
// The other half of memory. Memory keeps one sentence facts about Isaac, this
// reaches what was actually said, which matters because a tool result dies with
// the turn that fetched it and only the answer survives. Without it a question
// about something settled last week had nothing to reach for.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PastExchange is one earlier question and its answer.
type PastExchange struct {
	ConvID   string    `json:"conversation_id"`
	Title    string    `json:"title"`
	When     time.Time `json:"when"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
}

// History is chat's own conversation store, an interface for the same reason
// Memory is: this package cannot see the database the site owns.
type History interface {
	SearchPast(query string, limit int) ([]PastExchange, error)
}

var ChatHistory = Tool{
	Name: "chat_history",
	Description: "Search earlier conversations with Isaac for what was actually said. Use it when he " +
		"refers to something from another chat, asks what was decided or discussed before, or when a " +
		"question only makes sense against something already settled. It returns whole exchanges, so " +
		"quote the answer as something said earlier rather than as a fresh fact, and check anything " +
		"that has since changed. It does not search the web. Read only.",
	Schema: obj(map[string]any{
		"query": str("what to look for, the subject words rather than the whole question"),
		"n":     integer("how many exchanges to return, default 6"),
	}, "query"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		if d.History == nil {
			return nil, fmt.Errorf("the conversation history is not available in this turn")
		}
		q := strings.TrimSpace(argStr(a, "query"))
		if q == "" {
			return nil, fmt.Errorf("query is required, and it should be the subject rather than the whole question")
		}
		n := int(argNum(a, "n", 6))
		if n < 1 || n > 15 {
			n = 6
		}
		hits, err := d.History.SearchPast(q, n)
		if err != nil {
			return nil, err
		}
		if len(hits) == 0 {
			return map[string]any{"query": q, "exchanges": []any{}, "count": 0,
				"note": "Nothing earlier matched. Say so rather than inventing what was said, " +
					"and answer the question on its own."}, nil
		}
		return map[string]any{"query": q, "exchanges": hits, "count": len(hits),
			"note": "These are earlier exchanges, not current facts. Anything dated or priced in " +
				"them may have moved since, so look it up again before repeating it."}, nil
	},
}
