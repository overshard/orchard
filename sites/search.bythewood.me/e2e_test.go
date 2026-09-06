package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPipelineLive runs the real thing against the real web and the real model.
// It skips when the model is not up, so `make test` on a machine without a GPU
// still passes.
func TestPipelineLive(t *testing.T) {
	url := os.Getenv("LLM_URL")
	if url == "" {
		url = "http://search-llm:8091"
	}
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to spend real searches")
	}
	llm := NewLLM(url)
	if !llm.Healthy(context.Background()) {
		t.Skip("model not up")
	}

	dir, _ := os.MkdirTemp("", "search-e2e")
	defer os.RemoveAll(dir)
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	e := NewEngine(store, llm, NewBudget())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var steps []string
	pr := Progress(func(step, detail string) { steps = append(steps, step+": "+detail) })

	ans, err := e.Run(ctx, "What is SQLite FTS5 and what is it used for?", nil, pr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, s := range steps {
		t.Logf("  %s", s)
	}
	t.Logf("shape=%s elapsed=%s support=%.0f%%", ans.Shape, ans.Elapsed, ans.Support*100)
	t.Logf("\n%s", ans.Text)
	if ans.Text == "" {
		t.Fatal("empty answer")
	}
	if len(ans.Sources) == 0 {
		t.Fatal("no sources")
	}
	if !strings.Contains(ans.HTML, "<") {
		t.Error("html was not rendered")
	}
}

// TestGivenPageLive is the other half of the pipeline: a question that names
// the page to read never plans a search, and the answer comes off that page
// and nothing else.
func TestGivenPageLive(t *testing.T) {
	url := os.Getenv("LLM_URL")
	if url == "" {
		url = "http://search-llm:8091"
	}
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to fetch a real page")
	}
	llm := NewLLM(url)
	if !llm.Healthy(context.Background()) {
		t.Skip("model not up")
	}

	dir, _ := os.MkdirTemp("", "search-page")
	defer os.RemoveAll(dir)
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	e := NewEngine(store, llm, NewBudget())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const page = "https://go.dev/blog/go1.24"
	var steps []string
	pr := Progress(func(step, detail string) { steps = append(steps, step+": "+detail) })

	ans, err := e.Run(ctx, "summary of this "+page, nil, pr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, s := range steps {
		t.Logf("  %s", s)
	}
	t.Logf("shape=%s skill=%s elapsed=%s support=%.0f%%", ans.Shape, ans.Skill, ans.Elapsed, ans.Support*100)
	t.Logf("\n%s", ans.Text)

	if ans.Skill != "page" {
		t.Errorf("skill is %q, want page", ans.Skill)
	}
	if ans.Shape != ShapeSummary {
		t.Errorf("shape is %s, want summary", ans.Shape)
	}
	if len(ans.Queries) != 0 {
		t.Errorf("it searched anyway: %v", ans.Queries)
	}
	if len(ans.Sources) != 1 || ans.Sources[0].URL != page {
		t.Errorf("sources are %+v, want only the page it was given", ans.Sources)
	}
	if ans.Text == "" {
		t.Fatal("empty answer")
	}
}

func TestSplitClaims(t *testing.T) {
	in := strings.Join([]string{
		"Here is the intro sentence about the thing [1].",
		"",
		"- **flour** [2]",
		"- **sugar** [2]",
		"- **eggs** [2]",
		"",
		"1. Mix the flour and sugar together in a bowl [3].",
		"2. Bake at 350F for twenty minutes [3].",
	}, "\n")

	got := splitClaims(in)
	// One prose sentence, one grouped bullet run, two separate numbered steps.
	if len(got) != 4 {
		t.Fatalf("want 4 claims, got %d: %q", len(got), got)
	}
	if !strings.Contains(got[1], "flour") || !strings.Contains(got[1], "eggs") {
		t.Errorf("bullets should group into one claim, got %q", got[1])
	}
	if strings.Contains(got[2], "Bake") {
		t.Errorf("numbered steps must not group, got %q", got[2])
	}
}

func TestCitedIDsAndStrip(t *testing.T) {
	s := "The thing is true [3] and also this [12][3]."
	ids := citedIDs(s)
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 12 {
		t.Errorf("citedIDs = %v, want [3 12]", ids)
	}
	if got := stripCitations(s); got != "The thing is true and also this ." {
		t.Errorf("stripCitations = %q", got)
	}
}

