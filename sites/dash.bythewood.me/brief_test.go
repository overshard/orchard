package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func inNY(t *testing.T, v string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04", v, easternTime())
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestLatestSlot(t *testing.T) {
	cases := []struct {
		now      string
		weekdays bool
		kind     string
		at       string
	}{
		{"2026-10-07 06:59", false, "close", "2026-10-06 16:05"},
		{"2026-10-07 07:00", false, "morning", "2026-10-07 07:00"},
		{"2026-10-07 10:30", false, "morning", "2026-10-07 07:00"},
		{"2026-10-07 11:00", false, "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:04", false, "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:05", false, "close", "2026-10-07 16:05"},
		// Saturday morning, the news has a run and the markets keep Friday's close.
		{"2026-10-10 08:00", false, "morning", "2026-10-10 07:00"},
		{"2026-10-10 08:00", true, "close", "2026-10-09 16:05"},
		// Monday before the open still reaches back over the weekend.
		{"2026-10-12 06:00", true, "close", "2026-10-09 16:05"},
	}
	for _, c := range cases {
		slot, at := latestSlot(inNY(t, c.now), c.weekdays)
		if slot.kind != c.kind || !at.Equal(inNY(t, c.at)) {
			t.Errorf("%s weekdays=%t: got %s at %s, want %s at %s", c.now, c.weekdays, slot.kind, at.Format("2006-01-02 15:04"), c.kind, c.at)
		}
	}
}

func st(source, lean, title string, minsAgo int) story {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	return story{title: title, url: "https://" + strings.ToLower(source) + ".test/" + titleKey(title),
		source: source, lean: lean, at: base.Add(-time.Duration(minsAgo) * time.Minute), words: significant(title)}
}

// The event three outlets carried outranks a newer one only one did, since
// coverage is the ranking and recency only breaks ties.
func TestClusterRanksByOutlets(t *testing.T) {
	all := []story{
		st("FOX", "R", "Senate passes stopgap spending bill to avert shutdown", 60),
		st("NPR", "L", "Senate passes stopgap spending bill, averting a shutdown", 50),
		st("AP", "C", "Senate passes stopgap bill to avert government shutdown", 40),
		st("CBS", "L", "Wildfire forces evacuations in Southern California", 5),
	}
	out := clusterStories(all, 10)
	if len(out) != 2 {
		t.Fatalf("want 2 events, got %d", len(out))
	}
	if len(out[0].outlets()) != 3 {
		t.Errorf("want the shutdown story first with 3 outlets, got %v", out[0].outlets())
	}
	if out[0].lead().source != "AP" {
		t.Errorf("want the center outlet's telling, got %s", out[0].lead().source)
	}
}

func TestCiteCountsSidesAndPutsCenterFirst(t *testing.T) {
	events := clusterStories([]story{
		st("FOX", "R", "Senate passes stopgap spending bill to avert shutdown", 60),
		st("NPR", "L", "Senate passes stopgap spending bill, averting a shutdown", 50),
		st("AP", "C", "Senate passes stopgap bill to avert government shutdown", 40),
	}, 10)

	pt := cite(events, []int{1, 1, 9, 0})
	if pt.Coverage != "L1 C1 R1" {
		t.Errorf("coverage %q", pt.Coverage)
	}
	if len(pt.Links) != 3 || pt.Links[0].Source != "AP" {
		t.Errorf("links %+v", pt.Links)
	}
	if pt.Note != "" {
		t.Errorf("a story all sides carried was flagged %q", pt.Note)
	}
}

// One side alone covering a story is marked and kept, never dropped.
func TestCiteFlagsOneSidedCoverage(t *testing.T) {
	right := clusterStories([]story{
		st("FOX", "R", "Border crossings fall to lowest level in decades", 30),
		st("EXAMINER", "R", "Border crossings fall to lowest level in decades, data shows", 20),
	}, 10)
	if got := cite(right, []int{1}).Note; got != "RIGHT-LEANING OUTLETS ONLY" {
		t.Errorf("right only: %q", got)
	}

	left := clusterStories([]story{
		st("NPR", "L", "Report finds climate funding cuts hit rural towns hardest", 30),
		st("PBS", "L", "Climate funding cuts hit rural towns hardest, report finds", 20),
	}, 10)
	if got := cite(left, []int{1}).Note; got != "LEFT-LEANING OUTLETS ONLY" {
		t.Errorf("left only: %q", got)
	}

	single := clusterStories([]story{st("FOX", "R", "Border crossings fall to lowest level in decades", 30)}, 10)
	if got := cite(single, []int{1}).Note; got != "" {
		t.Errorf("one outlet is not a blindspot, got %q", got)
	}
}

// The model is handed outlet names and never their lean, since a model told
// which side a story came from starts writing about the sides.
func TestClusterListLeavesLeanOut(t *testing.T) {
	events := clusterStories([]story{st("FOX", "R", "Senate passes stopgap spending bill", 60)}, 10)
	list := clusterList(events, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(list, "[1] 1 outlet (FOX), 1h ago: Senate passes") {
		t.Errorf("list %q", list)
	}
	for _, word := range []string{"right", "left", "lean", "conservative", "liberal"} {
		if strings.Contains(strings.ToLower(list), word) {
			t.Errorf("list mentions %q: %q", word, list)
		}
	}
}

func TestTidy(t *testing.T) {
	got := tidy("Stocks fell [3, 4] after the Fed — citing inflation [12]. ")
	if got != "Stocks fell after the Fed, citing inflation." {
		t.Errorf("got %q", got)
	}
}

func TestBlurbStripsMarkupAndCuts(t *testing.T) {
	got := blurb(`<p>The <b>Senate</b> voted 52&amp;48.</p>`)
	if got != "The Senate voted 52&48." {
		t.Errorf("got %q", got)
	}
	long := blurb(strings.Repeat("word ", 100))
	if len(long) > 250 || !strings.HasSuffix(long, "...") {
		t.Errorf("not cut: %d %q", len(long), long[len(long)-10:])
	}
}

// Every brief feed has a budget and a row on the UPLINK panel, or a call to it
// is refused by the guard and nobody can see why.
func TestBriefFeedsAreGuarded(t *testing.T) {
	for _, f := range append(append([]briefFeed{}, newsFeeds...), marketFeeds...) {
		if _, ok := budgets[f.endpoint]; !ok {
			t.Errorf("%s has no budget", f.endpoint)
		}
		found := false
		for _, o := range feedOrder {
			found = found || o.key == f.endpoint
		}
		if !found {
			t.Errorf("%s is not on the uplink panel", f.endpoint)
		}
		if f.lean != "L" && f.lean != "C" && f.lean != "R" {
			t.Errorf("%s has lean %q", f.name, f.lean)
		}
	}
}

// A watchdog report and a lawsuit about different money, on a day the same
// four words lead half the headlines, are two events.
func TestClusterIgnoresTheDaysCommonWords(t *testing.T) {
	all := []story{
		st("HILL", "C", "Watchdog: Trump administration illegally withheld federal research funds", 30),
		st("FOX", "R", "DNC lawsuit accuses Trump administration of diverting $20M in federal funds for propaganda ads", 20),
	}
	for i := 0; i < 30; i++ {
		all = append(all, st("AP", "C", fmt.Sprintf("Trump administration federal funds story number %d", i), 40+i))
	}
	for _, c := range clusterStories(all, 100) {
		if len(c.outlets()) > 1 {
			t.Errorf("merged %v", c.outlets())
		}
	}
}
