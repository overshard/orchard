package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat.bythewood.me/tools"
)

// The shapes a question actually arrives in, and what has to come out of them
// for the snapshot to be asked about the right thing.
func TestSubjectOf(t *testing.T) {
	cases := []struct{ question, want string }{
		{"what is a goodyear welt", "goodyear welt"},
		{"what is postgresql", "postgresql"},
		{"what is kubernetes for", "kubernetes"},
		{"tell me about photosynthesis", "photosynthesis"},
		{"who is kim jong un", "kim jong un"},
		{"what's a b-tree", "b-tree"},
		{"what is a b-tree and why do databases use them", "b-tree"},
		{"explain the calvin cycle", "calvin cycle"},
		{"where is yadkin valley", "yadkin valley"},
		{"define entropy", "entropy"},

		// Nothing worth looking up, so the gate is left as it was.
		{"", ""},
		// The snapshot has an article on each of these and none of them answers
		// what is being asked, which is today's value.
		{"what's the weather like", ""},
		{"what is the weather", ""},
		{"what time is it", ""},
		{"what's the price", ""},
		{"why", ""},
		{"can you write me a bash script that renames every file in a directory", ""},
		{"what do you think about the way i structured the makefile in that repo", ""},
	}
	for _, c := range cases {
		if got := subjectOf(c.question); got != c.want {
			t.Errorf("subjectOf(%q) = %q, want %q", c.question, got, c.want)
		}
	}
}

// fakeWiki stands in for kiwix so the engine can be driven without one.
func fakeWiki(t *testing.T, title, lead string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/content/wikipedia/", func(w http.ResponseWriter, r *http.Request) {
		want := "/content/wikipedia/" + strings.ReplaceAll(title, " ", "_")
		if r.URL.Path != want {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "<p>%s</p>", lead)
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
	})
	mux.HandleFunc("/catalog/v2/entries", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<feed><entry><updated>2026-06-11T00:00:00Z</updated></entry></feed>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func groundEngine(t *testing.T, base string) *Engine {
	t.Helper()
	tools.WikiBase = base
	t.Cleanup(func() { tools.WikiBase = "http://orchard-wiki:8000" })
	return NewEngine(NewLLM("http://127.0.0.1:1", "local", ""), "test model")
}

// The case this was written for: the model answered from memory, so the gate
// has no tool result to check the draft against, and the snapshot has one.
func TestBackgroundIsFoundForAnAnsweredFromMemoryQuestion(t *testing.T) {
	eng := groundEngine(t, fakeWiki(t, "Goodyear welt",
		"A Goodyear welt is a strip of leather. The machine was invented by Auguste Destouy and improved for Charles Goodyear Jr."))

	got := eng.background(context.Background(), "what is a goodyear welt")
	if !strings.Contains(got, "Charles Goodyear Jr") {
		t.Errorf("background did not carry the article: %q", got)
	}
	if !strings.Contains(got, "looked up here rather than by the model") {
		t.Errorf("background does not say where it came from: %q", got)
	}
	// Without the age the gate cannot tell a stale article from a wrong draft.
	if !strings.Contains(got, "June 2026") {
		t.Errorf("background does not carry the snapshot date: %q", got)
	}
}

// The gate is told how old the background is, so a draft that is right about
// something recent is not sent back to be corrected against an older article.
func TestTheGatePromptWeighsTheSnapshotAge(t *testing.T) {
	if !strings.Contains(gateSystem, "background is old rather than the draft wrong") {
		t.Error("the gate prompt does not tell it a stale background is not a contradiction")
	}
	if !strings.Contains(gateSystem, "on the date it states") {
		t.Error("the gate prompt does not say the background carries its own date")
	}
}

// A question with no article behind it must add nothing, since an empty
// background has to leave the gate exactly as it was.
func TestBackgroundIsEmptyWhenTheSnapshotHasNothing(t *testing.T) {
	eng := groundEngine(t, fakeWiki(t, "Something Else", "Unrelated."))

	if got := eng.background(context.Background(), "what is a zzzznotathingxyz"); got != "" {
		t.Errorf("background = %q, want empty", got)
	}
}

// A question that is not about a lookupable thing must not cost a call at all.
func TestBackgroundSkipsAQuestionWithNoSubject(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	eng := groundEngine(t, srv.URL)

	got := eng.background(context.Background(), "can you write me a bash script that renames every file in a directory")
	if got != "" {
		t.Errorf("background = %q, want empty", got)
	}
	if calls != 0 {
		t.Errorf("the snapshot was asked %d times for a question with no subject", calls)
	}
}

// A snapshot that is down must not change a verdict, since this only ever adds
// evidence and the turn has to survive without it.
func TestBackgroundSurvivesTheSnapshotBeingDown(t *testing.T) {
	eng := groundEngine(t, "http://127.0.0.1:1")

	if got := eng.background(context.Background(), "what is postgresql"); got != "" {
		t.Errorf("background = %q, want empty when the snapshot is unreachable", got)
	}
}

// The nudge has to name the tool and the subject, or a model sent back goes and
// searches the web for what is already on this machine.
func TestWikiNudgeNamesTheToolAndTheSubject(t *testing.T) {
	n := wikiNudge("goodyear welt")
	for _, want := range []string{"wikipedia", "goodyear welt", "correct"} {
		if !strings.Contains(n, want) {
			t.Errorf("nudge does not mention %q: %s", want, n)
		}
	}
	if strings.Contains(strings.ToLower(n), "search the web") {
		t.Errorf("nudge sends it to the web: %s", n)
	}
}

// The gate is handed the background as its own block, so "no tool was called"
// stays true and the model is not told something was fetched when nothing was.
func TestBackgroundDoesNotMasqueradeAsAToolResult(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": `{"verdict":"answered","query":""}`}}},
		})
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		sent = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	eng := NewEngine(NewLLM(srv.URL, "local", ""), "test model")
	eng.enough(context.Background(), "what is a goodyear welt", "A draft.",
		"wikipedia on Goodyear welt, from an offline snapshot taken June 2026, looked up here rather than by the model: a strip of leather", nil, nil)

	if !strings.Contains(sent, "no tool was called") {
		t.Errorf("the gate was not told that no tool ran: %s", trimLine(sent, 400))
	}
	if !strings.Contains(sent, "Background, looked up locally") {
		t.Errorf("the background was not sent as its own block: %s", trimLine(sent, 400))
	}
}

