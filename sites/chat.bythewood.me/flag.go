package main

import (
	"context"
	"regexp"

	"chat.bythewood.me/tools"
)

// Asking for a conversation to be looked at again is a flag on it and not a
// fact about him. Written down as one, "Isaac corrected me on TheBurntPeanut"
// was recalled into questions about tailscale and dash for the rest of the day.
var flagAsk = regexp.MustCompile(`(?i)\b(remember|note|flag|mark|bookmark|save)\b[^.?!]{0,40}\b(this|the)\s+(chat|conversation|thread)\b|` +
	`\b(check|look at|come back to|fix|review|revisit)\b[^.?!]{0,30}\b(this|the)\s+(chat|conversation|thread)\b|` +
	`\b(this|the)\s+(chat|conversation|thread)\b[^.?!]{0,30}\b(later|when i get home|tomorrow)\b|` +
	`\bremember\b[^.?!]{0,30}\b(got (this|that|it) wrong|(was|were) wrong|messed (this|that|it) up)\b|` +
	`\b(so we can|to|and) (fix|look at|check) (this|it|that)( one)? later\b`)

const flaggedReply = "Flagged this conversation to come back to."

// flagTurn answers a flag ask without the model, in the shape a turn returns.
func (s *site) flagTurn(convID string) func(context.Context, []Message, string, string, string, *Trace, func(Event)) (Message, []tools.Result, []Source, []tools.Widget, Stats, error) {
	return func(_ context.Context, _ []Message, user, _, _ string, tr *Trace, emit func(Event)) (Message, []tools.Result, []Source, []tools.Widget, Stats, error) {
		if err := s.store.SetFlag(convID, user); err != nil {
			return Message{}, nil, nil, nil, Stats{}, err
		}
		tr.Add(Step{Kind: "memory", Label: "flagged the conversation to come back to", In: user,
			Out: "no fact written, since this is about the conversation rather than about him"})
		emit(Event{Kind: "block", HTML: s.render(flaggedReply)})
		return Message{Role: RoleAssistant, Content: flaggedReply}, nil, nil, nil, Stats{}, nil
	}
}
