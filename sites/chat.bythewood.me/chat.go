// The conversation loop. A turn is not one big generation: the model decides
// what it needs, tools fetch it, and only then does it write. That is the same
// insight search rests on and it is what makes a 4B usable here.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"chat.bythewood.me/tools"
)

const (
	// Enough for a comparison that searches per thing, reads the best page, and
	// then goes back for whatever the gate says is missing. The repeat ledger
	// below is what stops a model spending these on the same call over and
	// over, which is why this can be more than the four it used to be.
	maxToolRounds = 6

	// How many times a reply that is not an answer gets sent back. Two, because
	// a model that has ignored the instruction twice is not going to take it on
	// the third go and the turn still owes the user something.
	maxGates = 2

	// Budgets. The answer gets the big one because it is the only step whose
	// output the user reads.
	// Enough for several tool calls in one round. It was 900, which a model
	// asked to total a bank statement spent on one arithmetic expression before
	// being cut off mid string.
	toolTurnTokens = 1600
	answerTokens   = 2400
	// The gate emits an enum and a search query and nothing else.
	gateTokens = 120
)

// Event is what the browser is told while a turn runs.
//
// The answer arrives as `block` and `tail` rather than raw text. A block is a
// finished piece of markdown already rendered to HTML, and the tail is the
// unfinished paragraph after it, as plain text. That way the reader sees
// formatting appear as it is settled instead of reading plain text and then
// having the whole message reflow under them when the turn ends.
type Event struct {
	Kind string `json:"kind"` // status, tool, tool_done, widget, step, block, tail, done, error
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
	Tool string `json:"tool,omitempty"`
	Args string `json:"args,omitempty"`
	MS   int64  `json:"ms,omitempty"`
	OK   bool   `json:"ok,omitempty"`

	// The subject of a chart, sent as soon as the tool that named it returns so
	// the panel is drawing while the answer is still being written.
	Widget *tools.Widget `json:"widget,omitempty"`

	// One entry in the record of what this turn did, sent as it happens so the
	// list fills in rather than appearing all at once at the end.
	Step *Step `json:"step,omitempty"`
}

type Engine struct {
	// Render is the same markdown renderer the finished message uses, so what
	// streams in and what is stored cannot disagree.
	Render func(string) string

	llm  *LLM
	reg  *tools.Registry
	deps *tools.Deps
	now  func() time.Time
	// modelName is what the model is told it is. It is the readable name
	// rather than llama-swap's "local" alias, which is a routing key and means
	// nothing to a reader.
	modelName string
	place     string
	tz        string
}

// Deps is the shared dependency set, which the widget endpoints borrow so their
// calls go through the same breaker and the same spend ceiling a tool's would.
// They pass no session, since neither endpoint reads anything of Isaac's.
func (e *Engine) Deps() *tools.Deps { return e.deps }

func NewEngine(llm *LLM, modelName string) *Engine {
	return &Engine{
		llm: llm, reg: tools.Default(), deps: tools.NewDeps(), now: time.Now,
		modelName: modelName,
		place:     "Yadkin Valley, North Carolina", tz: "America/New_York",
	}
}

// ambient is what a person sitting here would know without being told. Without
// the date the model cannot tell what "this weekend" means, and without the
// place it answers a question about the weather as though it were nowhere.
//
// The time is stated to the hour rather than the minute on purpose: every
// system prompt opens with this block and llama.cpp caches the prompt prefix it
// has already processed, so a clock that ticks every minute means no turn ever
// reuses another's work.
func (e *Engine) ambient() string {
	loc, err := time.LoadLocation(e.tz)
	if err != nil {
		loc = time.UTC
	}
	t := e.now().In(loc)
	return fmt.Sprintf("Today is %s. It is around %s. The user is in %s, "+
		"which is what to use for weather, local news and anything asking what is nearby. "+
		"It is not a hint about what an unfamiliar name means.",
		t.Format("Monday, 2 January 2006"), t.Format("3 PM MST"), e.place)
}

