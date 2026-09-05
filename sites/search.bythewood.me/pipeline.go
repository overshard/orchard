package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"search.bythewood.me/skills"
)

const (
	serpTTL    = 6 * time.Hour
	pageTTL    = 14 * 24 * time.Hour
	maxSources = 5

	// Below this share of cited sentences holding up, the answer is treated as
	// wrong rather than imperfect, and the pipeline searches again with what it
	// learned. Most of the answer failing means the search found the wrong
	// pages, which no amount of rewriting fixes.
	retryBelowSupport = 0.5
)

// Source is one document that made it into the answer's evidence.
type Source struct {
	N         int
	URL       string
	Title     string
	Site      string
	Published string
	FromCache bool
}

// Passage is a numbered piece of evidence. The model is shown these IDs and
// never a URL, so a citation it emits always resolves back to real text.
type Passage struct {
	ID     int
	Source int
	Text   string
}

// Citation records the validation verdict for one sentence of the answer.
type Citation struct {
	Sentence  string
	PassageID int
	Source    int
	Supported bool
	Checked   bool
	// Repaired names the passage a failing claim was re-pointed at, when a
	// different passage turned out to support it.
	Repaired int
	Note     string
}

// Answer is the finished product.
type Answer struct {
	Query      string
	Standalone string
	Shape      Shape
	Text       string
	HTML       string
	Sources    []Source
	Passages   []Passage
	Citations  []Citation
	Queries    []string
	Elapsed    string
	Warnings   []string
	Retried    bool
	Support    float64

	// Skill names the handler that answered, when something other than a web
	// search did. Empty means the normal pipeline.
	Skill string

	// Links are checked URLs for the things the answer named, which the source
	// list does not cover: sources say where the research came from, not where
	// the subject lives.
	Links []EntityLink

	// Checks and Deps are the code shape's own verification, since a file that
	// does not parse and a package that does not exist are what go wrong in an
	// answer somebody is going to paste.
	Checks []CodeCheck
	Deps   []Dependency
}

// Progress reports pipeline steps to whoever is watching. The web handler wires
// it to an SSE stream; tests leave it nil.
type Progress func(step, detail string)

func (p Progress) send(step, detail string) {
	if p != nil {
		p(step, detail)
	}
}

// Engine runs the pipeline.
type Engine struct {
	store  *Store
	llm    *LLM
	client *http.Client
	budget *Budget
	skills *skills.Registry
}

func NewEngine(store *Store, llm *LLM, budget *Budget) *Engine {
	return &Engine{
		store: store, llm: llm, client: newHTTPClient(),
		budget: budget, skills: skills.Default(),
	}
}

func (e *Engine) skillDeps() skills.Deps {
	return skills.Deps{HTTP: e.client, UA: browserUA, Now: localNow}
}

// skillAnswer converts a skill result into the answer the rest of the site
// expects. Rendering happens here rather than in the skill, since the template
// and its renderer belong to this package.
func skillAnswer(question string, r *skills.Result, start time.Time) *Answer {
	a := &Answer{
		Query: question, Standalone: question, Skill: r.Skill,
		Shape: Shape(r.Shape), Text: r.Text, HTML: renderMarkdown(r.Text),
		Support: 1,
		Elapsed: time.Since(start).Round(10 * time.Millisecond).String(),
	}
	if a.Shape == "" {
		a.Shape = ShapeFactual
	}
	for i, src := range r.Sources {
		a.Sources = append(a.Sources, Source{
			N: i + 1, URL: src.URL, Title: src.Title, Site: src.Site,
		})
	}
	return a
}

