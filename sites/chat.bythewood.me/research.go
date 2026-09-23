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
// treats every reply as an answer. A reply that offers to go and check, or one
// that states facts nothing in the turn checked, gets another pass with the
// tools still on the table instead.

// The shapes a small model writes a deferral in, checked first since they are
// free. Every pattern needs a first person subject, because "you can search for
// it on their site" is advice rather than a deferral.
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

// Where a deferral has to start to count. Without the position, an answer ending
// on "if you want I can check the other two" is thrown away and the whole turn
// is spent again.
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
// reading its own earlier answer treats it as evidence and writes it out again.
// Six word shingles against every earlier answer catch that, and the floor sits
// between what a rehash scores and what a real follow-up does.
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

// The gate runs when the model stops calling tools and wants to answer.
//
// Two fields and one of them an enum, since a 4B handed a free reasoning field
// beside a constrained one writes the reasoning and then contradicts it. A
// verdict without a query sends the model back to the search it already ran.
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

"answered" means the draft answers the question with real specifics and every fact in it either came from the tool results, or was established earlier in this conversation, or is something no tool could change, like arithmetic, code, or an opinion asked for. A draft that disagrees with the background section is never "answered", whatever else is true of it.

"research" means the draft is not ready to send. Choose it when any of these are true:
- A background section is present and the draft disagrees with it. Compare them claim by claim before anything else. The background is the opening section of a Wikipedia article, so on a definition, a name, a date, an origin or who did something it is right and a draft that says otherwise is wrong, however confident the draft sounds. Two limits: the background does not cover everything, so a fact it is simply silent on is not a disagreement, and it was taken on the date it states, so a difference about something that could have changed since then means the background is old rather than the draft wrong, and that is "answered".
- The draft offers to look something up, says it will check, or asks whether it should.
- The draft says it does not know, has no access, has nothing to report, or cannot answer from what it has.
- The draft states a name, title, date, number, price or event that no tool result above supports.
- The question asks about something current, local, priced, scheduled or newsworthy and no tool was called.
- The draft answers part of the question and leaves the rest.
- The draft repeats an earlier answer in this conversation instead of addressing what the new question adds to it.
- The draft quotes a page or credits a figure to a source that is not in the tool results above. Pages read in earlier turns are gone and cannot be quoted from memory.

When the verdict is "research", query is the single web search that would close the biggest gap, written as a person would type it. When the verdict is "answered", query is an empty string.`

// Whether the question needs something the weights cannot hold. Asked of the
// question and never of the draft, since a draft that invents an answer reads
// exactly like one that knows it.
type freshness struct {
	NeedsFresh bool   `json:"needs_fresh"`
	Query      string `json:"query"`
}

var freshnessSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"needs_fresh": map[string]any{"type": "boolean"},
		"query":       map[string]any{"type": "string"},
	},
	"required":             []string{"needs_fresh", "query"},
	"additionalProperties": false,
}

// One question rather than the eight the draft check weighs at once, since a 9B
// asked for a single judgement gets it right far more often.
const freshnessSystem = `Decide whether answering the user's question correctly needs information you could not have from training alone, because it changes over time or has happened since.

true when the question touches news, current events, prices, markets, scores, odds, fixtures, schedules, opening or closing, weather, or what is happening now.
true whenever the question carries a time word like today, tonight, this weekend, yesterday, this week, right now, currently, or latest, even if the subject sounds ordinary.
true when the question asks what has been going on with something.

false when the answer is a definition, an explanation, how something works, history, code, arithmetic, or a recipe, none of which change.

When needs_fresh is true, query is the single web search that would answer it, written as a person would type it. When it is false, query is an empty string.`

// The second narrow question, asked of a draft that fetched nothing and passed
// the freshness check. What goes wrong there is specifics written from memory,
// so it is asked of the draft rather than of the question, which is often vague.
type grounding struct {
	NeedsCheck bool   `json:"needs_check"`
	Query      string `json:"query"`
}

var groundingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"needs_check": map[string]any{"type": "boolean"},
		"query":       map[string]any{"type": "string"},
	},
	"required":             []string{"needs_check", "query"},
	"additionalProperties": false,
}

const groundingSystem = `A draft answer was written without looking anything up. Decide whether it states specifics that ought to have been checked.

