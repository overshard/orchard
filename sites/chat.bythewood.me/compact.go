// Compaction. A small model degrades on a long conversation, so the old turns
// become a summary and the recent ones stay verbatim.
//
// The rule that shapes this: llama.cpp caches the prompt prefix it has already
// processed, and a rewrite of the history throws that away. Measured on this
// stack a repeated 7,661 token prefix reprocessed 4 tokens instead of all of
// them, 68ms against 2,535ms. So compaction happens at a threshold and not
// every turn, and the summary sits at the front where it stays put between
// compactions rather than moving every message.
package main

import (
	"context"
	"fmt"
	"strings"
)

const (
	// Roughly four characters to a token, which is close enough to decide when
	// to compact and cheaper than asking the server to count.
	charsPerToken = 4

	// Compact when the conversation would fill more than this share of the
	// window. Well below full, because the answer still needs room to be
	// generated into.
	compactAtFraction = 0.55

	// Never summarise the last few exchanges. A follow-up like "why?" refers
	// to them and a summary cannot carry that.
	keepVerbatim = 6
)

type Compactor struct {
	llm       *LLM
	ctxTokens int
}

func NewCompactor(llm *LLM, ctxTokens int) *Compactor {
	if ctxTokens <= 0 {
		ctxTokens = 32768
	}
	return &Compactor{llm: llm, ctxTokens: ctxTokens}
}

func (c *Compactor) budgetChars() int {
	return int(float64(c.ctxTokens) * compactAtFraction * charsPerToken)
}

// Window turns a stored conversation into what the model is handed. It returns
// the messages plus whether it compacted, so the caller can persist a new
// summary rather than recomputing one every turn.
func (c *Compactor) Window(ctx context.Context, conv Conversation, stored []Stored) (msgs []Message, summary string, covered int, changed bool) {
	summary, covered = conv.Summary, conv.Summarize
	if covered > len(stored) {
		covered = 0
		summary = ""
	}

	size := 0
	for _, m := range stored[covered:] {
		size += len(m.Content)
	}
	size += len(summary)

	if size > c.budgetChars() && len(stored)-covered > keepVerbatim {
		cut := len(stored) - keepVerbatim
		if cut > covered {
			if s, err := c.summarise(ctx, summary, stored[covered:cut]); err == nil && strings.TrimSpace(s) != "" {
				summary, covered, changed = s, cut, true
			}
		}
	}

	if strings.TrimSpace(summary) != "" {
		msgs = append(msgs, Message{Role: RoleUser,
			Content: "Earlier in this conversation:\n" + summary})
		msgs = append(msgs, Message{Role: RoleAssistant,
			Content: "Understood, I have that context."})
	}
	for _, m := range stored[covered:] {
		if m.Role != RoleUser && m.Role != RoleAssistant {
			continue
		}
		msgs = append(msgs, Message{Role: m.Role, Content: m.Content})
	}
	return msgs, summary, covered, changed
}

// summarise rewrites the whole summary rather than appending to it, so it stops
// growing without bound. What it is told to keep is what a follow-up actually
// needs: decisions, facts established, and anything the user asked for that has
// not been delivered yet.
func (c *Compactor) summarise(ctx context.Context, prev string, older []Stored) (string, error) {
	var b strings.Builder
	if strings.TrimSpace(prev) != "" {
		b.WriteString("Existing summary of even earlier turns:\n")
		b.WriteString(prev)
		b.WriteString("\n\n")
	}
	b.WriteString("Conversation to fold in:\n")
	for _, m := range older {
		who := "User"
		if m.Role == RoleAssistant {
			who = "Assistant"
		}
		body := m.Content
		if len(body) > 3000 {
			body = body[:3000] + " ..."
		}
		fmt.Fprintf(&b, "%s: %s\n\n", who, body)
	}

	msgs := []Message{
		{Role: RoleSystem, Content: "You compress a conversation so it can continue in a smaller window. " +
			"Write one replacement summary covering everything given, not a summary of the summary. Keep: what the user " +
			"is trying to do, decisions made, facts and numbers established, names and urls that were settled on, their " +
			"stated preferences, and anything they asked for that is still outstanding. Drop pleasantries and anything " +
			"superseded later. Write plain sentences under bold labels, no more than 200 words, and never invent detail " +
			"that is not in the text."},
		{Role: RoleUser, Content: b.String()},
	}
	out, err := c.llm.Complete(ctx, msgs, nil, 600)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Content), nil
}

// Title asks for a short name for a conversation, once, off the first exchange.
//
// The answer is passed as well as the question because a question is often too
// short to name on its own. "VXUS" alone was titled "Video game streaming
// service", when the reply beside it said plainly that it is a Vanguard ETF.
func (c *Compactor) Title(ctx context.Context, first, answer string) string {
	var b strings.Builder
	b.WriteString("Name the conversation below. Do not answer it.\n\nMessage:\n<<<\n")
	b.WriteString(trim(first, 500))
	b.WriteString("\n>>>")
	if a := strings.TrimSpace(answer); a != "" {
		b.WriteString("\n\nThe reply it got, which is what the message turned out to be about:\n<<<\n")
		b.WriteString(trim(a, 700))
		b.WriteString("\n>>>")
	}
	msgs := []Message{
		{Role: RoleSystem, Content: "You name conversations. You never answer the message you are given. " +
			"Reply with a noun phrase of three to six words naming the subject, no quotes, no trailing " +
			"period, and none of the words chat, conversation, question or help. " +
			"Where the message is short or is an abbreviation, a ticker or a name, take what it refers to " +
			"from the reply rather than guessing at it."},
		{Role: RoleUser, Content: b.String()},
	}
	out, err := c.llm.Complete(ctx, msgs, nil, 30)
	if err != nil {
		return ""
	}
	// A model that answers with a sentence and then explains itself still gave
	// a usable name on its first line, so take that rather than throwing the
	// whole thing away and falling back to the raw question.
	t := strings.TrimSpace(out.Content)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	// A model asked for a title sometimes answers with a markdown heading.
	t = strings.Trim(strings.TrimSpace(t), "\"'.:*#- ")
	if t == "" {
		return ""
	}
	return trim(titleWords(t), 48)
}

// titleWords caps a title at six words. A model that answered the question
// instead of naming it still opens on the subject, so the first six words are a
// usable name where the whole sentence is not.
func titleWords(t string) string {
	f := strings.Fields(t)
	if len(f) <= 6 {
		return strings.Join(f, " ")
	}
	return strings.Join(f[:6], " ")
}