// Run executes the pipeline: plan, gather, select, synthesize, validate, and
// retry once if the answer did not hold up.
func (e *Engine) Run(ctx context.Context, question string, history []Turn, pr Progress) (*Answer, error) {
	start := time.Now()

	// The model loads while the search and the fetches are in flight.
	go e.llm.Warm(ctx)

	// Routing is the first model call, and it is the one classification a 4B is
	// reliably good at. The skill names are an enum in the grammar rather than
	// an instruction in the prompt, so the model cannot name a handler that
	// does not exist, and every card carries the near misses that should not
	// fire it as well as the phrasings that should.
	//
	// A keyword matcher used to do this and it was wrong in both directions,
	// claiming "what is the S&P 500" for the quote skill and missing "do i need
	// a jacket today" entirely. The matcher survives inside the registry as the
	// fallback for when the model is unreachable.
	pr.send("route", "working out what kind of question this is")
	if res, name := e.skills.Run(ctx, e.llm, question, e.skillDeps()); res != nil {
		pr.send("skill", "answered from "+name)
		return skillAnswer(question, res, start), nil
	}

	standalone := question
	if len(history) > 0 {
		pr.send("followup", "reading the previous answer")
		standalone = e.rewriteFollowup(ctx, question, history)
	}

	ans := &Answer{Query: question, Standalone: standalone}

	pr.send("plan", "working out what to search for")
	plan := e.plan(ctx, standalone, "")
	ans.Shape = plan.Shape
	ans.Queries = plan.Queries
	contract := contractFor(plan.Shape)
	pr.send("plan", fmt.Sprintf("%s question, searching: %s", plan.Shape, strings.Join(plan.Queries, " / ")))

	if err := e.round(ctx, ans, plan, contract, pr); err != nil {
		return nil, err
	}

	// One self-correction round. A mostly-unsupported answer is evidence the
	// search missed, so the retry re-plans with the failure described rather
	// than rewording the same evidence.
	//
	// A code answer is judged on the code. Its prose is three caveats, so one
	// mis-cited caveat drops the support rate under the floor and would throw
	// away a file that parsed, and a file that did not parse is worth a second
	// search however well the caveats held up.
	var badCode bool
	if ans.Shape == ShapeCode {
		badCode = codeFailed(ans.Checks, ans.Deps)
	}
	if badCode || (ans.Shape != ShapeCode && ans.Support < retryBelowSupport && len(ans.Citations) > 0) {
		hint := e.failureHint(ans)
		if badCode {
			pr.send("retry", "the code did not hold up, searching again")
			hint = codeHint(ans.Checks, ans.Deps)
		} else {
			pr.send("retry", fmt.Sprintf("only %.0f%% of that held up, searching again", ans.Support*100))
		}
		retryPlan := e.plan(ctx, standalone, hint)
		// The retry keeps the first plan's shape, since the failure was in the
		// answer and re-classifying can only move a code question off the
		// contract that was right for it.
		retryPlan.Shape = ans.Shape
		retry := &Answer{Query: question, Standalone: standalone, Shape: retryPlan.Shape, Queries: retryPlan.Queries, Retried: true}
		if err := e.round(ctx, retry, retryPlan, contractFor(retryPlan.Shape), pr); err == nil && betterAnswer(retry, ans, badCode) {
			if badCode {
				retry.Warnings = append(retry.Warnings, "the first attempt was thrown away because its code did not check out")
			} else {
				retry.Warnings = append(retry.Warnings,
					fmt.Sprintf("first attempt was rejected, %.0f%% of its sentences were unsupported", (1-ans.Support)*100))
			}
			ans = retry
		}
	}

	ans.HTML = renderMarkdown(ans.Text)
	ans.Elapsed = time.Since(start).Round(100 * time.Millisecond).String()
	return ans, nil
}

