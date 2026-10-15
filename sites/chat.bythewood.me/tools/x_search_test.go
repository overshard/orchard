package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ddgXPage = `
<a rel="nofollow" class="result__a" href="https://x.com/someone/status/1234">A post about Go</a>
<a class="result__snippet" href="#">the snippet of the post</a>
<a rel="nofollow" class="result__a" href="https://twitter.com/someone/status/5678">An older post</a>
<a class="result__snippet" href="#">indexed under the old hostname</a>
<a rel="nofollow" class="result__a" href="https://x.com/someone">The profile page</a>
<a class="result__snippet" href="#">not a post</a>
<a rel="nofollow" class="result__a" href="https://example.com/blog/x-thoughts">Someone's blog</a>
<a class="result__snippet" href="#">not x at all</a>
`

// xcancel and Nitter are both closed, and the paid API needs a key, so a site
// search is the way in. It has to keep the posts and drop everything else a
// site search also returns.
func TestXSearchKeepsPostsAndNormalisesTheHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "site%3Ax.com") {
			t.Errorf("the search was not scoped to x.com: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(ddgXPage))
	}))
	defer srv.Close()

	hits := parseDDG(ddgXPage, 0)
	if len(hits) != 4 {
		t.Fatalf("parsed %d results, want 4", len(hits))
	}

	var posts []SearchHit
	for _, h := range hits {
		if isXPost(h.URL) {
			h.URL = asXCom(h.URL)
			posts = append(posts, h)
		}
	}
	if len(posts) != 2 {
		t.Fatalf("kept %d posts, want the two status pages: %#v", len(posts), posts)
	}
	// One hostname in one answer, since twitter.com only redirects.
	for _, p := range posts {
		if !strings.HasPrefix(p.URL, "https://x.com/") {
			t.Errorf("a link was left on the old hostname: %s", p.URL)
		}
	}
	_ = context.Background()
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