// identity is first in the prompt because a small model asked what it is will
// otherwise answer with whatever name dominated its training data, and several
// of them say Claude. It is a training artifact rather than a jailbreak, and
// the only fix is telling it what it actually is.
const identity = `You are %s, an open weights model running through llama.cpp on Isaac's own RTX 3070, in a chat application he wrote. You are not Claude, ChatGPT, Gemini, or any hosted assistant, and you were not made by Anthropic, OpenAI or Google. If you are asked what you are, say which model you are and that you run locally on his hardware. Do not claim to be anything else, and do not apologise for what you are.

`

const contract = `You are Isaac's assistant. He is a software engineer who self hosts everything he runs, has a family, camps and hikes, and asks direct questions and wants direct answers.

Use a tool whenever the answer depends on something you cannot know from memory: anything current, local, priced, scheduled, on a page, or checkable against a real record. Do not guess a fact a tool can give you, and do not tell the user to go look it up themselves.

Look a subject up before answering about it, whether it is a person, company, product, game, film, event, place, species, or a technical term or concept. That includes "who is X", "what is X" and "tell me about X". Being sure you know is not evidence, and a real name, a date or a definition you half remember is the part most likely to be wrong. Start with wikipedia, which is local and costs nothing and is there so you do not have to answer from memory, and go to web_search when it has no article or when the question is about something current.

Tools:
- Call a tool rather than describing what one would return.
- Never offer to look something up and never ask whether you should. There is nobody to answer you, so an offer ends the turn with nothing in it. If a tool would help, call it now.
- Never answer a question about the world from memory when a tool could check it. Your training data is old and this is what the tools are for.
- wikipedia is an offline snapshot on this machine. It answers instantly, it cannot be rate limited, and it carries each article's opening section only, so it is the cheapest way to get the background right before deciding whether anything needs searching. It knows nothing after its snapshot date, so never use it for news, prices, scores or anything that changed recently.
- web_search gives titles, urls and snippets. Call web_fetch on a url when you need what the page actually says.
- news reads a fixed list of publishers and is what to call for any question about what is happening or what happened over a period, rather than searching. Pass the window the question actually used, so today means today and this weekend means the weekend just gone, and pass the topic only when one was named. It hands back each publisher's own headline for you to rewrite plainly.
- web_search and web_fetch are the ordinary way to look something up and are what you should reach for. deep_search is the exception: it reads the pages properly and checks every sentence against what it cites, and it takes a minute or more during which nothing else can run. Use it when being wrong would matter, when Isaac asks you to check or verify or source something, or when a claim is disputed. Never use it for a quick fact, a score, a price or the weather, and never more than once in a turn.
- An attached file is already in this conversation in full. There is no url or path for it, so never try to fetch one, and never guess where it might be on a disk.
- Search once per thing you are comparing. One search rarely covers a comparison or a build.
- Use calc for totals rather than adding in your head.
- If a tool errors or is rate limited, say so plainly and answer with what you have. Never treat a missing tool as a reason not to answer.
- A search that finds nothing for what you assumed the question meant, and one clear hit for something else, has told you the assumption was wrong. Take the hit and answer about that, rather than reporting that the thing you invented could not be found.
- markets and weather draw a chart above your answer, so the reader can already see the price against its range, or the week with its rain and pollen. Say what it means rather than reading it out: the direction and why it matters, the day the rain arrives, whether the pollen is worth staying in for. Listing seven days of numbers underneath the panel that shows them is the one thing not to do.
- remember is long term memory, kept between conversations. Call it when he asks you to remember, note or forget something, and when he states a preference, a plan or something about himself worth keeping. Saying you will remember it does not remember it, the call does. List first when you need an id to correct or drop one, and keep each fact to one plain sentence about him.
- The orchard_ tools read Isaac's own infrastructure: his logs, uptime monitoring, analytics, git repositories and dashboard. Use them for any question about his own sites rather than guessing or searching the web, and say which one you read. They only read, so nothing you do with them can change anything.

Follow-ups:
- A follow-up is a new question. What you answered before covers what it says and nothing more, so anything this question adds needs a tool call before you answer it.
- Tool results do not survive the turn that fetched them. Your earlier answers are still here and the pages behind them are not, so never quote a page or credit a figure to a source you read in an earlier turn. Fetch it again if you need what it said.
- When the user pushes back, corrects you, or asks why, go and look. Rewriting the answer you already gave tells him nothing he does not have, and a correction usually means the first search missed the thing he is asking about.
- When he tells you what something is, that is now what it is. Drop your own reading of it completely, including the searches you built on it, and look the thing up under the name he gave it. Repeating the earlier answer after being told its premise was wrong is the worst thing you can do here.
- An unfamiliar name is a name. Look it up as one before deciding it must be a place, a river or a landmark near him.

Answers:
- Not every message is a question. When he is chatting, agreeing, joking or thinking out loud, answer like a person would in a line or two and call nothing. Never tell him you do not know what he is asking.
- Lead with the answer. No preamble, no restating the question, no closing offer of more help.
- Never write a web address. When you are given a numbered list of sources, end the sentence with the number it came from, like [2].
- Say plainly when you are unsure or when sources disagree. A short honest answer beats a confident wrong one.
- Every name, title, date, number and price you write has to come from a tool result in this turn, from an answer you already gave in this conversation, or from what the user told you. Anything else needs a tool call before you write it.
- This is the only reply the user gets, so put everything you found in it.
- Never invent a product, a song, a part number, a price or a source. Check it or say you are not sure.
- Follow the format and constraints asked for exactly. Given a budget, a word count or a unit, hit it and show the total.
- Markdown for structure. Bold only for labels, never mid sentence for emphasis.
- No em dashes and no semicolons. Use a comma, a full stop, or the word they stand in for.`