// round is one full attempt: gather, select, synthesize, validate, repair.
func (e *Engine) round(ctx context.Context, ans *Answer, plan Plan, contract Contract, pr Progress) error {
	pr.send("search", fmt.Sprintf("running %d search%s",
		len(plan.Queries), map[bool]string{true: "", false: "es"}[len(plan.Queries) == 1]))
	results := e.gather(ctx, plan.Queries, ans, pr)
	if len(results) == 0 {
		if len(ans.Warnings) > 0 {
			return fmt.Errorf("%s", ans.Warnings[0])
		}
		return fmt.Errorf("no search results for that question")
	}

	pr.send("fetch", fmt.Sprintf("reading %d pages", min(len(results), maxSources*2)))
	sources, passages, links := e.collect(ctx, results, ans.Standalone, contract, pr)
	if len(passages) == 0 {
		return fmt.Errorf("nothing readable found for that question")
	}
	ans.Sources, ans.Passages = sources, passages

	pr.send("write", "writing the answer")
	text, err := e.synthesize(ctx, ans.Standalone, passages, contract)
	if err != nil {
		return err
	}
	ans.Text = strings.TrimSpace(dropMeta(dropMissingFields(tidyCitations(text))))
	if contract.Shape == ShapeCode {
		pr.send("code", "parsing every file and looking up what it imports")
		// Before anything reads the answer, since a marker left in a file is
		// pasted into it and the fix has to happen once, at the source.
		ans.Text = stripCodeCitations(ans.Text)
		blocks := codeBlocks(ans.Text)
		// After the blocks are read, since that is where the file name comes
		// from when the model wrote it as a comment.
		ans.Text = liftFileComments(ans.Text)
		ans.Checks = checkCode(blocks)
		ans.Warnings = append(ans.Warnings, codeWarnings(ans.Checks)...)
		ans.Warnings = append(ans.Warnings, unusedConstants(blocks)...)
		if len(blocks) == 0 {
			ans.Warnings = append(ans.Warnings, "this came back with no code in it, so it is an explanation rather than something to paste")
		}
	}
	// Only the time sensitive shapes, since "scheduled for April 2026" in a
	// recipe is not a claim about now.
	if contract.Shape == ShapeStatus || contract.Shape == ShapeNews {
		now := localNow()
		ans.Warnings = append(ans.Warnings, staleFutures(ans.Text, now)...)
		ans.Warnings = append(ans.Warnings, staleNow(ans.Text, now)...)
	}

	// Linking runs alongside validation rather than after it. Neither needs the
	// other's result and both are several model or network calls.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ans.Links = e.linkEntities(ctx, ans.Standalone, text, links)
	}()
	if contract.Shape == ShapeCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ans.Deps = verifyDeps(ctx, e.client, codeBlocks(ans.Text))
		}()
	}

	pr.send("check", "checking every sentence against its source")
	// The prose only, because a line of code is not a claim: entailing it costs
	// a model call that can only fail, and `rows[0]` reads as a citation.
	ans.Citations = e.validate(ctx, proseOnly(ans.Text), passages)
	ans.Support = supportRate(ans.Citations)
	// An answer with nothing to check renders without the verification panel,
	// which looks the same as an answer that had nothing wrong with it. On a
	// site whose claim is that every sentence is checked, silence is the
	// misleading option, so it says so instead.
	if countChecked(ans.Citations) == 0 && strings.TrimSpace(ans.Text) != "" {
		note := "nothing in this answer carries a citation, so none of it was checked against a source"
		if contract.Shape == ShapeCode {
			// The code itself was parsed and its packages looked up, so saying
			// nothing was checked would be wrong in the other direction.
			note = "the writing around the code carries no citation, so only the code itself was checked"
		}
		ans.Warnings = append(ans.Warnings, note)
	}

	wg.Wait()
	ans.Warnings = append(ans.Warnings, depWarnings(ans.Deps)...)
	if len(ans.Links) > 0 {
		pr.send("check", fmt.Sprintf("found %d verified link%s", len(ans.Links),
			map[bool]string{true: "", false: "s"}[len(ans.Links) == 1]))
	}
	return nil
}

// betterAnswer decides whether the second attempt replaces the first. For code
// that is whether it fixed what was broken, and for everything else whether
// more of it held up.
func betterAnswer(retry, first *Answer, wasCode bool) bool {
	if wasCode {
		return !codeFailed(retry.Checks, retry.Deps) && len(retry.Checks) > 0
	}
	return retry.Support > first.Support
}

func countChecked(cs []Citation) int {
	n := 0
	for _, c := range cs {
		if c.Checked {
			n++
		}
	}
	return n
}

func supportRate(cs []Citation) float64 {
	checked, ok := 0, 0
	for _, c := range cs {
		if !c.Checked {
			continue
		}
		checked++
		if c.Supported {
			ok++
		}
	}
	// Nothing checked is not the same as nothing wrong, but the caller warns
	// about that case rather than reading a rate that has no denominator.
	if checked == 0 {
		return 1
	}
	return float64(ok) / float64(checked)
}

// failureHint describes what went wrong so the second plan does not repeat the
// first one's mistake.
func (e *Engine) failureHint(ans *Answer) string {
	var bad []string
	for _, c := range ans.Citations {
		if c.Checked && !c.Supported {
			bad = append(bad, stripCitations(c.Sentence))
		}
		if len(bad) >= 3 {
			break
		}
	}
	return fmt.Sprintf(
		"A previous search used the queries %q and the pages it found did not support the answer. "+
			"These claims could not be backed by anything found: %s. "+
			"Write different queries that would find pages actually stating the answer.",
		strings.Join(ans.Queries, ", "), strings.Join(bad, " | "))
}