func TestLinkCitationsSkipsTags(t *testing.T) {
	in := `<a href="/x[9]">text [3]</a>`
	got := linkCitations(in)
	if strings.Contains(got, `/x<a class="cite"`) {
		t.Error("substituted inside a tag")
	}
	if !strings.Contains(got, `href="#p3"`) {
		t.Errorf("did not link the citation: %s", got)
	}
}

func TestAmbientFactsHasNoInstructions(t *testing.T) {
	f := AmbientFacts()
	if strings.Contains(f, "Never state it") {
		t.Error("instructions leaked into the facts shown to a person")
	}
	if !strings.Contains(f, "Today is") {
		t.Error("no date in the ambient facts")
	}
}

func TestBudgetWindow(t *testing.T) {
	b := NewBudget()
	if st := b.State(); st.Left != budgetMax || st.Cooling {
		t.Fatalf("fresh budget: %+v", st)
	}
	for i := 0; i < budgetMax; i++ {
		b.Spend()
	}
	st := b.State()
	if st.Left != 0 || st.Questions != 0 {
		t.Errorf("spent budget should be empty: %+v", st)
	}
	if st.ResetIn <= 0 {
		t.Error("a spent budget should say when room opens")
	}

	b2 := NewBudget()
	b2.Limited()
	cooling, left := b2.Cooling()
	if !cooling || left <= 0 {
		t.Error("a 202 should start a cooldown")
	}
	if st := b2.State(); !st.Cooling || st.Note == "" {
		t.Errorf("cooling state should explain itself: %+v", st)
	}
}

// TestDevBypassCannotShipEnabled is the guard on the auth bypass. The release
// image is built with -tags embed, where Reloaded is false, so this asserts the
// only thing that matters: the environment variable cannot turn it on there.
func TestDevBypassCannotShipEnabled(t *testing.T) {
	t.Setenv("SEARCH_DEV_NOAUTH", "1")
	if !Reloaded && devOpen() {
		t.Fatal("the auth bypass is reachable in an embed build")
	}
	if Reloaded && !devOpen() {
		t.Fatal("the bypass should work in a development build")
	}
}

// liveEngine is the real thing against the real model, with a throwaway store.
func liveEngine(t *testing.T) *Engine {
	t.Helper()
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to spend real searches")
	}
	url := os.Getenv("LLM_URL")
	if url == "" {
		url = "http://search-llm:8091"
	}
	llm := NewLLM(url)
	if !llm.Healthy(context.Background()) {
		t.Skip("model not up")
	}
	dir := os.Getenv("SEARCH_STORE")
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "search-e2e"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewEngine(store, llm, NewBudget())
}

// The shape decides the contract, so a coding question read as a how-to gets
// prose with a snippet in it rather than a file. This costs one model call per
// case and no searches.
func TestShapeEval(t *testing.T) {
	e := liveEngine(t)
	cases := []struct {
		q    string
		want Shape
	}{
		{"build me a dockerfile that will run ollama on a windows system running docker desktop", ShapeCode},
		{"give me a mini flask site that serves a cached frontend api for yahoo finance", ShapeCode},
		{"write a simple html page with a map of the world showing how hot it is in every country", ShapeCode},
		{"write me a python script i can add to my daily cron to do regular restic backups", ShapeCode},
		{"what is the best way to export a database to a csv file on an as/400", ShapeCode},
		{"regex to match an email address in javascript", ShapeCode},
		{"how do i set up wireguard on debian", ShapeHowTo},
		{"how do i make a breakfast burrito", ShapeRecipe},
		{"sqlite vs postgres for a small site", ShapeComparison},
		{"what is the status of the artemis program", ShapeStatus},
		{"what is sqlite fts5", ShapeFactual},
		{"when is the next liverpool game", ShapeUpcoming},
		{"when do the panthers play next", ShapeUpcoming},
		{"when is the next spacex launch", ShapeUpcoming},
		{"what are the upcoming premier league fixtures this weekend", ShapeUpcoming},
		{"when does the new elder scrolls come out", ShapeUpcoming},
		// The other half of the same fixture list, which has to stay news.
		{"did liverpool win their last match", ShapeNews},
	}
	only := os.Getenv("SEARCH_EVAL_ONLY")
	var wrong, ran int
	for _, c := range cases {
		if only != "" && !strings.Contains(c.q, only) {
			continue
		}
		ran++
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		got := e.plan(ctx, c.q, "")
		cancel()
		if got.Shape != c.want {
			wrong++
			t.Errorf("%-72q got %-10s want %s", c.q, got.Shape, c.want)
		}
	}
	t.Logf("%d of %d shaped correctly", ran-wrong, ran)
}

