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
		// "over the weekend including today" has no answer in either of the
		// other two, and the plain weekend window excludes today by definition.
		{"weekend-and-today", "2026-09-05 00:00", "2026-09-07 16:20"},
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

// The rundown carried the same NPR story twice on 2026-09-08 because the two
// copies came off different feeds with different tracking parameters, and the
// url was the only key that got looked at.
func TestDedupeNewsIgnoresTrackingParameters(t *testing.T) {
	in := []NewsItem{
		{Source: "NPR Technology", Headline: "Voters are fed up with data centers", URL: "https://www.npr.org/2026/09/08/data-centers"},
		{Source: "NPR Technology", Headline: "Voters are fed up with data centers", URL: "https://npr.org/2026/09/08/data-centers/?utm_source=rss"},
		{Source: "BBC Technology", Headline: "Voters are fed up with data centers", URL: "https://bbc.co.uk/news/tech-1"},
	}
	out := dedupeNews(in)
	if len(out) != 2 {
		t.Fatalf("dedupe left %d items, want the NPR pair collapsed and the BBC one kept", len(out))
	}
	// Two publishers carrying one story is worth knowing, so the BBC copy stays.
	if out[1].Source != "BBC Technology" {
		t.Errorf("the second publisher was dropped: %#v", out)
	}
}

// One story written up under two headlines is still one story, which an exact
// key cannot see. The Navier-Stokes paper led the tech section twice.
func TestSameStoryCatchesARewrittenHeadline(t *testing.T) {
	a := headlineWords("On the Navier-Stokes Millennium Prize Problem")
	b := headlineWords("Navier-Stokes Millennium Prize Problem, by Tristan Buckmaster")
	if !sameStory(a, b) {
		t.Error("two headlines about one paper were treated as two stories")
	}

	// And two genuinely different stories are not merged, which would lose one.
	c := headlineWords("Mistral raises 3B euros for sovereign open-weight AI")
	if sameStory(a, c) {
		t.Error("two unrelated stories were merged")
	}
	// A short headline has too little in it to judge, so it is left alone.
	if sameStory(headlineWords("Keep Our Servers Running"), headlineWords("Servers Running Hot")) {
		t.Error("two short headlines were merged on a couple of shared words")
	}
}