// Plan is the output of the planning step.
type Plan struct {
	Queries []string `json:"queries"`
	Shape   Shape    `json:"shape"`
}

func (e *Engine) plan(ctx context.Context, question, hint string) Plan {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"minItems": 1, "maxItems": 3,
			},
			"shape": map[string]any{
				"type": "string",
				"enum": []string{"factual", "recipe", "howto", "comparison", "news", "status", "code"},
			},
		},
		"required":             []string{"queries", "shape"},
		"additionalProperties": false,
	}
	system := strings.Join([]string{
		AmbientContext(),
		"You plan a web search. Return short keyword queries that would find pages answering the question, and the shape of answer it wants.",
		"recipe: the user wants something they can cook from. comparison: two or more options weighed.",
		"code: the user wants something they can paste into a file and run, such as a script, a program, a config file, a query or a command. Anything asking to write, build or give code is this. So is a question about how or where to do something on a computer whose answer is a command, a query or a config, even when it is worded as the best way to do it, because what that reader wants is the command.",
		"Write code queries naming the language, the library, the platform and the version, and prefer official documentation over roundups.",
		"howto: ordered steps a person carries out in an interface or on hardware, where nothing gets typed into a file.",
		"news: something that already happened, including sports results and recent events.",
		"factual: everything else.",
		"For news, write queries that would find what happened, using words like result, final score, or the current month and year, not words like schedule, fixtures or upcoming.",
		"status: the user wants to know where an ongoing thing stands now, such as a court case, an investigation or a rollout. Write queries carrying the current month and year so the newest coverage is found rather than the first report.",
		"No sentences, no quotes, no search operators.",
	}, " ")
	user := question
	if hint != "" {
		user = question + "\n\n" + hint
	}

	var out Plan
	if err := e.llm.Structured(ctx, system, user, 250, schema, &out); err != nil || len(out.Queries) == 0 {
		slog.Warn("plan failed, falling back", slog.Any("err", err))
		return Plan{Queries: []string{question}, Shape: guessShape(question)}
	}
	for i, q := range out.Queries {
		out.Queries[i] = strings.TrimSpace(q)
	}
	if out.Shape == "" {
		out.Shape = guessShape(question)
	}
	return out
}

// rewriteFollowup turns "what about the vegetarian version" into a question
// that stands on its own, because the search engine has no conversation.
func (e *Engine) rewriteFollowup(ctx context.Context, question string, history []Turn) string {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"question": map[string]any{"type": "string"}},
		"required":             []string{"question"},
		"additionalProperties": false,
	}
	var b strings.Builder
	for _, t := range history {
		fmt.Fprintf(&b, "Q: %s\nA: %s\n\n", t.Question, truncate(t.Answer, 700))
	}
	var out struct {
		Question string `json:"question"`
	}
	err := e.llm.Structured(ctx,
		AmbientContext()+" Rewrite the user's follow-up into one standalone question that carries over whatever it refers to from the conversation. Keep it short. If it already stands alone, return it unchanged.",
		fmt.Sprintf("Conversation so far:\n\n%sFollow-up: %s", b.String(), question),
		200, schema, &out)
	if err != nil || strings.TrimSpace(out.Question) == "" {
		return question
	}
	return strings.TrimSpace(out.Question)
}

func (e *Engine) gather(ctx context.Context, queries []string, ans *Answer, pr Progress) []Result {
	var all []Result
	seen := map[string]bool{}
	for _, q := range queries {
		results := e.store.CachedSERP(q, serpTTL)
		if results == nil {
			// A cached query costs nothing, so the budget only guards the ones
			// that actually leave the house.
			if st := e.budget.State(); st.Left == 0 {
				ans.Warnings = append(ans.Warnings, fmt.Sprintf(
					"search allowance is spent, skipped %q (room opens in about %ds)", q, st.ResetIn))
				continue
			}
			// Only uncached queries pace, so a repeat question still returns at
			// once. The wait is reported because a silent pause looks like a
			// hang, and this one is deliberate.
			if waited := e.budget.Wait(ctx); waited > 500*time.Millisecond {
				pr.send("search", fmt.Sprintf("paused %.1fs between searches", waited.Seconds()))
			}
			if ctx.Err() != nil {
				return all
			}
			e.budget.Spend()
			var err error
			results, err = SearchDDG(e.client, q, 8)
			if err != nil {
				if err == errRateLimited {
					e.budget.Limited()
				}
				ans.Warnings = append(ans.Warnings, fmt.Sprintf("search %q: %v", q, err))
				continue
			}
			if err := e.store.PutSERP(q, results); err != nil {
				slog.Warn("serp cache write", slog.Any("err", err))
			}
		}
		for _, r := range results {
			if !seen[r.URL] {
				seen[r.URL] = true
				all = append(all, r)
			}
		}
	}
	return all
}

