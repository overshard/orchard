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

// A refusal is not a deferral and the head and length rules above do not reach
// it. This one is matched anywhere in a reply of any length, which is only safe
// because it says outright that the reply is not answering the question.
var refusal = regexp.MustCompile(`(?i)\b(i|we) (cannot|can'?t|could not|couldn'?t) answer (this|that|it|your question)\b.{0,60}\bfrom (the )?tool results\b`)

// The tool results die with the turn and only the answers survive, so a model
// reading its own earlier answer treats it as evidence it still holds and
// writes it out again. That is not a deferral and the patterns above miss it.
//
// Six word shingles against every earlier answer put the rehashes already in
// this site's history at 0.34, 0.40 and 0.59, and every real follow-up at 0.08.
const (
	repeatShingle = 6
	repeatOverlap = 0.30
	// Below this a draft is too short for the overlap to mean anything. A one
	// line answer to "why?" shares its whole vocabulary with what came before.
	repeatMinShingles = 20
)

var wordish = regexp.MustCompile(`[a-z0-9]+`)

// repeatsAnswered reports whether most of the draft is already in what this
// conversation has answered.
func repeatsAnswered(draft string, previous []string) bool {
	d := shingles(draft)
	if len(d) < repeatMinShingles {
		return false
	}
	seen := map[string]bool{}
	for _, p := range previous {
		for s := range shingles(p) {
			seen[s] = true
		}
	}
	if len(seen) == 0 {
		return false
	}
	hit := 0
	for s := range d {
		if seen[s] {
			hit++
		}
	}
	return float64(hit)/float64(len(d)) >= repeatOverlap
}

func shingles(s string) map[string]bool {
	w := wordish.FindAllString(strings.ToLower(s), -1)
	out := make(map[string]bool, len(w))
	for i := 0; i+repeatShingle <= len(w); i++ {
		out[strings.Join(w[i:i+repeatShingle], " ")] = true
	}
	return out
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

"answered" means the draft answers the question with real specifics and every fact in it either came from the tool results, or was established earlier in this conversation, or is something no tool could change, like arithmetic, code, or an opinion asked for.

"research" means the draft is not ready to send. Choose it when any of these are true:
- The draft offers to look something up, says it will check, or asks whether it should.
- The draft says it does not know, has no access, has nothing to report, or cannot answer from what it has.
- The draft states a name, title, date, number, price or event that no tool result above supports.
- The question asks about something current, local, priced, scheduled or newsworthy and no tool was called.
- The draft answers part of the question and leaves the rest.
- The draft repeats an earlier answer in this conversation instead of addressing what the new question adds to it.
- The draft quotes a page or credits a figure to a source that is not in the tool results above. Pages read in earlier turns are gone and cannot be quoted from memory.

When the verdict is "research", query is the single web search that would close the biggest gap, written as a person would type it. When the verdict is "answered", query is an empty string.`

// enough asks whether the draft can be sent. Anything that goes wrong is a yes,
// because a gate that fails closed would turn a working turn into a loop over a
// model that is not answering the gate either.
func (e *Engine) enough(ctx context.Context, question, draft string, results, previous []string) (verdict, Stats) {
	var b strings.Builder
	b.WriteString("Question:\n")
	b.WriteString(strings.TrimSpace(question))
	if len(previous) > 0 {
		// Without this the gate cannot tell a fresh answer from the last one
		// written out again, which is the whole failure on a follow-up.
		b.WriteString("\n\nAnswers already given earlier in this conversation:\n")
		for _, p := range previous {
			b.WriteString("- ")
			b.WriteString(trimLine(p, 600))
			b.WriteByte('\n')
		}
	}
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

// What a rehash is told, which has to say why rather than just no. A model sent
// back with the generic nudge writes the same answer a third time, since as far
// as it can see it already has the material.
func repeatNudge() string {
	return "That is the answer you already gave, and it does not address what this question adds. " +
		"The tool results from the earlier turns are gone and the pages behind them are not in front of you, so nothing there can be quoted or checked. " +
		"Call a tool now and get what this question needs."
}

// Quoted so a multi word query is not read as part of the sentence around it.
func quoted(s string) string { return "\"" + strings.ReplaceAll(s, "\"", "") + "\"" }

// gate is the whole check, and it returns the nudge to send the turn back with
// or an empty string to let the draft stand. The two free checks run first and
// the model is only asked when neither fired, which is a second saved on every
// one of the failures this exists for. previous is what this conversation has
// already answered, most recent last.
func (e *Engine) gate(ctx context.Context, question, draft string, previous []string, used []tools.Result, emit func(Event)) (string, Stats) {
	// Sending a turn back to research when the search endpoint is in the
	// penalty box is a guaranteed loop: it cannot succeed, and every pass costs
	// a model call and another failed request against a host that is already
	// refusing. The honest answer there is the one it has.
	if _, down := e.SearchDown(); down {
		return "", Stats{}
	}
	if isDeferral(draft) || refusal.MatchString(draft) {
		emit(Event{Kind: "status", Text: "looking it up"})
		return researchNudge(""), Stats{}
	}
	// Only when nothing was fetched. A turn that did the work and then restated
	// some of what it said before has answered, and taking it away would cost
	// the tool calls it already spent.
	if len(used) == 0 && repeatsAnswered(draft, previous) {
		emit(Event{Kind: "status", Text: "looking it up"})
		return repeatNudge(), Stats{}
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
	v, st := e.enough(ctx, question, draft, results, previous)
	if v.Verdict != "research" {
		return "", st
	}
	emit(Event{Kind: "status", Text: "looking it up"})
	return researchNudge(v.Query), st
}
