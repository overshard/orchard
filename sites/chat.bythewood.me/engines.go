// How the engines under internal/ are handed to the tools and the model, which
// neither side can see from where it sits.
package main

import (
	"context"

	"chat.bythewood.me/internal/memory"
	"chat.bythewood.me/tools"
)

// asker hands the model to an engine that asks it one thing at a time.
type asker struct{ llm *LLM }

func (a asker) Ask(ctx context.Context, system, user string, maxTokens int) (string, error) {
	out, err := a.llm.Complete(ctx, []Message{
		{Role: RoleSystem, Content: system},
		{Role: RoleUser, Content: user},
	}, nil, maxTokens)
	if err != nil {
		return "", err
	}
	return out.Content, nil
}

// memoryStore hands the fact store to the remember tool. The names differ
// either side because the tool has only the one kind of row to keep straight.
type memoryStore struct{ m *memory.Store }

func (m memoryStore) Facts() ([]tools.MemoryFact, error) {
	all, err := m.m.Facts()
	if err != nil {
		return nil, err
	}
	out := make([]tools.MemoryFact, 0, len(all))
	for _, f := range all {
		out = append(out, tools.MemoryFact{ID: f.ID, Text: f.Text})
	}
	return out, nil
}

func (m memoryStore) Add(text string) (int64, error)      { return m.m.Add(text) }
func (m memoryStore) Replace(id int64, text string) error { return m.m.Replace(id, text) }
func (m memoryStore) Delete(id int64) error               { return m.m.Delete(id) }

// historyStore hands the conversation store to the chat_history tool. Separate
// from memoryStore because the two answer different questions and a tool that
// reads facts has no business reaching messages.
type historyStore struct{ s *Store }

func (h historyStore) SearchPast(query string, limit int) ([]tools.PastExchange, error) {
	hits, err := h.s.SearchHistory(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]tools.PastExchange, 0, len(hits))
	for _, x := range hits {
		out = append(out, tools.PastExchange{ConvID: x.ConvID, Title: x.Title,
			When: x.When, Question: x.Question, Answer: x.Answer})
	}
	return out, nil
}