// collect fetches pages in parallel, then picks the chunks that answer the
// question rather than the ones that happen to come first.
func (e *Engine) collect(ctx context.Context, results []Result, question string, contract Contract, pr Progress) ([]Source, []Passage, []Link) {
	if len(results) > maxSources*2 {
		results = results[:maxSources*2]
	}

	type fetched struct {
		idx    int
		page   *Page
		id     int64
		cached bool
	}
	var (
		mu   sync.Mutex
		got  []fetched
		wg   sync.WaitGroup
		sema = make(chan struct{}, 5)
		done int
	)
	for i, r := range results {
		wg.Add(1)
		go func(i int, r Result) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() { <-sema }()

			var (
				p      *Page
				id     int64
				cached bool
			)
			if hit := e.store.CachedPage(r.URL, pageTTL); hit != nil {
				p, id, cached = hit, e.store.PageID(r.URL), true
			} else {
				var err error
				p, err = Fetch(e.client, r.URL)
				if err != nil {
					slog.Debug("fetch skipped", slog.String("url", r.URL), slog.Any("err", err))
					return
				}
				if id, err = e.store.PutPage(p); err != nil {
					slog.Warn("page cache write", slog.Any("err", err))
				}
			}
			mu.Lock()
			got = append(got, fetched{i, p, id, cached})
			done++
			pr.send("fetch", fmt.Sprintf("read %s", hostname(r.URL)))
			mu.Unlock()
		}(i, r)
	}
	wg.Wait()

	// Search order normally, but two shapes have a better ordering than the
	// search engine's.
	switch {
	// A status question is answered by whichever source is newest, so a dated
	// page outranks a well ranked stale one.
	case contract.Shape == ShapeStatus:
		sort.SliceStable(got, func(a, b int) bool {
			return got[a].page.Published > got[b].page.Published
		})
	// "breakfast burrito recipe" is the exact phrase every roundup is written
	// to rank for, so the first page of results is listicles naming twelve
	// recipes and giving the quantities for none. A page publishing a
	// schema.org Recipe is an actual recipe, and it is the only place the
	// quantities exist, so it goes first whatever the search engine thought.
	// A page with no code on it cannot show how the code is written, whatever
	// it ranks for, and the first page of results for anything with a language
	// name in it is content marketing.
	case contract.Shape == ShapeCode:
		sort.SliceStable(got, func(a, b int) bool {
			ca, cb := codeWeight(got[a].page), codeWeight(got[b].page)
			if ca != cb {
				return ca > cb
			}
			return got[a].idx < got[b].idx
		})
	case contract.Shape == ShapeRecipe:
		sort.SliceStable(got, func(a, b int) bool {
			ra, rb := hasStructuredRecipe(got[a].page), hasStructuredRecipe(got[b].page)
			if ra != rb {
				return ra
			}
			return got[a].idx < got[b].idx
		})
	default:
		sort.Slice(got, func(a, b int) bool { return got[a].idx < got[b].idx })
	}
	if len(got) > maxSources {
		got = got[:maxSources]
	}

	var sources []Source
	var passages []Passage
	var links []Link
	for i, f := range got {
		n := i + 1
		links = append(links, e.store.PageLinks(f.id)...)
		sources = append(sources, Source{
			N: n, URL: f.page.URL, Title: f.page.Title,
			Site: f.page.Site, Published: f.page.Published, FromCache: f.cached,
		})

		// Relevance first, document order as the fallback when the question's
		// words do not appear (which happens on pages that answer it anyway).
		chunks := e.store.RankPassages(f.id, question, contract.PerSource)
		if len(chunks) == 0 {
			chunks = e.store.PageChunks(f.id, contract.PerSource)
		}
		for _, text := range chunks {
			passages = append(passages, Passage{ID: len(passages) + 1, Source: n, Text: text})
		}
	}
	if len(passages) > contract.MaxPassages {
		passages = passages[:contract.MaxPassages]
	}
	return sources, passages, links
}