true when the draft names a product, model number, part number, version, company, person, book, film or song.
true when it gives a figure presented as fact: a price, a measurement, a nutrition number, a count, a capacity, a date.
true when it describes what a named tool, service, format or standard does or is used for.

false when the draft is explanation, reasoning, opinion, code the user asked to be written, arithmetic on figures the user supplied, or ordinary conversation.
false when every specific in it came from what the user said in the question.

When needs_check is true, query is the single web search that would check the most load bearing specific, written as a person would type it. When it is false, query is an empty string.`

// needsChecking reports whether a draft written from memory states specifics.
// A failure is a no, for the same reason every other gate fails open.
func (e *Engine) needsChecking(ctx context.Context, draft string) (grounding, Stats) {
	msgs := []Message{
		{Role: RoleSystem, Content: groundingSystem},
		{Role: RoleUser, Content: "Draft:\n" + trim(strings.TrimSpace(draft), 1800)},
	}
	var g grounding
	st, err := e.llm.Structured(ctx, msgs, gateTokens, groundingSchema, &g)
	if err != nil {
		return grounding{}, st
	}
	return g, st
}

// needsFresh reports whether the question wants current information. A failure
// is a no, for the same reason the draft check fails open: a turn that cannot
// reach the model to ask is not a turn to send round again.
func (e *Engine) needsFresh(ctx context.Context, question string) (freshness, Stats) {
	msgs := []Message{
		{Role: RoleSystem, Content: freshnessSystem},
		{Role: RoleUser, Content: "Question: " + strings.TrimSpace(question)},
	}
	var f freshness
	st, err := e.llm.Structured(ctx, msgs, gateTokens, freshnessSchema, &f)
	if err != nil {
		return freshness{}, st
	}
	return f, st
}

// enough asks whether the draft can be sent. Anything that goes wrong is a yes,
// because a gate that fails closed would turn a working turn into a loop over a
// model that is not answering the gate either.
func (e *Engine) enough(ctx context.Context, question, draft, background string, results, previous []string) (verdict, Stats) {
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
	if background != "" {
		b.WriteString("\n\nBackground, looked up locally rather than by the model:\n")
		b.WriteString(background)
		b.WriteByte('\n')
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

// A draft that totals numbers in prose is sent back to add them up with calc.
// Working out which numbers in a sentence are the addends is the part that goes
// wrong, and calc gets it right by construction.
var (
	totalWord = regexp.MustCompile(`(?i)\b(total|totals|totalling|altogether|all together|adds up to|comes to|sums? to|in total|grand total)\b`)
	// A citation marker is a number to a regex and is not one to a reader.
	citeNum = regexp.MustCompile(`\[\d{1,3}\]`)
	numeral = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)
)

// countsUpInProse is true for a draft that states a total over several numbers
// it worked out itself. Three is the floor: two numbers and a total is usually
// a comparison, and one is a quantity rather than a sum.
func countsUpInProse(draft string) bool {
	clean := citeNum.ReplaceAllString(draft, " ")
	if !totalWord.MatchString(clean) {
		return false
	}
	// Inside a fence the numbers belong to code somebody is about to run.
	clean = strings.Join(outsideFences(clean), "\n")
	return len(numeral.FindAllString(clean, -1)) >= 3 && totalWord.MatchString(clean)
}

// copiedTheNumbers reports that every figure in the draft already appears in a
// tool result from this turn, so the model transcribed rather than computed.
//
// Some tools add up for it. The property one returns an all-in monthly worked
// out in Go and a table built from it, and sending that back to be totalled with
// calc spent two rounds re-deriving a number that was already right.
func copiedTheNumbers(draft string, used []tools.Result) bool {
	if len(used) == 0 {
		return false
	}
	var outputs strings.Builder
	for _, r := range used {
		if r.Err != "" {
			continue
		}
		raw, err := json.Marshal(r.Content)
		if err != nil {
			return false
		}
		outputs.Write(raw)
		outputs.WriteByte('\n')
	}
	// Separators differ between a tool result and a sentence, so both sides are
	// compared on the digits alone.
	bare := strings.NewReplacer(",", "", "$", "").Replace(outputs.String())
	if bare == "" {
		return false
	}
	for _, n := range numeral.FindAllString(strings.Join(outsideFences(citeNum.ReplaceAllString(draft, " ")), "\n"), -1) {
		if !strings.Contains(bare, strings.ReplaceAll(n, ",", "")) {
			return false
		}
	}
	return true
}

// outsideFences drops fenced code, since arithmetic in an example is not a
// claim about a total.
func outsideFences(s string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			out = append(out, line)
		}
	}
	return out
}

func calcNudge() string {
	return "You added those up yourself. Call calc with the figures and write the total it gives you, " +
		"rather than the one you worked out. If some of the figures are missing, say which."
}

// propertyNudge is what a thin draft gets when the property tool already ran. A
// house is answered out of public records and not off the web, so sending it to
// a search there can only contradict what it was handed, and the sections it has
// not read yet are the thing it is actually missing.
func propertyNudge() string {
	return "That does not cover the house yet. Call property again for the sections you have not " +
		"read, using the same address, and answer from those. Do not search the web for it. " +
		"If the result gave you a cost table, print that table as it came and keep it in the answer."
}

// A house number, a street name and a street type, which is what every chip and
// every listing carries.
var streetAddress = regexp.MustCompile(`(?i)\b\d{1,6}\s+(?:[a-z0-9.'-]+\s+){1,4}(?:rd|road|st|street|dr|drive|ln|lane|ave|avenue|blvd|boulevard|ct|court|hwy|highway|way|pl|place|cir|circle|trl|trail|pkwy|parkway|loop|ter|terrace|run|pike)\b`)

func namesAnAddress(question string) bool { return streetAddress.MatchString(question) }

func addressNudge() string {
	return "That question names a house. Call property with that address and the section the question is about, " +
		"and answer from what it returns. Do not search the web for it."
}

func researchNudge(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return "That does not answer the question. Call a tool now and find out. " +
			"Do not offer to look something up and do not say what you would do next, there is nobody to answer you."
	}
	return "That does not answer the question yet. Search for " + quoted(q) + " now, and keep going until you have the specifics. " +
		"Do not offer to look something up and do not say what you would do next, there is nobody to answer you."
}

// What a draft contradicted by the snapshot is told. It names the tool and the
// subject, since a model sent back without both goes and searches the web for
// what is already on this machine.
func wikiNudge(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Part of that does not match what the offline Wikipedia says. Call wikipedia now, read it, and correct the answer."
	}
	return "Part of that does not match what the offline Wikipedia says about " + quoted(s) + ". " +
		"Call wikipedia with " + quoted(s) + " now, read the article, and correct the answer rather than repeating it."
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

// gate is the whole check. It returns the nudge to send the turn back with, or
// an empty string to let the draft stand. The two free checks run first and the
// model is only asked when neither fired. previous is what this conversation has
// already answered, most recent last.
func (e *Engine) gate(ctx context.Context, question, draft string, previous []string, used []tools.Result, emit func(Event)) (string, Stats) {
	// Sending a turn back to research while the search endpoint is in the penalty
	// box is a loop that cannot succeed, so every nudge below that names a search
	// is held back. The local snapshot is not, since it is a container on the
	// bridge and nothing there is refusing us.
	_, searchDown := e.SearchDown()
	if searchDown {
		return e.gateOffline(ctx, question, draft, used, emit)
	}
	// A news rundown is finished when it arrives. The gate reads a list of twenty
	// stories as an answer that covers part of the question, so left alone it
	// sends the turn back and the rundown becomes a single story write up.
	if calledNews(used) {
		return "", Stats{}
	}
	// Same for a turn that was told to remember something and did. There is no
	// question under it to research.
	if calledTool(used, tools.Remember.Name) {
		return "", Stats{}
	}
	// A follow up chip names the house and the model answers it from the last
	// reply, which reads to the freshness check as local and current, so it went
	// to the web and came back saying the lot's own flood zone was unknown.
	if len(used) == 0 && namesAnAddress(question) {
		emit(Event{Kind: "status", Text: "looking it up"})
		return addressNudge(), Stats{}
	}
	// Asked of the question and before anything reads the draft, since the failure
	// this catches is a draft that sounds like an answer. A turn that already
	// fetched something is left alone.
	var st Stats
	if len(used) == 0 {
		f, fst := e.needsFresh(ctx, question)
		st.merge(fst)
		if f.NeedsFresh {
			emit(Event{Kind: "status", Text: "looking it up"})
			return researchNudge(f.Query), st
		}
	}
	if isDeferral(draft) || refusal.MatchString(draft) {
		emit(Event{Kind: "status", Text: "looking it up"})
		if calledTool(used, tools.PropertyTool.Name) {
			return propertyNudge(), st
		}
		return researchNudge(""), st
	}
	// Only when nothing was fetched. A turn that did the work and then restated
	// some of what it said before has answered, and taking it away would cost
	// the tool calls it already spent.
	if len(used) == 0 && repeatsAnswered(draft, previous) {
		emit(Event{Kind: "status", Text: "looking it up"})
		return repeatNudge(), st
	}
	// Before the model check, since it costs nothing and the model check has
	// never once objected to a wrong sum.
	if !calledTool(used, tools.Calc.Name) && countsUpInProse(draft) && !copiedTheNumbers(draft, used) {
		emit(Event{Kind: "status", Text: "adding it up"})
		return calcNudge(), st
	}
	// A draft written from memory that states specifics. The freshness check above
	// only catches what changes over time, and the specifics that were wrong were
	// mostly things that do not.
	if len(used) == 0 {
		g, gst := e.needsChecking(ctx, draft)
		st.merge(gst)
		if g.NeedsCheck {
			emit(Event{Kind: "status", Text: "checking it"})
			return researchNudge(g.Query), st
		}
	}
	emit(Event{Kind: "status", Text: "checking the answer"})
	// Only when the turn fetched nothing, since that is the case the gate has
	// no evidence for. A turn that called tools already gave it something to
	// work with, and a second opinion there would argue with what was fetched.
	var background string
	if len(used) == 0 {
		background = e.background(ctx, question)
	}
	results := make([]string, 0, len(used))
	for _, r := range used {
		if r.Err != "" {
			results = append(results, r.Name+" failed: "+r.Err)
			continue
		}
		body, _ := json.Marshal(r.Content)
		results = append(results, r.Name+": "+string(body))
	}
	v, est := e.enough(ctx, question, draft, background, results, previous)
	st.merge(est)
	if v.Verdict != "research" {
		return "", st
	}
	emit(Event{Kind: "status", Text: "looking it up"})
	// The snapshot already has the article, so sending it to a web search for
	// something a local call answers in milliseconds is the slower way to be
	// right.
	if calledTool(used, tools.PropertyTool.Name) {
		return propertyNudge(), st
	}
	if background != "" {
		return wikiNudge(subjectOf(question)), st
	}
	return researchNudge(v.Query), st
}

// gateOffline is the gate with the search host refusing us. The only thing that
// can be acted on is a draft the local snapshot disagrees with, so anything else
// is let through.
func (e *Engine) gateOffline(ctx context.Context, question, draft string, used []tools.Result, emit func(Event)) (string, Stats) {
	if len(used) > 0 {
		return "", Stats{}
	}
	background := e.background(ctx, question)
	if background == "" {
		return "", Stats{}
	}
	emit(Event{Kind: "status", Text: "checking the answer"})
	v, st := e.enough(ctx, question, draft, background, nil, nil)
	if v.Verdict != "research" {
		return "", st
	}
	emit(Event{Kind: "status", Text: "looking it up"})
	return wikiNudge(subjectOf(question)), st
}

func calledNews(used []tools.Result) bool { return calledTool(used, tools.News.Name) }

func calledTool(used []tools.Result, name string) bool {
	for _, r := range used {
		if r.Name == name && r.Err == "" {
			return true
		}
	}
	return false
}