// Memory is what the engine is handed for this turn, already filtered down to
// what the question touched. The engine does no retrieval of its own, so the
// same turn can be run in a test with a fixed set of facts.
func (e *Engine) systemWith(memory string) Message {
	m := e.system()
	m.Content += memory
	return m
}

func (e *Engine) system() Message {
	name := e.modelName
	if name == "" {
		name = "a small local model"
	}
	return Message{Role: RoleSystem,
		Content: fmt.Sprintf(identity, name) + e.ambient() + "\n\n" + contract}
}

// RestoreGuard hands the guard its persistence and whatever the last process
// left behind.
func (e *Engine) RestoreGuard(store tools.PenaltyStore, saved map[string][2]int64) {
	e.deps.Guard.Restore(store, saved)
}

// RestoreSpend puts back what the last process spent, so a deploy is not a
// fresh day's allowance.
func (e *Engine) RestoreSpend(at []time.Time) {
	e.deps.Budgets.Restore(tools.SearchHost, at)
}

// SearchSpend is what the page shows, so the pool draining is visible before it
// is gone.
func (e *Engine) SearchSpend() (minute, hour, day int) {
	m, h, d, _ := e.deps.Budgets.Left(tools.SearchHost)
	return m, h, d
}

// SaveSpend hands the current counts back to the caller's store. It runs after
// a turn rather than per request, since a write on the request path costs more
// than losing one turn's counts to a hard kill.
func (e *Engine) SaveSpend(save func(host string, at []time.Time)) {
	save(tools.SearchHost, e.deps.Budgets.Spent(tools.SearchHost))
}

// SearchDown reports whether the search endpoint is in the penalty box and for
// how much longer, so the page can say so before a turn discovers it.
func (e *Engine) SearchDown() (time.Duration, bool) {
	left, ok := e.deps.Guard.Down()[tools.SearchHost]
	return left, ok
}

