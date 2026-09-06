package main

import (
	"strings"
	"testing"
)

// The rewrite is a model call and an address is the one thing in a question
// that cannot survive being paraphrased.
func TestKeepURLs(t *testing.T) {
	const q = "and this one https://example.com/post"
	cases := []struct{ standalone, want string }{
		{"What does the post say?", "What does the post say? https://example.com/post"},
		{"What does https://example.com/post say?", "What does https://example.com/post say?"},
	}
	for _, c := range cases {
		if got := keepURLs(q, c.standalone); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if got := keepURLs("who won the game", "who won the game"); got != "who won the game" {
		t.Errorf("added something to a question with no address in it: %q", got)
	}
}

// The planner picks a shape from a reading of the question, and a summary is
// not one of those: it happens because the question carried the address of the
// page to read, which is known before any planning.
func TestSummaryIsNotPlannable(t *testing.T) {
	for _, n := range shapeNames() {
		if n == string(ShapeSummary) {
			t.Error("the planner is offered summary, which it cannot know to pick")
		}
	}
	c := contractFor(ShapeSummary)
	if c.Shape != ShapeSummary {
		t.Fatalf("summary falls back to %s", c.Shape)
	}
	// The one rule this shape exists for.
	if !strings.Contains(c.Instruction, "nothing you know about the subject from elsewhere") {
		t.Error("the summary contract does not hold the answer to the page it was given")
	}
}

func TestListHosts(t *testing.T) {
	got := listHosts([]string{"https://cloudinabottle.org/blog/launch-post", "http://go.dev/blog/go1.24"})
	if want := "cloudinabottle.org, go.dev"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
