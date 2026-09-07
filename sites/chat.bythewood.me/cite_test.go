package main

import (
	"strings"
	"testing"

	"chat.bythewood.me/tools"
)

func testSources() []Source {
	return collectSources([]tools.Result{
		{Name: "web_search", Content: map[string]any{"results": []tools.SearchHit{
			{Title: "Portsmouth arrival", URL: "https://www.bbc.co.uk/news/articles/x", Snippet: "A boat carrying 140 people landed at Portsmouth"},
			{Title: "Crossings fall", URL: "https://example.org/stats?utm_source=ddg", Snippet: "crossings fell 43% over the year"},
		}}},
		{Name: "web_fetch", Content: map[string]any{
			"url":  "https://www.bbc.co.uk/news/articles/x#top",
			"text": "Border Force intercepted the dinghy off Portsmouth on Saturday and brought 140 people ashore. Hampshire Police opened an investigation into assaults on officers.",
		}},
		{Name: "weather", Content: map[string]any{"temp": 61}},
		{Name: "web_fetch", Err: "404", Content: map[string]any{"url": "https://example.com/gone"}},
	})
}

func TestCollectSources(t *testing.T) {
	srcs := testSources()
	if len(srcs) != 2 {
		t.Fatalf("want 2 sources, got %d: %+v", len(srcs), srcs)
	}
	// The fetched page is first because it is what the model actually read,
	// and the fragment on it does not make it a second source.
	if srcs[0].N != 1 || srcs[0].Site != "bbc.co.uk" {
		t.Errorf("the fetched page should be [1], got %+v", srcs[0])
	}
	if !strings.Contains(srcs[0].Text, "Hampshire") {
		t.Error("the fetched page kept the snippet instead of the page text")
	}
	if srcs[0].Title != "Portsmouth arrival" {
		t.Errorf("the search hit's title was lost: %q", srcs[0].Title)
	}
	// A tracking parameter is not part of the address.
	if strings.Contains(srcs[1].URL, "utm_source") {
		t.Errorf("utm parameter survived: %s", srcs[1].URL)
	}
	// A tool with no page behind it and a failed fetch are not sources.
	for _, s := range srcs {
		if strings.Contains(s.URL, "gone") {
			t.Error("a failed fetch was offered as a source")
		}
	}
}

func TestAttachKeepsAndRepairs(t *testing.T) {
	srcs := testSources()

	// A number the model wrote that names a real source is kept, and one that
	// names nothing is dropped rather than shown as text.
	got := attach("Border Force brought 140 people ashore at Portsmouth [1]. The rest is guesswork [9].", srcs)
	if !strings.Contains(got, "Portsmouth.[1]") {
		t.Errorf("the model's own citation was not kept: %q", got)
	}
	if strings.Contains(got, "[9]") {
		t.Errorf("a citation with no source behind it survived: %q", got)
	}

	// A sentence carrying a source's own figures and names gets one even
	// though the model wrote none.
	got = attach("Hampshire Police opened an investigation into assaults on officers.", srcs)
	if !strings.Contains(got, "[1]") {
		t.Errorf("a sentence lifted from the page was left uncited: %q", got)
	}

	// A sentence about nothing in particular gets nothing, since a pill on an
	// unsupported line reads as a check that passed.
	got = attach("That is worth thinking about before you decide anything.", srcs)
	if strings.Contains(got, "[") {
		t.Errorf("an unsupported sentence was given a source: %q", got)
	}
}

func TestAttachLeavesCodeAlone(t *testing.T) {
	srcs := testSources()
	src := "Read it like this:\n\n```go\nfmt.Println(rows[1])\n```\n\nThen use `cols[1]` after."
	got := attach(src, srcs)
	if !strings.Contains(got, "rows[1])") {
		t.Errorf("an index inside a fence was rewritten: %q", got)
	}
	if !strings.Contains(got, "`cols[1]`") {
		t.Errorf("an index inside a code span was rewritten: %q", got)
	}
}

func TestAttachMovesTheMarkerToTheEnd(t *testing.T) {
	srcs := testSources()
	// The model puts it after the full stop about as often as before it, and a
	// pill has to land in the same place either way.
	for _, in := range []string{
		"Border Force brought 140 people ashore at Portsmouth. [1] It was Saturday.",
		"Border Force brought 140 people ashore at Portsmouth [1]. It was Saturday.",
	} {
		got := attach(in, srcs)
		if !strings.Contains(got, "Portsmouth.[1]") {
			t.Errorf("marker not normalised for %q, got %q", in, got)
		}
		if strings.Count(got, "[1]") != 1 {
			t.Errorf("the marker was duplicated: %q", got)
		}
	}
}

