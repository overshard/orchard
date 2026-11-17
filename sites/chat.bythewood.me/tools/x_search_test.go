package tools

import (
	"context"
	"strings"
	"testing"
)

type fakeSearch struct {
	hits    []SearchHit
	domains []string
}

func (f *fakeSearch) Search(_ context.Context, _ string, domains []string, _ int) ([]SearchHit, error) {
	f.domains = domains
	return f.hits, nil
}

// xcancel and Nitter are both closed, and the paid API needs a key, so a site
// search is the way in. It has to keep the posts and drop everything else a
// site search also returns.
func TestXSearchKeepsPostsAndNormalisesTheHost(t *testing.T) {
	f := &fakeSearch{hits: []SearchHit{
		{Title: "A post about Go", URL: "https://x.com/someone/status/1234"},
		{Title: "An older post", URL: "https://twitter.com/someone/status/5678"},
		{Title: "The profile page", URL: "https://x.com/someone"},
		{Title: "Someone's blog", URL: "https://example.com/blog/x-thoughts"},
	}}
	d := &Turn{Deps: &Deps{Search: f}}
	out, err := XSearch.Run(context.Background(), d, map[string]any{"query": "go"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.domains, ",") != "x.com,twitter.com" {
		t.Errorf("the search was not scoped to x.com: %v", f.domains)
	}
	posts := out.(map[string]any)["posts"].([]SearchHit)
	if len(posts) != 2 {
		t.Fatalf("kept %d posts, want the two status pages: %#v", len(posts), posts)
	}
	// One hostname in one answer, since twitter.com only redirects.
	for _, p := range posts {
		if !strings.HasPrefix(p.URL, "https://x.com/") {
			t.Errorf("a link was left on the old hostname: %s", p.URL)
		}
	}
}

func TestIsXPost(t *testing.T) {
	yes := []string{
		"https://x.com/a/status/1",
		"https://twitter.com/a/status/1",
		"https://www.x.com/a/status/1?s=20",
	}
	for _, u := range yes {
		if !isXPost(u) {
			t.Errorf("%s was not taken as a post", u)
		}
	}
	no := []string{
		"https://x.com/someone",
		"https://x.com/hashtag/go",
		"https://help.x.com/en/using-x",
		"https://example.com/a/status/1",
	}
	for _, u := range no {
		if isXPost(u) {
			t.Errorf("%s was taken as a post", u)
		}
	}
}
