// Every round of a turn sends the whole history again, so a long conversation's
// older turns become a summary. Rewriting the history costs a call, so it runs at
// a threshold and not every turn, and the summary stays at the front.
package main

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"chat.bythewood.me/internal/history"
)

const (
	// Roughly four characters to a token, which is close enough to decide when
	// to compact and cheaper than asking the server to count.
	charsPerToken = 4

	// Compact past this many tokens of history. The window holds far more, but
	// a turn resends it every round and past 100K tokens a call bills at five
	// times the rate, so this leaves room for the turn's own reading.
	compactAtTokens = 40000

	// Never summarise the last few exchanges. A follow-up like "why?" refers
	// to them and a summary cannot carry that.
	keepVerbatim = 6
)

type Compactor struct {
	llm *LLM
}

func NewCompactor(llm *LLM) *Compactor { return &Compactor{llm: llm} }

func (c *Compactor) budgetChars() int { return compactAtTokens * charsPerToken }

// Window turns a stored conversation into what the model is handed. It returns
// the messages plus whether it compacted, so the caller can persist a new
// summary instead of recomputing one every turn.
func (c *Compactor) Window(ctx context.Context, conv history.Conversation, stored []Stored) (msgs []Message, summary string, covered int, changed bool) {
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

// summarise rewrites the whole summary instead of appending to it, so it stops
// growing without bound.
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
		fmt.Fprintf(&b, "%s: %s\n\n", who, m.Content)
	}

	msgs := []Message{
		{Role: RoleSystem, Content: "You compress the older part of a conversation so it can carry on from the summary. " +
			"Write one replacement summary covering everything given, folding any existing summary in rather than " +
			"summarising it. Keep what the user is trying to do, decisions made, facts, figures, names and urls that " +
			"were settled on, their stated preferences, and anything they asked for that is still outstanding. Drop " +
			"pleasantries and anything superseded later. Write plain sentences under bold labels, as long as it needs " +
			"to be and no longer, and never add detail that isn't in the text."},
		{Role: RoleUser, Content: b.String()},
	}
	out, err := c.llm.Complete(ctx, msgs, 2048)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Title asks for a short name for a conversation, once, off the first exchange.
// The answer is passed as well as the question, since a question is often too
// short to name on its own.
func (c *Compactor) Title(ctx context.Context, first, answer string) string {
	var b strings.Builder
	b.WriteString("Message:\n<<<\n")
	b.WriteString(trim(first, 1000))
	b.WriteString("\n>>>")
	if a := strings.TrimSpace(answer); a != "" {
		b.WriteString("\n\nThe reply it got:\n<<<\n")
		b.WriteString(trim(a, 1500))
		b.WriteString("\n>>>")
	}
	msgs := []Message{
		{Role: RoleSystem, Content: "You name conversations for a sidebar and never answer them. Give a noun phrase " +
			"of three to six words naming the subject, with no quotes and no trailing full stop, and leave out the " +
			"words chat, conversation, question and help. When the message is short, an abbreviation, a ticker or a " +
			"name, take what it refers to from the reply."},
		{Role: RoleUser, Content: b.String()},
	}
	var out struct {
		Title string `json:"title"`
	}
	if _, err := c.llm.Structured(ctx, msgs, 64, titleSchema, &out); err != nil {
		return ""
	}
	t := strings.Trim(strings.TrimSpace(out.Title), "\"'.:*#- ")
	if t == "" {
		return ""
	}
	return trim(sentenceCase(titleWords(t)), 48)
}

var titleSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"title": map[string]any{"type": "string"}},
	"required":   []string{"title"},
}

// sentenceCase lifts the first letter and leaves every other one alone, since
// VXUS, Lp(a) and iPhone are all cased the way they are for a reason.
func sentenceCase(t string) string {
	first := t
	if i := strings.IndexByte(t, ' '); i > 0 {
		first = t[:i]
	}
	// iPhone and eBay carry their capital in the middle, so a word holding one
	// anywhere is already cased the way somebody meant it.
	if strings.IndexFunc(first, unicode.IsUpper) >= 0 {
		return t
	}
	for i, r := range t {
		if !unicode.IsLetter(r) {
			continue
		}
		return t[:i] + string(unicode.ToUpper(r)) + t[i+utf8.RuneLen(r):]
	}
	return t
}

// titleWords caps a title at six words, since the sidebar has room for no more.
func titleWords(t string) string {
	f := strings.Fields(t)
	if len(f) <= 6 {
		return strings.Join(f, " ")
	}
	return strings.Join(f[:6], " ")
}