// TestCodeAnswerLive runs one real coding question end to end and prints what
// came back, which is the only way to judge whether the answer is pasteable.
// SEARCH_Q overrides the question while iterating.
func TestCodeAnswerLive(t *testing.T) {
	e := liveEngine(t)
	q := os.Getenv("SEARCH_Q")
	if q == "" {
		q = "write me a python script i can add to my daily cron to do regular restic backups"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	var steps []string
	ans, err := e.Run(ctx, q, nil, Progress(func(step, detail string) {
		steps = append(steps, step+": "+detail)
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, s := range steps {
		t.Logf("  %s", s)
	}
	t.Logf("shape=%s elapsed=%s support=%.0f%%", ans.Shape, ans.Elapsed, ans.Support*100)
	for _, c := range ans.Checks {
		t.Logf("  check %-24s %-10s %d lines  ok=%v  %s", c.File, c.Lang, c.Lines, c.OK, c.Note)
	}
	for _, d := range ans.Deps {
		t.Logf("  dep   %-30s %-8s checked=%v found=%v", d.Name, d.Eco, d.Checked, d.Found)
	}
	for _, w := range ans.Warnings {
		t.Logf("  warn  %s", w)
	}
	for _, s := range ans.Sources {
		t.Logf("  src   [%d] %s", s.N, s.URL)
	}
	t.Logf("\n%s", ans.Text)

	if ans.Shape != ShapeCode {
		t.Errorf("shape was %s, so the code contract never ran", ans.Shape)
	}
	if len(codeBlocks(ans.Text)) == 0 {
		t.Error("a code question came back with no code in it")
	}
	if strings.Contains(ans.HTML, "<pre") && !strings.Contains(ans.HTML, "<code") {
		t.Error("code did not render as a code block")
	}
}

// A follow-up names nothing a router can match. "odds on the match" carries no
// team, no competition and no date, so routing it before the rewrite meant a
// follow-up could never reach a skill, whatever it asked for.
func TestFollowupReachesASkill(t *testing.T) {
	e := liveEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	history := []Turn{{
		Question: "what is the s&p 500 at",
		Answer:   "The S&P 500 is at 6,502, up 0.4% on the day.",
	}}
	ans, err := e.Run(ctx, "what about the nasdaq", history, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("skill=%q standalone=%q", ans.Skill, ans.Standalone)
	t.Logf("\n%s", ans.Text)
	if ans.Skill != "markets" {
		t.Errorf("a follow-up asking for a quote answered from %q, not the markets skill", ans.Skill)
	}
	if !strings.Contains(strings.ToLower(ans.Standalone), "nasdaq") {
		t.Errorf("the router should have seen the resolved question, got %q", ans.Standalone)
	}
}

// The question that started this, end to end. It spends real searches, so it is
// behind SEARCH_LIVE like the rest, and what it checks is the one thing the
// support rate cannot see: whether the answer names a date that has not passed.
func TestUpcomingAnswerLive(t *testing.T) {
	e := liveEngine(t)
	q := os.Getenv("SEARCH_Q")
	if q == "" {
		q = "when is the next liverpool game"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	var steps []string
	ans, err := e.Run(ctx, q, nil, Progress(func(step, detail string) {
		steps = append(steps, step+": "+detail)
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, s := range steps {
		t.Logf("  %s", s)
	}
	t.Logf("shape=%s elapsed=%s support=%.0f%% retried=%v", ans.Shape, ans.Elapsed, ans.Support*100, ans.Retried)
	for _, w := range ans.Warnings {
		t.Logf("  warn  %s", w)
	}
	for _, c := range ans.Citations {
		t.Logf("  cite  checked=%v supported=%v [%d] %s", c.Checked, c.Supported, c.PassageID, truncate(c.Sentence, 90))
	}
	for _, s := range ans.Sources {
		t.Logf("  src   [%d] %s %s", s.N, s.Published, s.URL)
	}
	t.Logf("\n%s", ans.Text)

	if ans.Shape != ShapeUpcoming {
		t.Errorf("shape was %s, so the upcoming contract never ran", ans.Shape)
	}
	if noFutureDate(ans.Text, localNow()) {
		t.Error("the answer names no date today or later, which is the whole question")
	}
}