func TestDropSourceList(t *testing.T) {
	// What the model wrote under an answer before it had numbers, and every
	// line of it was dead text rather than a link.
	answer := "The crossings are down 43% on last year.\n\nSo the framing does not hold.\n\nbbc.co.uk/news/articles/crl60z17lyko\nindependent.co.uk/news/uk/home-news/portsmouth-b3045873.html"
	got := dropSourceList(answer)
	if strings.Contains(got, "bbc.co.uk") || strings.Contains(got, "independent") {
		t.Errorf("the address dump survived: %q", got)
	}
	if !strings.Contains(got, "the framing does not hold") {
		t.Errorf("the answer above it was eaten: %q", got)
	}

	// The heading form, and the same thing written on one line.
	if got := dropSourceList("It is down.\n\nSources: https://a.org/x and https://b.org/y"); strings.Contains(got, "a.org") {
		t.Errorf("a Sources: line survived: %q", got)
	}

	// A bulleted list of links in the middle of an answer is the answer, and
	// only a Sources heading turns one at the end into a dump.
	links := "Three places sell it:\n\n- example.org/parts/a\n- example.org/parts/b"
	if got := dropSourceList(links); got != links {
		t.Errorf("a list the reader asked for was dropped: %q", got)
	}
	withHeading := links + "\n\nSources\n\n- example.org/parts/a"
	if got := dropSourceList(withHeading); !strings.HasSuffix(got, "parts/b") {
		t.Errorf("a headed dump was not trimmed back to the answer: %q", got)
	}

	// An answer ending on a real sentence is untouched, including one that
	// mentions a site by name.
	for _, keep := range []string{
		"It is down 43% and nobody expected that.",
		"Check the Home Office quarterly table, which is the number that settles it.",
	} {
		if got := dropSourceList(keep); got != keep {
			t.Errorf("prose was trimmed: %q became %q", keep, got)
		}
	}
}

func TestLinkBareAddresses(t *testing.T) {
	got := linkBareAddresses("It is on bbc.co.uk/news/articles/x now.")
	if !strings.Contains(got, "https://bbc.co.uk/news/articles/x") {
		t.Errorf("a schemeless address was left dead: %q", got)
	}
	// Already a link, code, and a version number are all left alone.
	for _, keep := range []string{
		"See https://bbc.co.uk/news/articles/x for it.",
		"Run `go/bin/thing` first.",
		"The ratio is 1.2/3 either way.",
	} {
		if got := linkBareAddresses(keep); got != keep {
			t.Errorf("%q was rewritten to %q", keep, got)
		}
	}
}

func TestLinkCitations(t *testing.T) {
	srcs := testSources()
	got := linkCitations("<p>Ashore at Portsmouth[1].</p>", srcs)
	if !strings.Contains(got, `href="https://www.bbc.co.uk/news/articles/x"`) {
		t.Errorf("the pill did not link to the source: %q", got)
	}
	// A number with no source behind it stays as text rather than becoming a
	// link to nothing.
	if got := linkCitations("<p>Something[7].</p>", srcs); strings.Contains(got, "<a") {
		t.Errorf("an unknown number was linked: %q", got)
	}
	// Code is left alone, since an index is not a citation.
	if got := linkCitations("<pre><code>rows[1]</code></pre>", srcs); strings.Contains(got, "<a") {
		t.Errorf("an index inside code was linked: %q", got)
	}
	// A digit in an attribute is not a citation either.
	if got := linkCitations(`<p data-x="[1]">hi</p>`, srcs); strings.Contains(got, "<a") {
		t.Errorf("an attribute was rewritten: %q", got)
	}
}

func TestCited(t *testing.T) {
	srcs := testSources()
	out := cited("One thing[2]. Another thing.", srcs)
	if len(out) != 1 || out[0].N != 2 {
		t.Errorf("want only source 2, got %+v", out)
	}
	if len(cited("Nothing cited here.", srcs)) != 0 {
		t.Error("an answer citing nothing listed sources anyway")
	}
}

func TestSentences(t *testing.T) {
	got := sentences("It landed at 4am. Police opened a case. Nobody was charged.")
	if len(got) != 3 {
		t.Fatalf("want 3 sentences, got %d: %q", len(got), got)
	}
	// A decimal and an abbreviation are not sentence ends, and a code span
	// carrying a full stop is stepped over.
	if got := sentences("The figure is 43.5% of the total."); len(got) != 1 {
		t.Errorf("a decimal split a sentence: %q", got)
	}
	if got := sentences("Call `fmt.Println` for it."); len(got) != 1 {
		t.Errorf("a code span split a sentence: %q", got)
	}
}
