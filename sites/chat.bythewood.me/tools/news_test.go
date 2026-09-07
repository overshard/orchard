package tools

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, newYork())
	if err != nil {
		panic(err)
	}
	return t
}

// The windows are the whole point of this tool: if Isaac says today he means
// today, so each one is pinned against a fixed clock rather than trusted.
func TestNewsWindowsAreTakenLiterally(t *testing.T) {
	// A Monday afternoon, so "weekend" is the two days just gone.
	now := at("2026-09-07 16:20")

	for _, tc := range []struct {
		window, from, to string
	}{
		{"today", "2026-09-07 00:00", "2026-09-07 16:20"},
		{"yesterday", "2026-09-06 00:00", "2026-09-07 00:00"},
		{"weekend", "2026-09-05 00:00", "2026-09-07 00:00"},
		{"week", "2026-09-01 00:00", "2026-09-07 16:20"},
		{"month", "2026-08-09 00:00", "2026-09-07 16:20"},
	} {
		since, until, label := resolveWindow(now, tc.window)
		if !since.Equal(at(tc.from)) {
			t.Errorf("%s: from = %s, want %s", tc.window, since, at(tc.from))
		}
		if !until.Equal(at(tc.to)) {
			t.Errorf("%s: to = %s, want %s", tc.window, until, at(tc.to))
		}
		if label == "" {
			t.Errorf("%s: no label, so the answer cannot say which days it read", tc.window)
		}
	}
}

// Asked on a Saturday, the weekend is the one being had and not the one before,
// and it cannot run past now.
func TestTheWeekendAskedForOnTheWeekendIsThisOne(t *testing.T) {
	now := at("2026-09-05 11:00")
	since, until, _ := resolveWindow(now, "weekend")
	if !since.Equal(at("2026-09-05 00:00")) {
		t.Errorf("from = %s, want Saturday morning", since)
	}
	if !until.Equal(now) {
		t.Errorf("to = %s, want now rather than a future Sunday night", until)
	}
}

// An unknown word is today rather than an error, since the model picking
// something outside the enum should not cost the turn.
func TestAnUnknownWindowIsToday(t *testing.T) {
	now := at("2026-09-07 16:20")
	since, _, _ := resolveWindow(now, "fortnight")
	if !since.Equal(at("2026-09-07 00:00")) {
		t.Errorf("from = %s, want this morning", since)
	}
}

func TestFeedTimesParse(t *testing.T) {
	for _, s := range []string{
		"Mon, 07 Sep 2026 15:04:05 +0000",
		"Mon, 7 Sep 2026 15:04:05 GMT",
		"2026-09-07T15:04:05Z",
		"2026-09-07T11:04:05-04:00",
	} {
		if _, ok := parseFeedTime(s); !ok {
			t.Errorf("did not parse %q", s)
		}
	}
	if _, ok := parseFeedTime("last tuesday"); ok {
		t.Error("parsed something that is not a date")
	}
}

// A feed description arrives with markup often enough that leaving it in spends
// the model's window on span tags.
func TestSummariesLoseTheirMarkup(t *testing.T) {
	got := trimSummary(`<p>Five dead after a <a href="x">crash</a>.</p>`)
	if got != "Five dead after a crash." {
		t.Errorf("summary = %q", got)
	}
}

// Two publishers carrying one story is worth knowing. One publisher's own item
// arriving twice is not.
func TestDuplicatesGoAndSeparatePublishersStay(t *testing.T) {
	in := []NewsItem{
		{Source: "BBC", Headline: "Plane crash at Miami", URL: "https://bbc/1"},
		{Source: "BBC", Headline: "Plane crash at Miami", URL: "https://bbc/1"},
		{Source: "NPR", Headline: "Plane crash at Miami", URL: "https://npr/1"},
	}
	if got := dedupeNews(in); len(got) != 2 {
		t.Errorf("kept %d items, want the two publishers", len(got))
	}
}

// On general news a scored post about timestamps must not outrank a fatal
// crash, and on tech the score is exactly what should lead.
func TestWhatLeadsDependsOnTheTopic(t *testing.T) {
	desk := NewsItem{Source: "NPR", Headline: "Five dead in crash", rank: 0}
	scored := NewsItem{Source: "Hacker News", Headline: "Timestamp conversion", Points: 900}

	general := []NewsItem{scored, desk}
	sortNews(general, false)
	if general[0].Source != "NPR" {
		t.Errorf("general led with %q", general[0].Source)
	}

	tech := []NewsItem{desk, scored}
	sortNews(tech, true)
	if tech[0].Source != "Hacker News" {
		t.Errorf("tech led with %q", tech[0].Source)
	}
}

// The bug that started this: a flat cap over everything, sorted desks first,
// took 28 of 45 items and left all fourteen Hacker News stories and both
// Lobsters ones on the floor. A section holding both has to split its slots.
func TestASectionHoldingBothGivesTheAggregatorsSlots(t *testing.T) {
	// A plain taker, so this tests the sharing and not the sorting.
	take := func(items []NewsItem, slots int, _ bool) []NewsItem {
		if len(items) > slots {
			return items[:slots]
		}
		return items
	}
	desks := make([]NewsItem, 12)
	for i := range desks {
		desks[i] = NewsItem{Source: "BBC"}
	}
	scored := make([]NewsItem, 14)
	for i := range scored {
		scored[i] = NewsItem{Source: "Hacker News", Points: 400}
	}

	got := fillSection(section{slots: 9, aggregators: true,
		feeds: []feed{{"BBC", "u"}}}, desks, scored, take)
	if len(got) != 9 {
		t.Fatalf("filled %d of 9 slots", len(got))
	}
	counts := map[string]int{}
	for _, it := range got {
		counts[it.Source]++
	}
	if counts["Hacker News"] == 0 {
		t.Errorf("the aggregators were starved again: %v", counts)
	}
	if counts["BBC"] == 0 {
		t.Errorf("the newsrooms were starved: %v", counts)
	}
}

// A section with nothing from one side still fills up from the other rather
// than coming back half empty.
func TestASectionFillsUpWhenOneSideIsEmpty(t *testing.T) {
	take := func(items []NewsItem, slots int, _ bool) []NewsItem {
		if len(items) > slots {
			return items[:slots]
		}
		return items
	}
	scored := make([]NewsItem, 10)
	for i := range scored {
		scored[i] = NewsItem{Source: "Hacker News", Points: 300}
	}
	got := fillSection(section{slots: 6, aggregators: true, feeds: []feed{{"BBC", "u"}}}, nil, scored, take)
	if len(got) != 6 {
		t.Errorf("filled %d of 6 slots with only aggregators available", len(got))
	}
}
