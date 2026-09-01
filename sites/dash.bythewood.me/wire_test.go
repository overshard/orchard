package main

import (
	"strings"
	"testing"
	"time"
)

// row builds a dated at a fixed offset back from a fixed now, so the tests can
// talk about order without carrying timestamps around.
func row(source, bucket string, minsAgo int) dated {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return dated{
		Headline: Headline{Title: source + bucket + string(rune('a'+minsAgo)), URL: "https://example.test/" + source + bucket + string(rune('a'+minsAgo)), Source: source},
		at:       base.Add(-time.Duration(minsAgo) * time.Minute),
		bucket:   bucket,
	}
}

func sources(out []Headline) string {
	var b strings.Builder
	for i, h := range out {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(h.Source)
	}
	return b.String()
}

// The whole reason the panel balances rather than sorting: a news cycle where
// one bucket posts constantly used to take every row.
func TestBalanceGivesEveryBucketARow(t *testing.T) {
	var all []dated
	for i := 0; i < 20; i++ {
		all = append(all, row("BBC", bucketWorld, i))
	}
	all = append(all, row("NPR", bucketPolitics, 30))
	all = append(all, row("NPR", bucketMacro, 40))
	all = append(all, row("PBS", bucketUS, 50))

	out := balance(all, nil)
	if len(out) != wireShown {
		t.Fatalf("want %d rows, got %d", wireShown, len(out))
	}

	got := map[string]int{}
	for _, d := range all {
		for _, h := range out {
			if h.URL == d.URL {
				got[d.bucket]++
			}
		}
	}
	for _, b := range buckets {
		if got[b] == 0 {
			t.Errorf("bucket %s got no row: %v", b, sources(out))
		}
	}
}

func TestBalanceCapsOneOutlet(t *testing.T) {
	var all []dated
	for i := 0; i < 6; i++ {
		all = append(all, row("NPR", bucketUS, i))
		all = append(all, row("NPR", bucketPolitics, i+10))
	}
	// Enough from two other outlets that the panel can fill inside the caps.
	// With only one other, three plus three is short of eight and the fill is
	// right to overrun the cap rather than serve a short panel.
	for i := 0; i < 6; i++ {
		all = append(all, row("BBC", bucketWorld, i+20))
		all = append(all, row("PBS", bucketMacro, i+30))
	}

	out := balance(all, nil)
	npr := 0
	for _, h := range out {
		if h.Source == "NPR" {
			npr++
		}
	}
	if npr > wirePerSource {
		t.Errorf("NPR took %d rows, cap is %d: %v", npr, wirePerSource, sources(out))
	}
}

// The cap is a preference, not a reason to serve a short panel.
func TestBalanceFillsPastTheCapWhenItHasTo(t *testing.T) {
	var all []dated
	for i := 0; i < 12; i++ {
		all = append(all, row("NPR", bucketUS, i))
	}

	out := balance(all, nil)
	if len(out) != wireShown {
		t.Fatalf("want %d rows from a single outlet, got %d", wireShown, len(out))
	}
}