// Run drives one user turn and emits events as it goes.
// Run drives one user turn. The session is the caller's own, forwarded to the
// orchard tools so each site checks it rather than this one holding a
// credential of its own.
func (e *Engine) Run(ctx context.Context, history []Message, user, session, memory string, tr *Trace, emit func(Event)) (Message, []tools.Result, []Source, []tools.Widget, Stats, error) {
	deps := e.deps.WithSession(session)
	// deep_search asks another service to run a model, so the flag has to travel
	// with the call rather than only with this process's own requests.
	deps.Incognito = IsIncognito(ctx)
	// Which widgets have already gone out, since the sink holds every one the
	// turn has produced and each round would otherwise resend the earlier ones.
	sentWidgets := map[string]bool{}
	sys := e.systemWith(memory)
	msgs := append([]Message{sys}, history...)
	msgs = append(msgs, Message{Role: RoleUser, Content: user})
	tr.Add(Step{Kind: "prompt", Label: "system prompt built",
		In: user, Out: sys.Content,
		Meta: itoa(len(sys.Content)) + " characters, " + itoa(len(history)) + " earlier messages in the window"})

	var used []tools.Result
	// The first move on any question naming a thing, since the snapshot is on
	// this machine and is newer than the weights. It goes in after the history
	// so the cached prompt prefix survives, and it is recorded as a tool call
	// because that is what it is and the reader should see its age.
	if res, msg, ok := e.opening(ctx, user); ok {
		emit(Event{Kind: "tool", Tool: res.Name, Args: shortArgs(string(res.Args))})
		emit(Event{Kind: "tool_done", Tool: res.Name, MS: res.Elapsed.Milliseconds(), OK: true})
		msgs = append(msgs, msg)
		used = append(used, res)
		tr.Add(Step{Kind: "wikipedia", Label: "looked the subject up before answering",
			In: subjectOf(user), Out: msg.Content, MS: res.Elapsed.Milliseconds(),
			Meta: "local snapshot, no web request"})
	}

	// What this conversation has already answered. A follow-up is where the loop
	// goes wrong, since the results behind those answers are gone and the
	// answers are not, so the model rewrites one instead of fetching anything.
	var answered []string
	for _, m := range history {
		if m.Role == RoleAssistant && strings.TrimSpace(m.Content) != "" {
			answered = append(answered, m.Content)
		}
	}

	var usedDeepSearch bool
	// news reads every feed on the list, so a second call re-reads all of them
	// for a rundown the turn already has. One is the whole answer.
	var usedNews bool
	var stats Stats
	schemas := e.reg.Schemas()

	// A model that gets a thin or failed result will ask for the very same
	// thing again, and again, until the round budget runs out. Nothing in the
	// prompt reliably stops it, so the harness does: an identical call is
	// answered from the ledger with a line telling it not to repeat, and after
	// enough repeats the tools come off the table entirely.
	seen := map[string]tools.Result{}
	repeats := 0
	gates := 0
	// Set when the gate has sent the turn back, so the round that follows a
	// nudge cannot answer with prose again. The nudge still carries the query,
	// and this is what makes it an instruction rather than a request.
	forceTools := false

	for round := 0; round < maxToolRounds; round++ {
		last := round == maxToolRounds-1
		if last {
			// Out of tool budget. Taking the tools away is what forces an
			// answer; leaving them on lets a model spend every round calling
			// something and hand back an empty turn.
			msgs = append(msgs, Message{Role: RoleUser,
				Content: "You have used your tool budget for this turn. Answer now with what you have, and say plainly if something is missing."})
			break
		}
		offer := schemas
		// deep_search runs a whole pipeline on the same single GPU slot this
		// turn is using, so a second call is a second minute of everything else
		// waiting. The prompt asks for one, and this is what makes it one.
		if usedDeepSearch {
			offer = tools.Without(offer, tools.DeepSearch.Name)
		}
		if usedNews {
			offer = tools.Without(offer, tools.News.Name)
		}
		if repeats >= 2 {
			// It is going in circles. Take the tools away and make it answer
			// with what it has rather than spending the rest of the budget.
			offer = nil
		}
		emit(Event{Kind: "status", Text: thinkingLabel(round)})
		roundStart := time.Now()
		var reply Message
		var st Stats
		var err error
		if forceTools && len(offer) > 0 {
			reply, st, err = e.llm.CompleteRequiringTool(ctx, msgs, offer, toolTurnTokens)
		} else {
			reply, st, err = e.llm.CompleteStats(ctx, msgs, offer, toolTurnTokens)
		}
		forcedThisRound := forceTools
		forceTools = false
		stats.merge(st)
		on := "the conversation so far, plus " + itoa(len(offer)) + " tools on the table"
		if forcedThisRound {
			on += ", and it was made to call one"
		}
		tr.Add(Step{Kind: "model", Label: "round " + itoa(round+1) + ", decide",
			In:  on,
			Out: decision(reply), MS: time.Since(roundStart).Milliseconds(), Bad: err != nil,
			Meta: itoa(st.Prompt) + " tokens in, " + itoa(st.Completion) + " out"})
		// A tool call cut off by the token budget arrives as unparseable JSON
		// and llama.cpp refuses the whole request, which would otherwise lose an
		// answer the model was most of the way through. Asking again with the
		// tools off is always answerable, since by then it has whatever the
		// earlier rounds fetched.
		if err != nil && isTruncatedToolCall(err) {
			emit(Event{Kind: "status", Text: "answering"})
			reply, st, err = e.llm.CompleteStats(ctx, msgs, nil, toolTurnTokens)
			stats.merge(st)
			if err == nil {
				break
			}
		}
		if err != nil {
			return Message{}, used, nil, deps.Widgets.List(), stats, err
		}
		if len(reply.ToolCalls) == 0 {
			// A model sometimes writes its tool call syntax as ordinary text,
			// which llama.cpp cannot parse and hands back as content. Recover
			// the call so the turn is not wasted, and strip the markup either
			// way so it never reaches the page.
			cleaned, salvaged := salvageCalls(reply.Content, func(n string) bool {
				_, ok := e.reg.Get(n)
				return ok
			})
			if len(salvaged) > 0 {
				reply.Content = cleaned
				reply.ToolCalls = salvaged
			} else {
				// It stopped calling tools, which is not the same as having
				// answered. A reply that offers to go and check, or that
				// asserts things nothing in this turn checked, goes back with
				// the tools still on rather than becoming the answer.
				if gates < maxGates && round < maxToolRounds-1 {
					gateStart := time.Now()
					nudge, gst := e.gate(ctx, user, reply.Content, answered, used, emit)
					stats.merge(gst)
					tr.Add(Step{Kind: "gate", Label: "checked the draft before sending it",
						In: reply.Content, Out: gateOutcome(nudge),
						MS: time.Since(gateStart).Milliseconds(), Bad: nudge != ""})
					if nudge != "" {
						gates++
						// The draft itself is never appended. A model handed
						// its own text back writes it again.
						msgs = append(msgs, Message{Role: RoleUser, Content: nudge})
						forceTools = true
						continue
					}
				}
				// It answered without tools. Stream it properly rather than
				// handing back a block of text that appeared all at once.
				break
			}
		}
		msgs = append(msgs, reply)
		for _, tc := range reply.ToolCalls {
			key := tc.Function.Name + "\x00" + canonArgs(tc.Function.Arguments)
			if prev, done := seen[key]; done {
				repeats++
				emit(Event{Kind: "tool", Tool: tc.Function.Name, Args: shortArgs(tc.Function.Arguments)})
				emit(Event{Kind: "tool_done", Tool: prev.Name, MS: 0, OK: prev.Err == ""})
				body, _ := json.Marshal(map[string]any{
					"repeat": true,
					"note": "You already called this tool with these exact arguments in this turn. " +
						"The result is below and it will not change. Do not call it again. " +
						"Use what you have, or try different arguments, or answer and say what is missing.",
					"result": prev.Content,
				})
				id := tc.ID
				if id == "" {
					id = tc.Function.Name
				}
				msgs = append(msgs, Message{Role: RoleTool, ToolCallID: id, Name: prev.Name, Content: string(body)})
				continue
			}
			emit(Event{Kind: "tool", Tool: tc.Function.Name, Args: shortArgs(tc.Function.Arguments)})
			if tc.Function.Name == tools.DeepSearch.Name {
				usedDeepSearch = true
			}
			if tc.Function.Name == tools.News.Name {
				usedNews = true
			}
			res := e.reg.Call(ctx, deps, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			seen[key] = res
			used = append(used, res)
			tr.Add(Step{Kind: "tool", Label: res.Name, In: tc.Function.Arguments,
				Out: resultText(res), MS: res.Elapsed.Milliseconds(), Bad: res.Err != "",
				Meta: snapshotMeta(res.Content)})
			emit(Event{Kind: "tool_done", Tool: res.Name, MS: res.Elapsed.Milliseconds(), OK: res.Err == ""})
			// Straight after the tool that named it, so the chart is drawing
			// while the answer is still being written rather than appearing
			// under a finished one.
			for _, wdg := range drained(deps.Widgets, sentWidgets) {
				emit(Event{Kind: "widget", Widget: &wdg})
			}
			body, _ := json.Marshal(res.Content)
			if len(body) > 14000 {
				body = append(body[:14000], []byte(`","truncated":true}`)...)
			}
			id := tc.ID
			if id == "" {
				id = tc.Function.Name
			}
			msgs = append(msgs, Message{Role: RoleTool, ToolCallID: id, Name: res.Name, Content: string(body)})
		}
	}

	// Everything the turn read, numbered. The model is handed the numbers and
	// never an address, so a link under this answer is one a tool fetched.
	srcs := collectSources(used)

	// The answer is generated fresh here rather than reusing whatever the last
	// tool round produced, because that one was written under a small budget
	// with tools still on the table. Without saying so, a model writes the
	// sentence it would have written before calling another tool, which reads
	// as "Let me check that" and then stops.
	msgs = append(msgs, Message{Role: RoleUser, Content: finalTurn + sourcePrompt(srcs)})

	emit(Event{Kind: "status", Text: "writing"})
	answerStart := time.Now()
	var sb strings.Builder
	w := &blockWriter{emit: emit, render: func(md string) string {
		return linkCitations(e.Render(prepare(md, srcs)), srcs)
	}}
	text, st, err := e.llm.Stream(ctx, msgs, answerTokens, func(d string) {
		sb.WriteString(d)
		w.write(d)
	})
	w.flush()
	stats.merge(st)
	tr.Add(Step{Kind: "answer", Label: "wrote the reply", MS: time.Since(answerStart).Milliseconds(),
		Out: sb.String(), Bad: err != nil && sb.Len() == 0,
		Meta: itoa(st.Prompt) + " tokens in, " + itoa(st.Completion) + " out"})
	if err != nil && sb.Len() == 0 {
		return Message{}, used, nil, deps.Widgets.List(), stats, err
	}
	text = prepare(text, srcs)
	if strings.TrimSpace(text) == "" {
		text = "I could not produce an answer for that. The model returned nothing."
		emit(Event{Kind: "block", HTML: e.Render(text)})
	}
	return Message{Role: RoleAssistant, Content: text}, used, cited(text, srcs), deps.Widgets.List(), stats, nil
}

// prepare is everything done to the model's markdown before it is rendered or
// stored: the address dump at the end goes, a schemeless address becomes a
// link, and the citations are repaired. It runs on each finished block as it
// streams and on the whole answer at the end, and agrees with itself because
// every step works a line at a time.
func prepare(md string, srcs []Source) string {
	return attach(linkBareAddresses(dropSourceList(md)), srcs)
}

// blockWriter turns a token stream into finished markdown blocks. It only ever
// closes a block on a blank line that is not inside a fenced code block, since
// a fence is full of blank lines and cutting one in half renders as garbage.
type blockWriter struct {
	emit     func(Event)
	render   func(string) string
	buf      strings.Builder
	fenced   bool
	lastTail string
}

func (w *blockWriter) write(d string) {
	w.buf.WriteString(d)
	for {
		cut, ok := w.boundary(w.buf.String())
		if !ok {
			break
		}
		s := w.buf.String()
		block := strings.TrimRight(s[:cut], "\n")
		rest := s[cut:]
		w.buf.Reset()
		w.buf.WriteString(rest)
		// An empty render is a block the cleanup took out, which is the
		// address list the model still writes at the end sometimes.
		if h := w.render(block); strings.TrimSpace(block) != "" && strings.TrimSpace(h) != "" {
			w.emit(Event{Kind: "block", HTML: h})
		}
		w.lastTail = ""
	}
	tail := w.buf.String()
	if looksLikeCall(tail) {
		// Hold it back rather than showing markup that is about to be removed.
		return
	}
	if tail != w.lastTail {
		w.lastTail = tail
		w.emit(Event{Kind: "tail", Text: tail})
	}
}

// boundary finds the end of the first complete block in s, tracking fences so
// a blank line inside one is not treated as the end of anything.
func (w *blockWriter) boundary(s string) (int, bool) {
	fenced := w.fenced
	at := 0
	lines := strings.SplitAfter(s, "\n")
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		at += len(ln)
		// A blank line outside a fence ends a block, and the last line is not
		// a boundary because more of it may still be coming.
		if !fenced && trimmed == "" && i < len(lines)-1 && at > 0 {
			w.fenced = false
			return at, true
		}
	}
	return 0, false
}