func (e *Engine) synthesize(ctx context.Context, question string, passages []Passage, contract Contract) (string, error) {
	var b strings.Builder
	for _, p := range passages {
		fmt.Fprintf(&b, "[%d] %s\n\n", p.ID, truncate(p.Text, 1800))
	}
	system := strings.Join([]string{
		AmbientContext(),
		"You answer using only the numbered passages provided.",
		"Cite every factual sentence with the passage number it came from, like [3]. A sentence may carry more than one.",
		"Never state anything the passages do not support, and never fill a gap from your own knowledge.",
		"If the passages do not answer the question that was asked, say so in the first sentence and then say what they do cover. Do not answer a nearby question instead.",
		"Never put a citation on a statement that something is missing, unknown or not specified, and prefer leaving that line out entirely.",
		"Write markdown. Use **bold** for the facts worth skimming.",
		contract.Instruction,
	}, " ")
	user := fmt.Sprintf("Passages:\n\n%s\nQuestion: %s", b.String(), question)
	return e.llm.Complete(ctx, system, user, contract.MaxTokens)
}

// validate checks each cited sentence against the passage it cites, then tries
// to repair the ones that fail by looking for a passage that does support them.
func (e *Engine) validate(ctx context.Context, text string, passages []Passage) []Citation {
	byID := map[int]Passage{}
	for _, p := range passages {
		byID[p.ID] = p
	}

	var out []Citation
	for _, sentence := range splitClaims(text) {
		ids := citedIDs(sentence)
		if len(ids) == 0 {
			continue
		}
		claim := stripCitations(sentence)
		for _, id := range ids {
			p, ok := byID[id]
			if !ok {
				out = append(out, Citation{
					Sentence: plainText(sentence), PassageID: id, Checked: true,
					Note: "cites a passage that does not exist",
				})
				continue
			}
			c := Citation{Sentence: plainText(sentence), PassageID: id, Source: p.Source}
			supported, err := e.entails(ctx, p.Text, claim)
			if err == nil {
				c.Checked = true
				c.Supported = supported
			}
			if c.Checked && !c.Supported {
				// The claim may be true and merely mis-cited, which is a
				// different problem from an invented one and worth telling
				// apart.
				if alt := e.findSupport(ctx, claim, passages, id); alt > 0 {
					c.Supported = true
					c.Repaired = alt
					c.Note = fmt.Sprintf("cited [%d] but [%d] is what supports it", id, alt)
				} else {
					c.Note = "no fetched passage states this"
				}
			}
			out = append(out, c)
		}
	}
	return out
}

func (e *Engine) entails(ctx context.Context, passage, claim string) (bool, error) {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"supported": map[string]any{"type": "boolean"}},
		"required":             []string{"supported"},
		"additionalProperties": false,
	}
	var v struct {
		Supported bool `json:"supported"`
	}
	err := e.llm.Structured(ctx,
		strings.Join([]string{
			"You check whether a passage supports a claim.",
			"Answer true if the passage states the claim, directly implies it, or describes the same step or item in different words.",
			"A paraphrase is still supported. A claim naming something the passage never mentions is not.",
			"Answer only with the JSON field.",
		}, " "),
		fmt.Sprintf("Passage:\n%s\n\nClaim:\n%s", truncate(passage, 1800), claim),
		40, schema, &v)
	return v.Supported, err
}

// findSupport looks for another passage that backs a failing claim. It checks
// the most textually similar ones first rather than all of them, since every
// check is a model call.
func (e *Engine) findSupport(ctx context.Context, claim string, passages []Passage, skip int) int {
	type scored struct {
		id    int
		score int
	}
	var cands []scored
	words := contentWords(claim)
	for _, p := range passages {
		if p.ID == skip {
			continue
		}
		cands = append(cands, scored{p.ID, overlap(words, contentWords(p.Text))})
	}
	sort.Slice(cands, func(a, b int) bool { return cands[a].score > cands[b].score })

	byID := map[int]Passage{}
	for _, p := range passages {
		byID[p.ID] = p
	}
	for i, c := range cands {
		if i >= 3 || c.score == 0 {
			break
		}
		if ok, err := e.entails(ctx, byID[c.id].Text, claim); err == nil && ok {
			return c.id
		}
	}
	return 0
}

