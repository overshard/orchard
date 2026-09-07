package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"chat.bythewood.me/tools"
)

// Checking a draft that called no tools against the local Wikipedia.
//
// The gate asks whether every fact in a draft is supported by a tool result,
// and when the model answered straight from memory there are no tool results,
// so there is nothing for it to check and the draft goes out. That is how an
// answer crediting the Goodyear welt to the tyre company rather than to Charles
// Goodyear Jr. reached the page, which the article's own opening section gets
// right.
//
// So when nothing was fetched, the subject of the question is looked up in the
// offline snapshot and handed to the gate as background. It costs one call to a
// container on the bridge and no model call, since the gate was going to run
// anyway.

// Only when the turn fetched nothing. A turn that called tools has results for
// the gate to work with, and a question whose subject is not a thing an article
// is named after gets nothing useful out of this.

// The shapes a question opens with. Stripping one leaves the subject, which is
// what the wikipedia tool takes, since it refuses a question outright.
var questionHead = regexp.MustCompile(`(?i)^\s*(what|who|which|where|when|why|how)('?s| is| are| was| were| do| does| did)?\s+|^\s*(tell me about|explain|describe|define)\s+`)

// Trailing filler left behind once the head is gone, as in "kubernetes for".
var questionTail = regexp.MustCompile(`(?i)\s+(for|about|like|used for|good for|mean|means|work|works)\s*[?.!]*\s*$`)

// A subject runs to the first clause break. "a b-tree and why do databases use
// them" is one question about one thing.
var clauseBreak = regexp.MustCompile(`(?i)\s+(and|but|or|so|because|since|which|that|vs\.?|versus)\s+`)

// Past this it is a sentence rather than the name of something, and looking it
// up returns whatever happened to rank.
const subjectMaxWords = 5

// subjectOf pulls the thing a question is about out of it, or returns empty
// when there is not one worth looking up.
func subjectOf(question string) string {
	s := strings.TrimSpace(question)
	if s == "" {
		return ""
	}
	s = questionHead.ReplaceAllString(s, "")
	if loc := clauseBreak.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	s = questionTail.ReplaceAllString(s, "")
	s = strings.Trim(s, " \t?.!,:;")
	// Leading article, which is never part of a title.
	s = regexp.MustCompile(`(?i)^(a|an|the)\s+`).ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" || len(strings.Fields(s)) > subjectMaxWords {
		return ""
	}
	// A bare question word survives the head pattern, which needs a word after
	// it to strip anything, so "why" comes through as its own subject.
	if notASubject[strings.ToLower(s)] {
		return ""
	}
	return s
}

var notASubject = map[string]bool{
	"why": true, "how": true, "what": true, "who": true, "when": true, "where": true,
	"which": true, "it": true, "that": true, "this": true, "them": true, "they": true,
}

// background looks the question's subject up in the offline snapshot and
// returns a line for the gate, or empty when there is nothing to add. Anything
// that goes wrong returns empty, since this only ever adds evidence and a
// failure here must not change a verdict.
func (e *Engine) background(ctx context.Context, question string) string {
	subject := subjectOf(question)
	if subject == "" {
		return ""
	}
	args, err := json.Marshal(map[string]string{"query": subject})
	if err != nil {
		return ""
	}
	res := e.reg.Call(ctx, e.deps, tools.Wikipedia.Name, args)
	if res.Err != "" {
		return ""
	}
	m, ok := res.Content.(map[string]any)
	if !ok {
		return ""
	}
	if found, _ := m["found"].(bool); !found {
		return ""
	}
	title, _ := m["title"].(string)
	summary, _ := m["summary"].(string)
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	date, _ := m["snapshot_date"].(string)
	if date == "" {
		date = "an unknown date"
	}
	// The age has to travel with the text. Without it a draft that is right
	// about something recent looks wrong against an older article, and the gate
	// sends a correct answer back to be broken.
	return "wikipedia on " + title + ", from an offline snapshot taken " + date +
		", looked up here rather than by the model: " + trimLine(summary, 1500)
}