func (w *blockWriter) flush() {
	rest, _ := salvageCalls(w.buf.String(), func(string) bool { return false })
	rest = strings.TrimSpace(rest)
	w.buf.Reset()
	if h := w.render(rest); rest != "" && strings.TrimSpace(h) != "" {
		w.emit(Event{Kind: "block", HTML: h})
	}
	w.emit(Event{Kind: "tail", Text: ""})
}

// looksLikeCall reports whether a chunk is the start of a tool call written as
// prose. The tail is held back once this is true, so half a tag is never shown
// on its way to being stripped.
func looksLikeCall(s string) bool {
	return strings.Contains(s, "<tool_call") || strings.Contains(s, "<function=")
}

const finalTurn = `Write your reply now. You have no tools left for this turn, so do not say you are about to look something up, do not offer to check anything, and do not describe what you would do next. There is nobody to answer an offer.

Use the tool results above and what this conversation has already established. Every name, title, date, number and price in your answer has to come from one of those two, and a fact you cannot point at is one to leave out. Do not quote a page you read in an earlier turn, since it is not in front of you now. Do not attach a title to the wrong person or a place to the wrong country, which is the mistake to check for before you write a name.

Give the whole answer in one go, with the specifics. If a part is still missing, say which part in one line and answer the rest. If the last message was not a question, just reply to it, a line or two is the whole job.`