// Every bucket capped or empty has to end the loop rather than spin it.
func TestBalanceStopsWhenNothingIsLeft(t *testing.T) {
	all := []dated{row("NPR", bucketUS, 1), row("BBC", bucketWorld, 2)}

	done := make(chan []Headline, 1)
	go func() { done <- balance(all, nil) }()
	select {
	case out := <-done:
		if len(out) != 2 {
			t.Fatalf("want 2 rows, got %d", len(out))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("balance did not return")
	}
}

// The Fed row is pinned, so it leads and it does not lose its slot to the fill.
func TestBalanceKeepsThePinnedRowFirst(t *testing.T) {
	fed := Headline{Title: "Federal Reserve issues FOMC statement", URL: "https://federalreserve.test/fomc", Source: "FED", Age: "34d"}
	var all []dated
	for i := 0; i < 12; i++ {
		all = append(all, row("NPR", bucketUS, i))
	}

	out := balance(all, []Headline{fed})
	if len(out) != wireShown {
		t.Fatalf("want %d rows, got %d", wireShown, len(out))
	}
	if out[0].Source != "FED" {
		t.Errorf("want FED first, got %q", out[0].Source)
	}
}

func TestPromotionalKeepsRealNews(t *testing.T) {
	keep := []string{
		"Congress averts a government shutdown ahead of the midterms",
		"Federal Reserve issues FOMC statement",
		"Germany says Russia behind Leipzig airport drone attack",
	}
	for _, title := range keep {
		if promotional(title) {
			t.Errorf("filtered real news: %q", title)
		}
	}

	drop := []string{
		"Sign up for our daily briefing",
		"Sponsored: the best cash back cards",
		"Prime Day deals you can still get",
	}
	for _, title := range drop {
		if !promotional(title) {
			t.Errorf("kept an ad: %q", title)
		}
	}
}

// NPR's news and politics feeds carry the same story under the same headline,
// and the Fed's title is not one of them.
func TestTitleKeyMatchesAcrossFeeds(t *testing.T) {
	a := "Congress averts a government shutdown ahead of the midterms"
	b := "Congress averts a government shutdown ahead of the midterms."
	if titleKey(a) != titleKey(b) {
		t.Errorf("same story read as two: %q vs %q", titleKey(a), titleKey(b))
	}
	if titleKey(a) == titleKey("Federal Reserve issues FOMC statement") {
		t.Error("two stories read as one")
	}
}

// The Fed wraps every field in CDATA, which is the shape that has to parse for
// the pinned row to exist at all.
func TestParseRSSTimeReadsTheFedFormat(t *testing.T) {
	got, err := parseRSSTime("Wed, 29 Jul 2026 18:00:00 GMT")
	if err != nil {
		t.Fatalf("fed pubDate did not parse: %v", err)
	}
	if got.Year() != 2026 || got.Month() != time.July || got.Day() != 29 {
		t.Errorf("parsed to %v", got)
	}
}

// PBS posts least often of the three US feeds and was losing every slot to
// whoever was faster, which is the reason the bucket pass prefers the outlet
// with the fewest rows over the newest story outright.
func TestBalanceSpreadsOutletsInsideABucket(t *testing.T) {
	var all []dated
	// Two fast outlets and one slow one, all in the same bucket.
	for i := 0; i < 10; i++ {
		all = append(all, row("NPR", bucketUS, i))
		all = append(all, row("BBC", bucketUS, i))
	}
	all = append(all, row("PBS", bucketUS, 90))

	out := balance(all, nil)
	for _, h := range out {
		if h.Source == "PBS" {
			return
		}
	}
	t.Errorf("the slow outlet got no row: %v", sources(out))
}

// The panel reads down in time order under the pinned rows, not in the order
// the four buckets happened to fill.
func TestBalanceReadsNewestFirst(t *testing.T) {
	all := []dated{
		row("NPR", bucketUS, 60),
		row("BBC", bucketWorld, 5),
		row("PBS", bucketMacro, 30),
	}

	fed := Headline{Title: "FOMC", URL: "https://federalreserve.test/fomc", Source: "FED"}
	out := balance(all, []Headline{fed})
	if len(out) != 4 {
		t.Fatalf("want 4 rows, got %d", len(out))
	}
	if out[0].Source != "FED" {
		t.Fatalf("want FED pinned first, got %q", out[0].Source)
	}
	if got := sources(out); got != "FED BBC PBS NPR" {
		t.Errorf("want newest first under the pin, got %v", got)
	}
}

func TestClipDropsBBCVideo(t *testing.T) {
	if !clip("https://www.bbc.co.uk/news/videos/ce30lqln299o?at_medium=RSS") {
		t.Error("kept a video clip")
	}
	if clip("https://www.bbc.co.uk/news/articles/cj06q4ynpmjo?at_medium=RSS") {
		t.Error("dropped a story")
	}
}
