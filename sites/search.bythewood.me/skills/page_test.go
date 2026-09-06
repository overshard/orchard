package skills

import (
	"context"
	"strings"
	"testing"
)

func TestURLsIn(t *testing.T) {
	cases := []struct {
		q    string
		want []string
	}{
		{"summary of this https://cloudinabottle.org/blog/launch-post",
			[]string{"https://cloudinabottle.org/blog/launch-post"}},
		{"what does https://example.com/a say, and is it right?",
			[]string{"https://example.com/a"}},
		{"read (https://example.com/a) and http://example.org/b",
			[]string{"https://example.com/a", "http://example.org/b"}},
		{"the same one twice https://example.com/a https://example.com/a",
			[]string{"https://example.com/a"}},
		{"how many people live in http land", nil},
		{"what is https://localhost about", nil},
		{"a query string survives https://example.com/a?b=c&d=e#f",
			[]string{"https://example.com/a?b=c&d=e#f"}},
		{"summarise https://en.wikipedia.org/wiki/Go_(programming_language)",
			[]string{"https://en.wikipedia.org/wiki/Go_(programming_language)"}},
		{"summarise (https://en.wikipedia.org/wiki/Go_(programming_language)).",
			[]string{"https://en.wikipedia.org/wiki/Go_(programming_language)"}},
	}
	for _, c := range cases {
		got := URLsIn(c.q)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q: got %v, want %v", c.q, got, c.want)
		}
	}
}

// The claim is the whole routing decision for this one, so it has to hold
// without a model. Decide is handed a nil model on purpose: reaching for it
// would panic, which is the assertion.
func TestPageClaimsWithoutTheModel(t *testing.T) {
	r := Default()
	route := r.Decide(context.Background(), nil, "summary of this https://cloudinabottle.org/blog/launch-post")
	if route.Skill != "page" {
		t.Fatalf("got %s, want page (%s)", route.Skill, route.Why())
	}
	if got := r.match("tl;dr https://example.com/post"); got != "page" {
		t.Errorf("offline matcher got %s, want page", got)
	}
}

// A claiming skill is kept out of the prompt the model reads, since it can
// never be the answer to a question with no address in it.
func TestPageIsNotInTheRoutingPrompt(t *testing.T) {
	r := Default()
	if strings.Contains(r.systemPrompt(), "page:") {
		t.Error("the page card is in the routing prompt, which is noise on every other question")
	}
	for _, n := range r.routable() {
		if n == "page" {
			t.Error("page is in the enum the model chooses from")
		}
	}
}

func TestPageRun(t *testing.T) {
	res, err := (Page{}).Run(context.Background(), "summary of this https://example.com/post", Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.URLs) != 1 || res.URLs[0] != "https://example.com/post" {
		t.Fatalf("got %+v", res)
	}
	if res.Text != "" {
		t.Error("it gathers rather than answers, so it must not write text")
	}
	if res.Shape != "summary" {
		t.Errorf("shape is %q, want summary", res.Shape)
	}

	// The router can only send it a question with an address in it, but a
	// skill that trusts the router has no way of declining.
	if res, _ := (Page{}).Run(context.Background(), "what is a summary", Deps{}); res != nil {
		t.Error("claimed a question with no address in it")
	}
}

func TestWantsProbability(t *testing.T) {
	for _, q := range []string{
		"what are the odds on the us open",
		"who is favoured to win the election",
		"chances of a rate cut this month",
		"is trump going to win",
	} {
		if !wantsProbability(q) {
			t.Errorf("%q should read as a question about how likely something is", q)
		}
	}
	for _, q := range []string{
		"how is the us open going",
		"what's the score in the us open",
		"who won the us open",
		"when is the election",
	} {
		if wantsProbability(q) {
			t.Errorf("%q is not asking for a price", q)
		}
	}
}