// A gate that answers "research" whatever it is asked, so the path through it
// is what the test is measuring rather than the model's judgment.
func fakeGate(t *testing.T, verdict string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": `{"verdict":"` + verdict + `","query":"something"}`}}},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The whole reason for a local corpus is that it works while a search host is
// refusing us, so the check it makes possible has to work then too.
func TestTheLocalCheckStillRunsWhenSearchIsBoxed(t *testing.T) {
	tools.WikiBase = fakeWiki(t, "Goodyear welt", "A Goodyear welt is a strip of leather.")
	t.Cleanup(func() { tools.WikiBase = "http://orchard-wiki:8000" })

	eng := NewEngine(NewLLM(fakeGate(t, "research"), "local", ""), "test model")
	eng.Deps().Guard.Trip(tools.SearchHost)
	if _, down := eng.SearchDown(); !down {
		t.Fatal("the search host was not boxed, so this proves nothing")
	}

	nudge, _ := eng.gate(context.Background(), "what is a goodyear welt",
		"A goodyear welt is made by the Goodyear tyre company.", nil, nil, func(Event) {})
	if nudge == "" {
		t.Fatal("a draft the snapshot disagrees with was let through while search was boxed")
	}
	if !strings.Contains(nudge, "wikipedia") {
		t.Errorf("the nudge does not name the local tool: %s", nudge)
	}
	if strings.Contains(strings.ToLower(nudge), "search for") {
		t.Errorf("the nudge sends it to a search that is refusing us: %s", nudge)
	}
}

// With search boxed and nothing local to check against, the draft has to stand.
// Sending it anywhere is a loop, since every road out of here needs that host.
func TestABoxedSearchWithNoBackgroundLetsTheDraftStand(t *testing.T) {
	tools.WikiBase = fakeWiki(t, "Something Else", "Unrelated.")
	t.Cleanup(func() { tools.WikiBase = "http://orchard-wiki:8000" })

	eng := NewEngine(NewLLM(fakeGate(t, "research"), "local", ""), "test model")
	eng.Deps().Guard.Trip(tools.SearchHost)

	nudge, _ := eng.gate(context.Background(), "what is a zzzznotathingxyz",
		"I think it is a kind of bird.", nil, nil, func(Event) {})
	if nudge != "" {
		t.Errorf("the turn was sent back with nowhere to go: %s", nudge)
	}
}

// The chip has to say how old a snapshot answer is, since it otherwise looks
// exactly like one read off the live web.
func TestSnapshotAgeReachesTheToolSummary(t *testing.T) {
	got := snapshotAge(map[string]any{"found": true, "snapshot_date": "June 2026"})
	if got != "June 2026" {
		t.Errorf("snapshotAge = %q, want %q", got, "June 2026")
	}
	// A tool that reads the live thing has no age and must not grow one.
	if got := snapshotAge(map[string]any{"temperature": 71}); got != "" {
		t.Errorf("snapshotAge = %q, want empty for a live tool", got)
	}
	if got := snapshotAge("not a map"); got != "" {
		t.Errorf("snapshotAge = %q, want empty", got)
	}
}
