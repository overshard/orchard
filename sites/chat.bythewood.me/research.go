package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"chat.bythewood.me/tools"
)

// What stops a turn ending on "I could look that up for you".
//
// The turn loop breaks the moment the model replies without a tool call, which
// treats every reply as an answer. Two of them are not: a reply that offers to
// go and check, and a reply that states facts about the world nothing in the
// turn checked. Both read as an answer to the loop and neither is one, so the
// model gets one more pass with the tools still on the table rather than the
// turn ending there.

// A deferral in the shapes a small model actually writes them. It is checked
// first because it is free and catches the common case without a model call.
//
// Every pattern needs a first person subject, since "you can search for it on
// their site" is advice and not a deferral, and an answer that mentions
// searching in passing must not be thrown away.
var deferrals = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(i|we)\s+(can|could|will|shall|am able to)\s+(now\s+)?(go\s+)?(and\s+)?(search|look|check|find|fetch|pull|dig|research|browse|see)\b`),
	regexp.MustCompile(`(?i)\b(let me|i'?ll|i will|i'?m going to|i am going to)\s+(go\s+)?(and\s+)?(search|look|check|find|fetch|pull|dig|research|browse|see|grab|get)\b`),
	regexp.MustCompile(`(?i)\bwould you like me to\b`),
	regexp.MustCompile(`(?i)\bdo you want me to\b`),
	regexp.MustCompile(`(?i)\bshall i\b`),
	regexp.MustCompile(`(?i)\bif you'?d? (like|want)\b.{0,40}\b(search|look|check|find)\b`),
	regexp.MustCompile(`(?i)\bjust (say|let me know|tell me)\b.{0,30}\b(and|so)? ?i'?ll\b`),
	regexp.MustCompile(`(?i)\bi (do not|don'?t) have (access to|real ?time|current|up ?to ?date|live)\b`),
	regexp.MustCompile(`(?i)\bmy (training data|knowledge) (only )?(goes|extends|cuts off|ends)\b`),
	regexp.MustCompile(`(?i)\b(i|we) (do not|don'?t) have (anything|any information|any details|much) (to report|on that|about that)\b`),
}

// Where a deferral has to start to count. A reply that puts the work off says
// so at the top, and one that answers and then offers to dig further has
// answered. Without the position an answer ending on "if you want I can check
// the other two" is thrown away and the whole turn is spent again.
const deferralHead = 240

// The length past which even an early hedge is not a deferral, since a model
// that opened with one and then wrote two thousand characters did the work.
const deferralMax = 1200

// isDeferral reports whether a reply puts the work off rather than doing it.
func isDeferral(reply string) bool {
	s := strings.TrimSpace(reply)
	if s == "" || len(s) > deferralMax {
		return false
	}
	for _, re := range deferrals {
		if m := re.FindStringIndex(s); m != nil && m[0] < deferralHead {
			return true
		}
	}
	return false
}

// The gate. It runs when the model stops calling tools and wants to answer,
// which is the only moment where both "is this actually an answer" and "is
// there enough behind it" can be asked at once.
//
// Two fields and one of them an enum, because a 4B handed a free reasoning
// field beside a constrained one writes the reasoning and then contradicts it.
// The query is what makes a verdict actionable: a step that says more research
// is needed without saying what to look for sends the model back to the search
// it already ran.
type verdict struct {
	Verdict string `json:"verdict"`
	Query   string `json:"query"`
}

var verdictSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"verdict": map[string]any{
			"type": "string",
			"enum": []string{"answered", "research"},
		},
		"query": map[string]any{"type": "string"},
	},
	"required":             []string{"verdict", "query"},
	"additionalProperties": false,
}

const gateSystem = `You are checking a draft answer before it is sent. Answer only with the JSON object you were given a schema for.

"answered" means the draft answers the question with real specifics and every fact in it either came from the tool results or is something no tool could change, like arithmetic, code, or an opinion asked for.

"research" means the draft is not ready to send. Choose it when any of these are true:
- The draft offers to look something up, says it will check, or asks whether it should.
- The draft says it does not know, has no access, or has nothing to report.
- The draft states a name, title, date, number, price or event that no tool result above supports.
- The question asks about something current, local, priced, scheduled or newsworthy and no tool was called.
- The draft answers part of the question and leaves the rest.

When the verdict is "research", query is the single web search that would close the biggest gap, written as a person would type it. When the verdict is "answered", query is an empty string.`

// enough asks whether the draft can be sent. Anything that goes wrong is a yes,
// because a gate that fails closed would turn a working turn into a loop over a
// model that is not answering the gate either.
func (e *Engine) enough(ctx context.Context, question, draft string, results []string) (verdict, Stats) {
	var b strings.Builder
	b.WriteString("Question:\n")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n\nTool results in this turn:\n")
	if len(results) == 0 {
		b.WriteString("(none, no tool was called)")
	} else {
		for _, r := range results {
			b.WriteString("- ")
			b.WriteString(trimLine(r, 600))
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\nDraft answer:\n")
	b.WriteString(trimLine(strings.TrimSpace(draft), 2000))

	msgs := []Message{
		{Role: RoleSystem, Content: gateSystem},
		{Role: RoleUser, Content: b.String()},
	}
	var v verdict
	st, err := e.llm.Structured(ctx, msgs, gateTokens, verdictSchema, &v)
	if err != nil || v.Verdict != "research" {
		return verdict{Verdict: "answered"}, st
	}
	return v, st
}

// The nudge that goes back into the conversation. The draft itself is never
// appended, because a model handed its own deferral writes it again.
func researchNudge(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return "That does not answer the question. Call a tool now and find out. " +
			"Do not offer to look something up and do not say what you would do next, there is nobody to answer you."
	}
	return "That does not answer the question yet. Search for " + quoted(q) + " now, and keep going until you have the specifics. " +
		"Do not offer to look something up and do not say what you would do next, there is nobody to answer you."
}

// Quoted so a multi word query is not read as part of the sentence around it.
func quoted(s string) string { return "\"" + strings.ReplaceAll(s, "\"", "") + "\"" }

// gate is the whole check: the free pattern first, then the model only when the
// pattern found nothing. A turn that already looks like a deferral does not
// need a second opinion, and skipping the call there is a second saved on every
// one of the failures this exists for.
func (e *Engine) gate(ctx context.Context, question, draft string, used []tools.Result, emit func(Event)) (verdict, Stats) {
	if isDeferral(draft) {
		emit(Event{Kind: "status", Text: "looking it up"})
		return verdict{Verdict: "research"}, Stats{}
	}
	emit(Event{Kind: "status", Text: "checking the answer"})
	results := make([]string, 0, len(used))
	for _, r := range used {
		if r.Err != "" {
			results = append(results, r.Name+" failed: "+r.Err)
			continue
		}
		body, _ := json.Marshal(r.Content)
		results = append(results, r.Name+": "+string(body))
	}
	v, st := e.enough(ctx, question, draft, results)
	if v.Verdict == "research" {
		emit(Event{Kind: "status", Text: "looking it up"})
	}
	return v, st
}
