package main

import (
	"strings"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

// The answer that started this: every fixture in it had already been played,
// every sentence in it was true, and none of it was what was asked for.
const playedFixtures = `On September 4, 2026, Liverpool FC played Ipswich Town in a match that ended in a draw [2].

- Liverpool FC played Ipswich Town on September 4, 2026 [2].
- Liverpool FC played Newcastle United on August 23, 2026 [4].
- Liverpool FC played Brentford FC on May 24, 2026 [7].`

func TestLatestDate(t *testing.T) {
	now := day(2026, time.September, 5)
	cases := []struct {
		text string
		want string
	}{
		{playedFixtures, "2026-09-04"},
		{"Liverpool play Fulham at Anfield on 12 September 2026.", "2026-09-12"},
		{"The next one is September 12, 2026, at 3pm.", "2026-09-12"},
		{"Published 2026-09-12 by the club.", "2026-09-12"},
		{"They play on 12 September, kick off at 12:30.", "2026-09-12"},
		{"The window closes in September 2026.", "2026-09-01"},
		{"No date anywhere in this sentence.", ""},
	}
	for _, c := range cases {
		got, ok := latestDate(c.text, now)
		if c.want == "" {
			if ok {
				t.Errorf("%q: found %s, wanted nothing", c.text, got.Format("2006-01-02"))
			}
			continue
		}
		if !ok {
			t.Errorf("%q: found no date, wanted %s", c.text, c.want)
			continue
		}
		if got.Format("2006-01-02") != c.want {
			t.Errorf("%q: got %s, want %s", c.text, got.Format("2006-01-02"), c.want)
		}
	}
}

// A date with no year written in December is next January, not ten months ago.
func TestLatestDateRollsTheYear(t *testing.T) {
	got, ok := latestDate("They are back on 3 January.", day(2026, time.December, 20))
	if !ok || got.Format("2006-01-02") != "2027-01-03" {
		t.Errorf("got %s ok=%v, want 2027-01-03", got.Format("2006-01-02"), ok)
	}
}

func TestNoFutureDate(t *testing.T) {
	now := day(2026, time.September, 5)
	if !noFutureDate(playedFixtures, now) {
		t.Error("an answer of played fixtures should not count as answering what is next")
	}
	if !noFutureDate("They have a game soon.", now) {
		t.Error("an answer with no date at all does not answer when")
	}
	// Today counts, since a match this evening is the next one.
	if noFutureDate("They play Fulham on 5 September 2026.", now) {
		t.Error("today is not in the past")
	}
	if noFutureDate("They played on 4 September and play Fulham on 12 September 2026.", now) {
		t.Error("one future date is enough, background about the last match is fine")
	}
}

func TestPastOnly(t *testing.T) {
	now := day(2026, time.September, 5)
	got := pastOnly(playedFixtures, now)
	if len(got) != 1 || !strings.Contains(got[0], "4 September 2026") {
		t.Fatalf("want one warning naming the latest date, got %q", got)
	}
	if got := pastOnly("They play Fulham on 12 September 2026.", now); len(got) != 0 {
		t.Errorf("a dated future answer needs no warning, got %q", got)
	}
	if got := pastOnly("There is a game coming up.", now); len(got) != 1 {
		t.Errorf("an answer with no date should warn, got %q", got)
	}
	if got := pastOnly("", now); len(got) != 0 {
		t.Errorf("an empty answer is handled elsewhere, got %q", got)
	}
}

func TestGuessShapeUpcoming(t *testing.T) {
	for _, q := range []string{
		"when is the next liverpool game",
		"when do liverpool play next",
		"upcoming premier league fixtures",
		"when is the next spacex launch",
		"gta 6 release date",
	} {
		if got := guessShape(q); got != ShapeUpcoming {
			t.Errorf("%q: got %s, want upcoming", q, got)
		}
	}
	// The other half of the same question still reads as news.
	for _, q := range []string{"who won the liverpool game", "did liverpool win last night"} {
		if got := guessShape(q); got != ShapeNews {
			t.Errorf("%q: got %s, want news", q, got)
		}
	}
}

// A shape the planner can pick and the contract map does not hold falls back to
// prose, which is the wrong format quietly.
func TestEveryShapeHasAContract(t *testing.T) {
	for _, s := range shapeEnum {
		c, ok := contracts[s]
		if !ok {
			t.Errorf("%s is in the plan enum with no contract", s)
			continue
		}
		if c.Shape != s || c.Instruction == "" || c.MaxTokens == 0 {
			t.Errorf("%s has an incomplete contract: %+v", s, c)
		}
	}
	if len(shapeEnum) != len(contracts) {
		t.Errorf("%d shapes in the enum against %d contracts", len(shapeEnum), len(contracts))
	}
}

// A fixture list writes the month short, and the page that carries the cup tie
// is not always the page the answer came off.
func TestSoonerAcrossPassages(t *testing.T) {
	now := day(2026, time.September, 5)
	passages := []Passage{
		{ID: 1, Text: "Next match\n\n12 Sept 2026, 14:00/Liverpool FC vs Fulham FC"},
		{ID: 2, Text: "## Upcoming fixtures\n- Liverpool vs Atleti Wed, 9 Sept 2026 - 19:00 - Champions League"},
	}
	answer := "The next Liverpool game is on **12 September 2026** against Fulham FC at Anfield [1]."
	got := sooner(answer, passages, now)
	if len(got) != 1 || !strings.Contains(got[0], "9 September") {
		t.Fatalf("want a warning naming the earlier date, got %q", got)
	}
	if got := sooner("The next one is 9 September 2026 [2].", passages, now); len(got) != 0 {
		t.Errorf("leading with the soonest needs no warning, got %q", got)
	}
	// Nothing sooner in the passages, so nothing to say.
	if got := sooner(answer, passages[:1], now); len(got) != 0 {
		t.Errorf("got %q", got)
	}
	// A page stamping itself with today is not a match today.
	stamped := []Passage{{ID: 1, Text: "**Last updated:** Sep 5, 2026\n\n- 12 Sept 2026, 14:00/Liverpool FC vs Fulham FC"}}
	if got := sooner(answer, stamped, now); len(got) != 0 {
		t.Errorf("a page stamp is not a fixture, got %q", got)
	}
}

func TestShortMonths(t *testing.T) {
	now := day(2026, time.September, 5)
	for text, want := range map[string]string{
		"Wed, 9 Sept 2026 - 19:00":     "2026-09-09",
		"12 Sept 2026, 14:00":          "2026-09-12",
		"Sat Nov 28, 2026 at Goodison": "2026-11-28",
		"31 Feb 2027 is not a day":     "",
	} {
		got, ok := latestDate(text, now)
		if want == "" {
			if ok {
				t.Errorf("%q: read %s off a date that does not exist", text, got.Format("2006-01-02"))
			}
			continue
		}
		if !ok || got.Format("2006-01-02") != want {
			t.Errorf("%q: got %s ok=%v, want %s", text, got.Format("2006-01-02"), ok, want)
		}
	}
}

func TestScheduleAge(t *testing.T) {
	now := day(2026, time.September, 5)
	fresh := []Source{{Published: "2026-06-19"}, {Published: "2026-09-02"}}
	if got := scheduleAge(fresh, now); len(got) != 0 {
		t.Errorf("a page from three days ago is current enough, got %q", got)
	}
	old := []Source{{Published: "2026-06-19"}, {Published: "2026-07-05"}}
	if got := scheduleAge(old, now); len(got) != 1 || !strings.Contains(got[0], "62 days ago") {
		t.Errorf("want one warning naming the age, got %q", got)
	}
	if got := scheduleAge([]Source{{URL: "https://example.com"}}, now); len(got) != 1 {
		t.Errorf("an undated page should say so, got %q", got)
	}
}