func thinkingLabel(round int) string {
	if round == 0 {
		return "thinking"
	}
	return "checking"
}

// sizeArgs say how much to return rather than what to fetch. They are left out
// of the repeat key, because asking for the same page again with a bigger limit
// is the same call and it is exactly how a model talks itself into a loop.
var sizeArgs = map[string]bool{"n": true, "max_chars": true, "top": true, "limit": true, "days": true}

// canonArgs is a stable key for a set of arguments, so the same call written
// with the keys in a different order is still recognised as the same call.
func canonArgs(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return strings.TrimSpace(raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if sizeArgs[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v;", k, m[k])
	}
	return strings.ToLower(b.String())
}

// shortArgs is what the UI shows beside a tool chip. The whole argument object
// is noise in a status line.
func shortArgs(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, k := range []string{"query", "location", "symbols", "url", "expression", "artist", "league"} {
		if v, ok := m[k]; ok {
			s := fmt.Sprint(v)
			if len(s) > 60 {
				s = s[:57] + "..."
			}
			return s
		}
	}
	return ""
}

// isTruncatedToolCall spots the refusal llama.cpp returns when the arguments of
// a tool call did not parse. It is matched on the message because the status is
// a plain 500 that says nothing else.
func isTruncatedToolCall(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "tool call") && strings.Contains(m, "parse")
}

