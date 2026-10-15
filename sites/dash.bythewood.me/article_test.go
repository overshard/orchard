package main

import (
	"context"
	"strings"
	"testing"
)

func TestArticleTextKeepsTheArticle(t *testing.T) {
	page := `<html><head><meta property="og:description" content="Short blurb."><script>var p = "<p>not this</p>";</script></head>
<body><nav><p>Home News Sport Weather and every other section of the site</p></nav>
<article><h1>Title</h1><p>By Jane Doe</p>
<p>The city council voted on Tuesday to close the old bridge for repairs that are expected to take two years.</p>
<figure><p>A caption that runs long enough to look like a paragraph to a careless reader.</p></figure>
<p>Residents said the detour adds &quot;twenty minutes&quot; to a commute, and the mayor promised a <a href="/x">shuttle</a> service during the work.</p>
<p>The vote passed seven to two, and the work starts in March after a final review of the contractor's bid and the state's share of the cost.</p>
<p>Officials said a second bridge further north would stay open throughout, though it carries a lower weight limit for trucks.</p></article>
<footer><p>Copyright and terms and privacy and all the rest of the footer text</p></footer></body></html>`
	got := articleText(page)
	for _, want := range []string{"voted on Tuesday", `"twenty minutes"`, "shuttle service"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, bad := range []string{"not this", "Home News", "caption", "By Jane Doe", "Copyright"} {
		if strings.Contains(got, bad) {
			t.Errorf("kept %q in %q", bad, got)
		}
	}
}

func TestArticleTextFallsBackToTheDescription(t *testing.T) {
	page := `<meta name="description" content="A paywalled story about the bridge vote and the detour it brings for commuters.">
<p>Subscribe to keep reading this story and every other one.</p>`
	if got := articleText(page); got != "A paywalled story about the bridge vote and the detour it brings for commuters." {
		t.Errorf("%q", got)
	}
	if got := articleText(`<p>Sign in.</p>`); got != "" {
		t.Errorf("%q", got)
	}
}

func TestSkipArticle(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://news.ycombinator.com/item?id=1":      true,
		"https://news.google.com/rss/articles/abc":    true,
		"https://example.com/paper.PDF":               true,
		"ftp://example.com/x":                         true,
		"https://www.example.com/2026/10/bridge-vote": false,
	} {
		if skipArticle(raw) != want {
			t.Errorf("%s: want %v", raw, want)
		}
	}
}

// A point whose outlets can't be fetched still gets a TL;DR off their feed
// descriptions, and one with nothing but headlines gets none.
func TestEventTLDRUsesDescriptions(t *testing.T) {
	m := &scriptedModel{answers: []string{`{"tldr": "The council voted to close the bridge for two years."}`}}
	b := &Briefer{model: m, guard: NewGuard(t.TempDir())}
	events := []cluster{{stories: []story{
		{source: "AP", title: "Council closes bridge", url: "https://news.google.com/rss/articles/a", desc: "The council voted 7-2."},
		{source: "NPR", title: "Bridge to close", url: "https://news.google.com/rss/articles/b"},
	}}}
	links := []Link{{Source: "AP", URL: "https://news.google.com/rss/articles/a", Title: "Council closes bridge"}, {Source: "NPR", URL: "https://news.google.com/rss/articles/b", Title: "Bridge to close"}}
	if got := b.eventTLDR(context.Background(), events, []int{1}, links); got != "The council voted to close the bridge for two years." {
		t.Errorf("%q", got)
	}
	if !strings.Contains(m.asks[0], "[AP] Council closes bridge\nSummary: The council voted 7-2.") || !strings.Contains(m.asks[0], "[NPR] Bridge to close") {
		t.Errorf("ask: %s", m.asks[0])
	}
	events[0].stories[0].desc = ""
	if got := b.eventTLDR(context.Background(), events, []int{1}, links); got != "" || len(m.asks) != 1 {
		t.Errorf("headlines alone gave %q", got)
	}
}
