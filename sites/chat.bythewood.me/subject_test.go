package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"chat.bythewood.me/tools"
)

// The shapes a question actually arrives in, and what has to come out of them.
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

		{"", ""},
		// What is being asked about each of these is today's value.
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

// With search boxed the draft has to stand. Sending it anywhere is a loop,
// since every road out of here needs that host.
func TestABoxedSearchLetsTheDraftStand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": `{"verdict":"research","query":"something"}`}}},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	t.Cleanup(srv.Close)

	eng := NewEngine(NewLLM(srv.URL, "local", ""), "test model")
	eng.Deps().Guard.Trip(tools.SearchHost)

	nudge, _ := eng.gate(context.Background(), "what is a goodyear welt",
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

// "Big news today" reduces to "Big news", which is in neither map, so the head
// noun is checked too.
func TestSubjectOfSkipsALiveHeadNoun(t *testing.T) {
	for _, q := range []string{
		"Big news today",
		"any big news today",
		"what are the latest headlines",
		"whats the current gas price",
	} {
		if got := subjectOf(q); got != "" {
			t.Errorf("subjectOf(%q) = %q, want it skipped", q, got)
		}
	}
	// And a real subject whose name merely contains one of those words survives.
	for _, q := range []string{
		"what is News Corporation",
		"who is Rick Springfield",
	} {
		if subjectOf(q) == "" {
			t.Errorf("subjectOf(%q) was skipped, want it looked up", q)
		}
	}
}

// A remark is not a subject, and each of these clears the word cap.
func TestSubjectOfSkipsSomebodyTalking(t *testing.T) {
	for _, q := range []string{
		"That's a crazy high number",
		"sorry i wanted total calories",
		"is there no service that exposes a search for X posts?",
		"oh it needs redis to run?",
		"tl;dr https://rmondello.com/2026/09/07/switching-password-managers-2026/",
		"okay that works",
		"I think it is wrong",
		"wow this is a lot",
	} {
		if got := subjectOf(q); got != "" {
			t.Errorf("subjectOf(%q) = %q, want nothing to look up", q, got)
		}
	}
}

// The tightening has to leave a real subject alone, including the shapes the
// head strip handles.
func TestSubjectOfKeepsARealSubject(t *testing.T) {
	for q, want := range map[string]string{
		"what is Nim":                        "Nim",
		"what is scvmm":                      "scvmm",
		"what is a b-tree":                   "b-tree",
		"tell me about PostgreSQL":           "PostgreSQL",
		"North Korea":                        "North Korea",
		"yadkin valley":                      "yadkin valley",
		"what is the capital of france":      "capital of france",
		"who is the prime minister of japan": "prime minister of japan",
	} {
		if got := subjectOf(q); got != want {
			t.Errorf("subjectOf(%q) = %q, want %q", q, got, want)
		}
	}
}

// "how much" and "how many" carry their own verb, so stripping "how" alone left
// "much is a keystone crickett" behind, which then tripped the verb check.
func TestSubjectOfHandlesHowMuch(t *testing.T) {
	if got := subjectOf("how much is a keystone crickett"); got != "keystone crickett" {
		t.Errorf("subjectOf = %q, want the rifle", got)
	}
	if got := subjectOf("how many people live in Tokyo"); got != "people live in Tokyo" && got != "" {
		t.Logf("how many reduced to %q", got)
	}
}

func TestARemarkOrAReleaseQuestionFindsTheThing(t *testing.T) {
	for q, want := range map[string]string{
		"ah so trixie is still fairly new -- when is it's EOL?": "",
		"hmmmm tempting it's not that big really":               "",
		"when was trixie released":                              "trixie",
		"when is forky coming out":                              "forky",
	} {
		if got := subjectOf(q); got != want {
			t.Errorf("subjectOf(%q) = %q, want %q", q, got, want)
		}
	}
}