// drained returns the widgets a sink has gained since it was last read. The
// sink keeps the whole turn's list because that is what gets stored on the
// message, so emitting has to track what it already sent.
func drained(sink *tools.Sink, sent map[string]bool) []tools.Widget {
	var out []tools.Widget
	for _, w := range sink.List() {
		k := w.Kind + "\x00" + w.Symbol + "\x00" + w.Place
		if sent[k] {
			continue
		}
		sent[k] = true
		out = append(out, w)
	}
	return out
}

// decision says what a round chose, which is the part of a reply worth showing
// when the reply itself is a tool call rather than prose.
func decision(m Message) string {
	if len(m.ToolCalls) == 0 {
		return "answered without calling anything"
	}
	var names []string
	for _, tc := range m.ToolCalls {
		names = append(names, tc.Function.Name+"("+shortArgs(tc.Function.Arguments)+")")
	}
	return "called " + strings.Join(names, ", ")
}

func gateOutcome(nudge string) string {
	if nudge == "" {
		return "let it through"
	}
	return "sent it back: " + nudge
}

func resultText(r tools.Result) string {
	if r.Err != "" {
		return r.Err
	}
	b, err := json.Marshal(r.Content)
	if err != nil {
		return ""
	}
	return string(b)
}

// snapshotMeta says how old a tool's data is, for the tools that read something
// dated rather than the live thing.
func snapshotMeta(content any) string {
	if d := snapshotAge(content); d != "" {
		return "snapshot taken " + d
	}
	return ""
}
