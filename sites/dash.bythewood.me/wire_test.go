package main

import (
	"strings"
	"testing"
	"time"
)

// row builds a dated at a fixed offset back from a fixed now, so the tests can
// talk about order without carrying timestamps around.
func row(source string, minsAgo int) dated {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	id := source + string(rune('a'+minsAgo))
	return dated{
		Headline: Headline{Title: id, URL: "https://example.test/" + id, Source: source},
		at:       base.Add(-time.Duration(minsAgo) * time.Minute),
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

// The panel reads down newest to oldest, which is the whole shape Isaac asked
// for, and the cap must not reorder anything on the way there.
func TestPickReadsNewestFirst(t *testing.T) {
	all := []dated{row("NPR", 5), row("BBC", 20), row("NPR", 40), row("BBC", 90)}

	out := pick(all)
	if got := sources(out); got != "NPR BBC NPR BBC" {
		t.Errorf("want the input order back, got %v", got)
	}
}

func TestPickFillsThePanel(t *testing.T) {
	var all []dated
	for i := 0; i < 40; i++ {
		all = append(all, row("BBC", i))
		all = append(all, row("NPR", i))
	}

	out := pick(all)
	if len(out) != wireShown {
		t.Fatalf("want %d rows, got %d", wireShown, len(out))
	}
}

// BBC supplies two of the three feeds and posts more often, so without the cap
// a busy afternoon is a column of one outlet.
func TestPickCapsOneOutlet(t *testing.T) {
	var all []dated
	for i := 0; i < 20; i++ {
		all = append(all, row("BBC", i))
	}
	for i := 0; i < 20; i++ {
		all = append(all, row("NPR", i+30))
	}

	out := pick(all)
	bbc := 0
	for _, h := range out {
		if h.Source == "BBC" {
			bbc++
		}
	}
	if bbc > wirePerSource {
		t.Errorf("BBC took %d rows, cap is %d: %v", bbc, wirePerSource, sources(out))
	}
}

// The cap is a preference, not a reason to serve a short panel.
func TestPickFillsPastTheCapWhenItHasTo(t *testing.T) {
	var all []dated
	for i := 0; i < 14; i++ {
		all = append(all, row("NPR", i))
	}

	out := pick(all)
	if len(out) != wireShown {
		t.Fatalf("want %d rows from a single outlet, got %d", wireShown, len(out))
	}
	// The fill has to keep the panel in time order rather than append what the
	// cap passed over to the bottom.
	// row names each headline in age order, so the panel is in time order when
	// the titles come back ascending.
	for i := 1; i < len(out); i++ {
		if out[i-1].Title > out[i].Title {
			t.Fatalf("rows are out of order: %v", sources(out))
		}
	}
}

func TestPickTakesWhatItHasWhenTheresLittle(t *testing.T) {
	all := []dated{row("NPR", 1), row("BBC", 2)}

	out := pick(all)
	if len(out) != 2 {
		t.Fatalf("want 2 rows, got %d", len(out))
	}
}

func TestPromotionalKeepsRealNews(t *testing.T) {
	keep := []string{
		"Congress averts a government shutdown ahead of the midterms",
		"Federal Reserve issues FOMC statement",
		"Germany says Russia behind Leipzig airport drone attack",
	}
	for _, title := range keep {
		if promotional(title) || sidebar(title) {
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

// BBC mixes video and rolling live pages into its news feeds under a headline
// that reads like a story.
func TestSidebarDropsTheNonStories(t *testing.T) {
	drop := []string{
		"Watch: Defence calls for one juror to be dismissed",
		"In pictures: the storm that flooded Toronto",
		"Live updates: the Fed decision",
	}
	for _, title := range drop {
		if !sidebar(title) {
			t.Errorf("kept a non story: %q", title)
		}
	}
}

// NPR's feeds carry the same story under the same headline, and two outlets
// carry it under headlines that differ by punctuation.
func TestTitleKeyMatchesAcrossFeeds(t *testing.T) {
	a := "Congress averts a government shutdown ahead of the midterms"
	b := "Congress averts a government shutdown ahead of the midterms."
	if titleKey(a) != titleKey(b) {
		t.Errorf("same story read as two: %q vs %q", titleKey(a), titleKey(b))
	}
	if titleKey(a) == titleKey("Germany says Russia behind Leipzig drone attack") {
		t.Error("two stories read as one")
	}
}

func TestParseRSSTimeReadsBothFeedFormats(t *testing.T) {
	for _, v := range []string{"Thu, 03 Sep 2026 22:31:23 GMT", "Thu, 03 Sep 2026 18:19:02 -0400"} {
		got, err := parseRSSTime(v)
		if err != nil {
			t.Fatalf("pubDate %q did not parse: %v", v, err)
		}
		if got.Year() != 2026 || got.Month() != time.September || got.Day() != 3 {
			t.Errorf("%q parsed to %v", v, got)
		}
	}
}

func TestClipDropsBBCVideoAndLive(t *testing.T) {
	if !clip("https://www.bbc.co.uk/news/videos/ce30lqln299o?at_medium=RSS") {
		t.Error("kept a video clip")
	}
	if !clip("https://www.bbc.co.uk/news/live/cx2g5nrpz4vt?at_medium=RSS") {
		t.Error("kept a live page")
	}
	if clip("https://www.bbc.co.uk/news/articles/cj06q4ynpmjo?at_medium=RSS") {
		t.Error("dropped a story")
	}
}

// Two outlets word the same story differently, which is what the exact title
// key cannot catch, and a duplicate row is the most obvious thing on a panel
// of ten.
func TestDedupeCatchesTheSameStoryTwice(t *testing.T) {
	npr := "Leon Black defies subpoena to testify in Epstein inquiry and sues House panel"
	bbc := "US billionaire Leon Black defies summons and sues Epstein panel"
	if !sameStory(significant(npr), significant(bbc)) {
		t.Errorf("read one story as two: %q and %q", npr, bbc)
	}

	// Same day, same president, different stories.
	a := "Trump asks Supreme Court to lift block on USPS plan to restrict mail voting"
	b := "Trump $1 coin makes him first living president on US currency in a century"
	if sameStory(significant(a), significant(b)) {
		t.Errorf("read two stories as one: %q and %q", a, b)
	}
}

func TestDedupeKeepsTheNewerCopy(t *testing.T) {
	newer := row("BBC", 5)
	newer.Title = "US billionaire Leon Black defies summons and sues Epstein panel"
	older := row("NPR", 60)
	older.Title = "Leon Black defies subpoena to testify in Epstein inquiry and sues House panel"

	out := dedupe([]dated{newer, older})
	if len(out) != 1 {
		t.Fatalf("want 1 row, got %d", len(out))
	}
	if out[0].Source != "BBC" {
		t.Errorf("want the newer copy, got %q", out[0].Source)
	}
}

// An outlet runs an explainer beside the story it explains, and the panel wants
// the story.
func TestSidebarDropsTheExplainer(t *testing.T) {
	if !sidebar("Could Lindsay Clancy trial end in a mistrial? Here are the jury's options") {
		t.Error("kept an explainer")
	}
	if sidebar("Trump asks Supreme Court to lift block on USPS plan") {
		t.Error("dropped a story")
	}
}

// One outlet writes charged where the other writes charges, and the stem is
// what makes those the same word.
func TestDedupeCatchesTheSameStoryWordedApart(t *testing.T) {
	bbc := "ICE agent charged with lying about shooting Venezuelan man during crackdown"
	npr := "ICE officer faces federal charges for lying over shooting of Venezuelan immigrant"
	if !sameStory(significant(bbc), significant(npr)) {
		t.Errorf("read one story as two: %q and %q", bbc, npr)
	}

	// Three angles on one death are three stories and the panel keeps them all.
	a := "Tributes to Gloria Steinem are flooding in, from Hollywood to Capitol Hill"
	b := "Feminist activist and journalist Gloria Steinem dies, aged 92"
	if sameStory(significant(a), significant(b)) {
		t.Errorf("read two stories as one: %q and %q", a, b)
	}
}