func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.Fields(strings.ToLower(s)) {
		w := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, f)
		if len(w) > 3 && !stopword[w] {
			out[w] = true
		}
	}
	return out
}

func overlap(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}

func citedIDs(s string) []int {
	var ids []int
	seen := map[int]bool{}
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			break
		}
		n, ok := 0, j > 1
		for _, c := range s[i+1 : i+j] {
			if c < '0' || c > '9' {
				ok = false
				break
			}
			n = n*10 + int(c-'0')
		}
		if ok && n > 0 && !seen[n] {
			seen[n] = true
			ids = append(ids, n)
		}
		i += j
	}
	return ids
}

func stripCitations(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// splitClaims breaks an answer into the units worth checking.
//
// A run of list items citing the same passage is one claim, not one per line.
// An ingredient list produced fifteen separate model calls that all asked the
// same question of the same passage, which was slow and told the reader
// nothing. Prose splits on sentences as before.
const shortItem = 80

func splitClaims(text string) []string {
	var out []string
	var bullets []string
	var bulletCite string

	flush := func() {
		if len(bullets) == 0 {
			return
		}
		out = append(out, strings.Join(bullets, "; "))
		bullets = nil
		bulletCite = ""
	}

	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			flush()
			continue
		}
		if item, ordered, ok := listItem(t); ok {
			// A numbered list is a sequence of steps and each step is its own
			// claim, since one wrong step in a recipe is the whole problem. A
			// short bulleted run is a list of things and reads as one claim.
			if ordered || len(item) > shortItem {
				flush()
				out = append(out, item)
				continue
			}
			ids := citedIDs(t)
			key := ""
			if len(ids) > 0 {
				key = fmt.Sprint(ids)
			}
			// A change of citation ends the run, since the group is only one
			// claim while every line points at the same evidence.
			if bulletCite != "" && key != bulletCite {
				flush()
			}
			bulletCite = key
			bullets = append(bullets, item)
			continue
		}
		flush()
		out = append(out, splitSentences(t)...)
	}
	flush()

	var kept []string
	for _, c := range out {
		if len(strings.TrimSpace(c)) > 15 {
			kept = append(kept, strings.TrimSpace(c))
		}
	}
	return kept
}

// listItem recognises a markdown bullet or numbered line, returning its text
// and whether the list was ordered.
func listItem(t string) (text string, ordered bool, ok bool) {
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
		return strings.TrimSpace(t[2:]), false, true
	}
	for i := 0; i < len(t) && i < 3; i++ {
		if t[i] >= '0' && t[i] <= '9' {
			continue
		}
		if i > 0 && (t[i] == '.' || t[i] == ')') && i+1 < len(t) && t[i+1] == ' ' {
			return strings.TrimSpace(t[i+2:]), true, true
		}
		break
	}
	return "", false, false
}

// splitSentences is deliberately crude. It only has to find units small enough
// to check individually, so a split inside an abbreviation costs nothing.
func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder
	for i, r := range text {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			rest := text[i+1:]
			if rest == "" || rest[0] == ' ' || rest[0] == '\n' {
				if s := strings.TrimSpace(cur.String()); len(s) > 15 {
					out = append(out, s)
				}
				cur.Reset()
			}
		}
	}
	if s := strings.TrimSpace(cur.String()); len(s) > 15 {
		out = append(out, s)
	}
	return out
}

// dropMissingFields removes lines whose whole content is that the passages did
// not say something. The prompt asks the model to leave them out and it writes
// them anyway, and "Total Time: Not specified" is noise in a recipe rather than
// an answer.
func dropMissingFields(text string) string {
	return strings.TrimSpace(eachProseLine(text, func(line string) (string, bool) {
		l := strings.ToLower(plainText(line))
		if strings.Contains(l, "not specified") || strings.Contains(l, "not mentioned") ||
			strings.Contains(l, "not provided") || strings.Contains(l, "not given") {
			// Only when the line is a field, not when it is a real sentence
			// saying the sources fall short of the question.
			if len(l) < 90 && strings.Count(l, " ") < 12 {
				return "", false
			}
		}
		return line, true
	}))
}

// plainText strips the markdown emphasis so a claim reads as a sentence in the
// validation list rather than as source.
func plainText(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "__", "")
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
