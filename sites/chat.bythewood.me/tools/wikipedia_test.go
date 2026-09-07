package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeKiwix answers the two endpoints the tool uses. articles is keyed by the
// title as it appears in a path.
func fakeKiwix(t *testing.T, articles map[string]string, hits []string) (*httptest.Server, *int) {
	t.Helper()
	searches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		searches++
		var items strings.Builder
		for _, h := range hits {
			fmt.Fprintf(&items, "<item><title>%s</title><link>/content/wikipedia/%s</link></item>",
				h, strings.ReplaceAll(h, " ", "_"))
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><rss><channel>%s</channel></rss>`, items.String())
	})
	mux.HandleFunc("/content/wikipedia/", func(w http.ResponseWriter, r *http.Request) {
		title := strings.TrimPrefix(r.URL.Path, "/content/wikipedia/")
		body, ok := articles[title]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("/catalog/v2/entries", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<feed><entry><updated>2026-06-29T00:00:00Z</updated></entry></feed>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &searches
}

func wikiDeps() *Deps {
	return &Deps{HTTP: &http.Client{Timeout: 5 * time.Second}, Now: time.Now, Guard: NewGuard(time.Minute)}
}

func lookup(t *testing.T, base, q string) map[string]any {
	t.Helper()
	out, err := wikiLookup(context.Background(), wikiDeps(), base, q)
	if err != nil {
		t.Fatalf("lookup %q: %v", q, err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("lookup %q returned %T", q, out)
	}
	return m
}

// A one word lookup names an article, and going through the ranker instead can
// return a page that merely mentions the word.
func TestExactTitleBeatsTheRanker(t *testing.T) {
	srv, searches := fakeKiwix(t,
		map[string]string{"PostgreSQL": "<p>PostgreSQL is a database.</p>"},
		[]string{"Misskey"})

	got := lookup(t, srv.URL, "PostgreSQL")
	if got["found"] != true {
		t.Fatalf("found = %v, want true", got["found"])
	}
	if got["title"] != "PostgreSQL" {
		t.Errorf("title = %v, want PostgreSQL", got["title"])
	}
	if *searches != 0 {
		t.Errorf("the ranker was asked %d times when the title was exact", *searches)
	}
}

// Article titles capitalise their first letter, so a lowercase query has to be
// tried both ways before falling through to search.
func TestExactTitleTriesTheCapital(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"Photosynthesis": "<p>Photosynthesis is a process.</p>"},
		nil)

	got := lookup(t, srv.URL, "photosynthesis")
	if got["title"] != "Photosynthesis" {
		t.Errorf("title = %v, want Photosynthesis", got["title"])
	}
}

// The failure this guard exists for: a short query with no article of its own,
// where the best ranked hit shares nothing with it and reads as an answer.
func TestShortQueryWithNoRealMatchIsAMiss(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"Abu_Simbel_temples": "<p>Abu Simbel is in Egypt.</p>"},
		[]string{"Abu Simbel temples"})

	got := lookup(t, srv.URL, "goodyear welt")
	if got["found"] != false {
		t.Fatalf("found = %v, want false for a match sharing no word", got["found"])
	}
	if got["summary"] != nil {
		t.Error("a rejected match still carried a summary")
	}
	near, _ := got["near_titles"].([]string)
	if len(near) == 0 || near[0] != "Abu Simbel temples" {
		t.Errorf("near_titles = %v, want the rejected title first", got["near_titles"])
	}
}

// A question is not what this takes. Ranking lead sections across 19 million
// articles answered "who is the leader of north korea" with a military history
// article, so a question is refused and the model is told to pass the subject.
func TestAQuestionIsRefusedRatherThanAnsweredWrong(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"Military_history_of_Korea": "<p>Korea's military history.</p>"},
		[]string{"Military history of Korea"})

	got := lookup(t, srv.URL, "who is the leader of north korea")
	if got["found"] != false {
		t.Fatalf("found = %v, want false for a question", got["found"])
	}
	note, _ := got["note"].(string)
	if !strings.Contains(note, "name of the thing") {
		t.Errorf("note does not say to pass the subject: %q", note)
	}
}

// The same question asked as a subject has to work, or the advice in the miss
// above goes nowhere.
func TestTheSubjectBehindAQuestionResolves(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"North_Korea": "<p>North Korea is a country in East Asia.</p>"},
		[]string{"North Korea"})

	got := lookup(t, srv.URL, "North Korea")
	if got["title"] != "North Korea" {
		t.Errorf("title = %v, want North Korea", got["title"])
	}
}

// A title carrying more than was asked for still matches when the extra is a
// qualifier, which is how a place resolves from the name people use for it.
func TestAQualifiedTitleStillMatches(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"Yadkin_Valley,_North_Carolina": "<p>The Yadkin Valley is a region.</p>"},
		[]string{"Yadkin Valley, North Carolina"})

	got := lookup(t, srv.URL, "yadkin valley")
	if got["found"] != true {
		t.Fatalf("found = %v, want true", got["found"])
	}
}

// The licence notice kiwix appends to every article would otherwise end every
// summary on the same two sentences.
func TestTheLicenceFooterIsStripped(t *testing.T) {
	article := `<p>A thing exists.</p><div class="zim-footer">This article is issued from Wikipedia.</div>`
	srv, _ := fakeKiwix(t, map[string]string{"Thing": article}, nil)

	got := lookup(t, srv.URL, "Thing")
	sum, _ := got["summary"].(string)
	if strings.Contains(sum, "issued from Wikipedia") {
		t.Errorf("the licence footer survived: %s", sum)
	}
	if !strings.Contains(sum, "A thing exists") {
		t.Errorf("the prose was lost: %s", sum)
	}
}

// An infobox flattens into a run of labels with no sentence in it, which is
// most of what a lead section weighs on a page that has one.
func TestTheInfoboxIsStripped(t *testing.T) {
	article := `<h1>Sourdough</h1>
	<table class="infobox"><tr><th>Type</th><td>Bread</td></tr>
	<tr><td><table><tr><td>Nested junk</td></tr></table></td></tr></table>
	<p>Sourdough bread is made by fermentation.</p>`
	srv, _ := fakeKiwix(t, map[string]string{"Sourdough": article}, nil)

	got := lookup(t, srv.URL, "Sourdough")
	sum, _ := got["summary"].(string)
	for _, junk := range []string{"Nested junk", "Bread"} {
		if strings.Contains(sum, junk) {
			t.Errorf("summary still carries table content %q: %s", junk, sum)
		}
	}
	if !strings.Contains(sum, "fermentation") {
		t.Errorf("summary lost the prose: %s", sum)
	}
}

// Cutting at the first heading is what keeps this a lead section if the ZIM is
// ever swapped for a flavour that carries whole articles.
func TestOnlyTheLeadSectionIsReturned(t *testing.T) {
	article := `<p>The lead sentence.</p><h2 id="History">History</h2><p>Everything after.</p>`
	srv, _ := fakeKiwix(t, map[string]string{"Thing": article}, nil)

	got := lookup(t, srv.URL, "Thing")
	sum, _ := got["summary"].(string)
	if strings.Contains(sum, "Everything after") {
		t.Errorf("the body below the first heading came back: %s", sum)
	}
	if !strings.Contains(sum, "lead sentence") {
		t.Errorf("the lead is missing: %s", sum)
	}
}

// Nothing found has to be a miss the model is told to search past, since the
// snapshot is large enough that an empty result reads as proof of absence.
func TestNothingFoundSaysSoAndSendsItToSearch(t *testing.T) {
	srv, _ := fakeKiwix(t, nil, nil)

	got := lookup(t, srv.URL, "zzzznotathing")
	if got["found"] != false {
		t.Fatalf("found = %v, want false", got["found"])
	}
	note, _ := got["note"].(string)
	if !strings.Contains(note, "web_search") {
		t.Errorf("note does not send it to search: %q", note)
	}
}

// The date is the difference between background and a stale fact presented as
// current, so it has to reach the model with the answer.
func TestTheSnapshotDateIsInTheResult(t *testing.T) {
	srv, _ := fakeKiwix(t, map[string]string{"Thing": "<p>A thing.</p>"}, nil)

	got := lookup(t, srv.URL, "Thing")
	note, _ := got["note"].(string)
	if !strings.Contains(note, "June 2026") {
		t.Errorf("note does not carry the snapshot date: %q", note)
	}
}

// A title with a space or a bracket has to survive into a url the model can
// hand to web_fetch.
func TestTheWikipediaURLIsUsable(t *testing.T) {
	srv, _ := fakeKiwix(t,
		map[string]string{"Supreme_Leader_(North_Korea)": "<p>The supreme leader.</p>"},
		[]string{"Supreme Leader (North Korea)"})

	got := lookup(t, srv.URL, "who is the leader of north korea")
	want := "https://en.wikipedia.org/wiki/Supreme_Leader_(North_Korea)"
	if got["url"] != want {
		t.Errorf("url = %v, want %v", got["url"], want)
	}
	if _, err := url.Parse(got["url"].(string)); err != nil {
		t.Errorf("url does not parse: %v", err)
	}
}

// The snapshot is on the bridge, so it must not be reachable through the fence
// that exists for third party hosts, and must not put a container in the
// penalty box when it blips.
func TestTheSnapshotIsNotRateLimitedLikeAThirdParty(t *testing.T) {
	srv, _ := fakeKiwix(t, map[string]string{"Thing": "<p>A thing.</p>"}, nil)
	d := wikiDeps()

	for i := 0; i < 5; i++ {
		if _, err := wikiLookup(context.Background(), d, srv.URL, "Thing"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if down := d.Guard.Down(); len(down) > 0 {
		t.Errorf("the snapshot host was put in the penalty box: %v", down)
	}
}
