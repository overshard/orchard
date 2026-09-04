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
