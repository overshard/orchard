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
var questionTail = regexp.MustCompile(`(?i)\s+((is|are|was|were)\s+(it|this|that|they|there)|for|about|like|used for|good for|mean|means|work|works)\s*[?.!]*\s*$`)

// A trailing time phrase. It is stripped rather than used as a signal on its
// own, because the question it is attached to may still name a real subject:
// "what is the RTX 5090 going for today" is about the card.
var timeTail = regexp.MustCompile(`(?i)\s+(right\s+now|now|today|tonight|yesterday|currently|lately|so\s+far|this\s+(morning|afternoon|evening|week|weekend|month|year)|last\s+(night|week|weekend|month|year))\s*[?.!]*\s*$`)

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
	// A time word on the end is never part of a name, and leaving it there hid
	// the head noun: "Big news today" ended in "today" and so read as an
	// ordinary subject.
	s = timeTail.ReplaceAllString(s, "")
	s = strings.Trim(s, " \t?.!,:;")
	// Leading article, which is never part of a title.
	s = regexp.MustCompile(`(?i)^(a|an|the)\s+`).ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" || len(strings.Fields(s)) > subjectMaxWords {
		return ""
	}
	// A bare question word survives the head pattern, which needs a word after
	// it to strip anything, so "why" comes through as its own subject.
	l := strings.ToLower(s)
	if notASubject[l] || liveSubject[l] {
		return ""
	}
	// The whole phrase is not always the giveaway. "Big news today" reduces to
	// "Big news", which is in neither map, and the snapshot answered it with
	// the founding date of Universe Today. The head noun is what the phrase is
	// really about, and "News Corporation" still survives because its head is
	// the corporation.
	if f := strings.Fields(l); len(f) > 1 && liveSubject[f[len(f)-1]] {
		return ""
	}
	return s
}

// Things the snapshot has an article about and can never answer a question
// about, since what is being asked is today's value and not what the thing is.
// "what's the weather like" reduces to "weather" and would otherwise spend a
// lookup and a page of context on the meteorology article.
var liveSubject = map[string]bool{
	"weather": true, "forecast": true, "temperature": true, "time": true, "date": true,
	"news": true, "score": true, "scores": true, "price": true, "prices": true,
	"stock": true, "stocks": true, "market": true, "markets": true, "traffic": true,
	"pollen": true, "aqi": true, "air quality": true, "exchange rate": true,
	"headline": true, "headlines": true, "story": true, "stories": true,
}

var notASubject = map[string]bool{
	"why": true, "how": true, "what": true, "who": true, "when": true, "where": true,
	"which": true, "it": true, "that": true, "this": true, "them": true, "they": true,
	// A message telling the assistant to do something is not a message about a
	// thing. "remember that i want to watch this" reduced to "remember" and
	// fetched the Wikipedia article on memory, which then sat in front of the
	// model while it decided what the turn was about.
	"remember": true, "forget": true, "note": true, "save": true, "keep": true,
	"add": true, "update": true, "delete": true, "ignore": true,
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

// opening looks the question's subject up before the model decides anything.
//
// The snapshot is local, so this costs a call on the bridge and no web request,
// and it is current in a way the weights are not: the model had Fumio Kishida
// as prime minister of Japan and the snapshot has Sanae Takaichi. Handing it
// over first means the common question is answered from something checkable
// rather than from training, and the model can still go further from there.
//
// It returns the result to record and the message to put in front of the model,
// or a zero result when there is nothing worth adding.
func (e *Engine) opening(ctx context.Context, question string) (tools.Result, Message, bool) {
	subject := subjectOf(question)
	if subject == "" {
		return tools.Result{}, Message{}, false
	}
	args, err := json.Marshal(map[string]string{"query": subject})
	if err != nil {
		return tools.Result{}, Message{}, false
	}
	res := e.reg.Call(ctx, e.deps, tools.Wikipedia.Name, args)
	if res.Err != "" {
		return tools.Result{}, Message{}, false
	}
	m, ok := res.Content.(map[string]any)
	if !ok {
		return tools.Result{}, Message{}, false
	}
	if found, _ := m["found"].(bool); !found {
		return tools.Result{}, Message{}, false
	}
	title, _ := m["title"].(string)
	summary, _ := m["summary"].(string)
	date, _ := m["snapshot_date"].(string)
	if strings.TrimSpace(summary) == "" {
		return tools.Result{}, Message{}, false
	}
	if date == "" {
		date = "an unknown date"
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, here is the opening section of the " +
		title + " article from the offline Wikipedia on this machine, looked up for you. It was taken " +
		date + ", so it is newer than your training and still older than today.\n\n" +
		summary +
		"\n\nUse it where it answers the question, and say what it says rather than what you remember, " +
		"since your memory of a name, a date or who currently holds an office is the part most likely to " +
		"be out of date. Call another tool if the question needs more than this covers, and use " +
		"web_search for anything that could have changed since " + date + "."}
	return res, msg, true
}
