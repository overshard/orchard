package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func event(titles ...string) cluster {
	var c cluster
	for i, t := range titles {
		c.stories = append(c.stories, story{title: t, url: "https://news.test/" + t, source: []string{"AP", "NPR", "FOX"}[i%3], words: significant(t)})
	}
	return c
}

func TestUnsupported(t *testing.T) {
	source := "Lula wins fourth term in Brazil's runoff\nBrazilian voters gave Luiz Inacio Lula da Silva 51.2% of the vote.\nThree people were hurt."
	cases := []struct {
		line string
		want []string
	}{
		{"Lula won a fourth term with 51.2% of the vote in Brazil.", nil},
		{"Brazilian voters returned Lula, and three were hurt.", nil},
		{"Pres. Lula won as 3 were hurt.", nil},
		{"Bolsonaro conceded to Lula in Brazil.", []string{"Bolsonaro"}},
		{"Lula won with 54% of the vote.", []string{"54"}},
		{"Lula won on Tuesday.", []string{"Tuesday"}},
	}
	for _, c := range cases {
		got := unsupported(c.line, source)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%q: got %v, want %v", c.line, got, c.want)
		}
	}
}

func TestStripParties(t *testing.T) {
	source := "Sen. Jane Doe (R-Texas) said the bill would pass."
	if got := stripParties("Jeanine Pirro (R-Ind.) and Sen. Jane Doe (R-Texas) spoke.", source); got != "Jeanine Pirro and Sen. Jane Doe (R-Texas) spoke." {
		t.Errorf("%q", got)
	}
}

func TestNamesWeekday(t *testing.T) {
	if got := namesWeekday("Tuesday's Fed decision looms."); got != "tuesday" {
		t.Errorf("%q", got)
	}
	if got := namesWeekday("Fridays are slow, and futures are flat."); got != "friday" {
		t.Errorf("%q", got)
	}
	if got := namesWeekday("Futures are flat ahead of the Fed."); got != "" {
		t.Errorf("%q", got)
	}
}

// The model writes one event's line into the next one's field, which put one
// story's link under another story's sentence.
func TestReadProblems(t *testing.T) {
	events := []cluster{event("Lula wins fourth term in Brazil"), event("Comey indicted on new charges")}
	bad := readProblems(events, map[string]string{"event1": "Comey was indicted again.", "event2": "Lula won another term."})
	if !strings.Contains(bad[1], "isn't about event 1") || !strings.Contains(bad[2], "isn't about event 2") {
		t.Errorf("swapped fields: %v", bad)
	}
	bad = readProblems(events, map[string]string{"event1": "Lula won as Bolsonaro conceded in Brazil.", "event2": "Comey faced new charges."})
	if !strings.Contains(bad[1], "Bolsonaro") || bad[2] != "" {
		t.Errorf("a name from memory: %v", bad)
	}
	if bad = readProblems(events, map[string]string{"event1": "Lula won a fourth term.", "event2": "Comey faced new charges."}); len(bad) != 0 {
		t.Errorf("a good read flagged: %v", bad)
	}
}

func TestPointProblemsAndRelated(t *testing.T) {
	events := []cluster{event("Fed holds rates steady"), event("Anthropic files for IPO")}
	out := modelPoints{Points: []modelPoint{
		{Text: "The Fed held rates steady.", Sources: []int{1, 2}},
		{Text: "Anthropic filed for an IPO.", Sources: []int{1}},
		{Text: "Anthropic filed for a $40 billion IPO.", Sources: []int{2}},
	}}
	bad := pointProblems(events, out)
	if _, ok := bad[0]; ok {
		t.Errorf("a good point flagged: %v", bad)
	}
	if !strings.Contains(bad[1], "isn't about the events it cites") || !strings.Contains(bad[2], "40") {
		t.Errorf("%v", bad)
	}
	if got := related(out.Points[0].Text, events, out.Points[0].Sources); len(got) != 1 || got[0] != 1 {
		t.Errorf("the Fed point should only link the Fed event: %v", got)
	}
}

// Go writes the days, since the model names the wrong one.
func TestCalendarClause(t *testing.T) {
	et := easternTime()
	releases := []release{
		{at: time.Date(2026, 10, 29, 8, 30, 0, 0, et), short: "GDP"},
		{at: time.Date(2026, 10, 30, 8, 30, 0, 0, et), short: "PCE"},
	}
	week := marketLine{label: weekAhead, forward: true, week: true, day: time.Date(2026, 10, 26, 0, 0, 0, 0, et)}
	if got := calendarClause(week, releases, inNY(t, "2026-10-25 09:00")); got != "WED FED 2PM · THU GDP 8:30AM · FRI PCE 8:30AM" {
		t.Errorf("week: %q", got)
	}
	day := marketLine{label: "TODAY", forward: true, day: time.Date(2026, 10, 28, 0, 0, 0, 0, et)}
	if got := calendarClause(day, releases, inNY(t, "2026-10-28 15:00")); got != "FED OUT" {
		t.Errorf("after the decision: %q", got)
	}
	quiet := marketLine{label: "TOMORROW", forward: true, day: time.Date(2026, 10, 27, 0, 0, 0, 0, et)}
	if got := calendarClause(quiet, releases, inNY(t, "2026-10-26 09:00")); got != "NOTHING MAJOR SCHEDULED" {
		t.Errorf("quiet day: %q", got)
	}
	if got := impliedMove(18.39, false); got != "±1.16%" {
		t.Errorf("day: %q", got)
	}
	if got := impliedMove(18.39, true); got != "±2.55%" {
		t.Errorf("week: %q", got)
	}
}

func TestMarketsWeekendSlots(t *testing.T) {
	cases := []struct{ now, desk, kind, at string }{
		{"2026-10-24 12:00", "markets", "morning", "2026-10-24 07:00"},
		{"2026-10-25 17:00", "markets", "morning", "2026-10-24 07:00"},
		{"2026-10-25 18:20", "markets", "futures", "2026-10-25 18:15"},
		{"2026-10-26 06:00", "markets", "futures", "2026-10-25 18:15"},
		{"2026-10-25 17:00", "news", "close", "2026-10-25 16:05"},
		{"2026-10-24 06:00", "markets", "close", "2026-10-23 16:05"},
	}
	for _, c := range cases {
		slot, at := deskSlot(c.desk, inNY(t, c.now))
		if slot.kind != c.kind || !at.Equal(inNY(t, c.at)) {
			t.Errorf("%s %s: got %s at %s", c.desk, c.now, slot.kind, at)
		}
	}
	if got := nextSlot(inNY(t, "2026-10-25 17:00")); !got.Equal(inNY(t, "2026-10-25 18:15")) {
		t.Errorf("next after sunday's close: %s", got)
	}
}

func TestMarkAndTimeline(t *testing.T) {
	fed := Point{Text: "The Fed held rates steady.", Links: []Link{{Source: "AP", URL: "https://news.test/fed", Title: "Fed holds rates steady"}}}
	prev := Brief{Slot: inNY(t, "2026-10-28 09:00").Unix(), Points: []Point{fed}}
	c := event("Fed holds rates steady")
	c.stories[0].url, c.stories[0].at = "https://news.test/fed", inNY(t, "2026-10-28 08:00")

	if got := mark(prev, fed, []cluster{c}); got != "" {
		t.Errorf("same story, nothing new: %q", got)
	}
	late := story{title: "Fed keeps rates on hold", url: "https://news.test/fed2", source: "NPR", at: inNY(t, "2026-10-28 10:00")}
	c.stories = append(c.stories, late)
	if got := mark(prev, fed, []cluster{c}); got != "UPDATED" {
		t.Errorf("a new outlet since: %q", got)
	}
	ipo := Point{Text: "Anthropic filed for an IPO.", Links: []Link{{URL: "https://news.test/ipo", Title: "Anthropic files for IPO"}}}
	if got := mark(prev, ipo, nil); got != "NEW" {
		t.Errorf("a new story: %q", got)
	}
	if got := mark(Brief{}, ipo, nil); got != "" {
		t.Errorf("nothing to compare against: %q", got)
	}

	past := []Brief{
		{Slot: inNY(t, "2026-10-26 07:00").Unix(), Points: []Point{{Text: "Fed meeting nears.", Links: []Link{{URL: "https://news.test/fed", Title: "Fed holds rates steady"}}}}},
		{Slot: inNY(t, "2026-10-27 16:05").Unix(), Points: []Point{{Text: "Fed began its meeting.", Links: fed.Links}}},
		{Slot: inNY(t, "2026-10-27 07:00").Unix(), Points: []Point{ipo}},
		prev,
	}
	steps := timeline(past, fed, inNY(t, "2026-10-28 11:00"))
	if len(steps) != 2 || steps[0].Day != "MON OCT 26" || steps[1].Text != "Fed began its meeting." {
		t.Errorf("%+v", steps)
	}
}

func TestIntensity(t *testing.T) {
	past := []Brief{
		{Slot: inNY(t, "2026-10-27 07:00").Unix(), Read: &Read{Spread: "TOP STORY ON 4 OF 10 OUTLETS"}},
		{Slot: inNY(t, "2026-10-27 11:00").Unix(), Read: &Read{Spread: "TOP STORY ON 8 OF 10 OUTLETS"}},
		{Slot: inNY(t, "2026-10-28 07:00").Unix(), Read: &Read{Spread: "TOP STORY ON 5 OF 10 OUTLETS"}},
	}
	got := intensity(past, inNY(t, "2026-10-28 11:00"))
	if len(got) != intensityDays {
		t.Fatalf("%d days", len(got))
	}
	last, before := got[len(got)-1], got[len(got)-2]
	if last.Day != "WED OCT 28" || last.Share != 50 || before.Share != 80 || before.Level != "major" || got[0].Share != 0 {
		t.Errorf("%+v %+v %+v", got[0], before, last)
	}
}

func TestWeekReviewAndHeldWeek(t *testing.T) {
	link := []Link{{Source: "WSJ", URL: "https://wsj.test/1", Title: "Stocks rise on chip rally"}}
	past := []Brief{
		{Slot: inNY(t, "2026-10-20 16:05").Unix(), Points: []Point{{Label: "TODAY", Lean: "up", Move: "0.60%", Text: "Chipmakers rallied.", Links: link}}},
		{Slot: inNY(t, "2026-10-21 07:00").Unix(), Points: []Point{{Label: "YESTERDAY", Lean: "up", Move: "0.60%", Text: "Chips led.", Links: link}}},
		{Slot: inNY(t, "2026-10-23 11:00").Unix(), Points: []Point{{Label: "TODAY", Lean: "down", Move: "0.20% SO FAR", Text: "Oil weighed.", Links: link}}},
		{Slot: inNY(t, "2026-10-22 16:05").Unix(), Points: []Point{{Label: "TODAY", Lean: "down", Move: "0.30%"}}},
	}
	got := weekReview(past, inNY(t, "2026-10-19 00:00"))
	if len(got) != 1 || got[0].session.Weekday() != time.Tuesday && got[0].session.Weekday() != time.Monday {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[0].stories[0].desc, "Chips led.") || got[0].session.Format("2006-01-02") != "2026-10-20" {
		t.Errorf("the later telling of Monday should win: %+v", got[0])
	}

	sat := Brief{Slot: inNY(t, "2026-10-24 07:00").Unix(), Points: []Point{{Label: lastWeek, Lean: "up", Move: "1.10%", Text: "Chips led.", Links: link}}}
	if pt, ok := heldWeek(sat, inNY(t, "2026-10-25 18:15")); !ok || pt.Text != "Chips led." {
		t.Errorf("sunday should keep saturday's week: %+v", pt)
	}
	if _, ok := heldWeek(sat, inNY(t, "2026-10-31 07:00")); ok {
		t.Error("a week later kept last week's look back")
	}
}

// A read field about the wrong event gets a retry naming it, and one still
// wrong after is left out of the read and its links.
func TestReadDropsAFieldAboutAnotherEvent(t *testing.T) {
	events := []cluster{event("Lula wins fourth term in Brazil", "Lula wins Brazil runoff", "Brazil reelects Lula"), event("Comey indicted on new charges", "Comey faces charges")}
	swapped := `{"event1": "Lula won a fourth term.", "event2": "Lula won the runoff."}`
	m := &scriptedModel{answers: []string{swapped, swapped,
		`{"reason": "The reports say so.", "verdict": "supported"}`}}
	b := &Briefer{model: m}
	rated := map[string]eventRating{events[0].lead().url: {Impact: 3}, events[1].lead().url: {Impact: 3}}
	r, covered, err := b.read(context.Background(), briefSlots[2], inNY(t, "2026-10-26 11:00"), events, rated, 3, inNY(t, "2026-10-26 11:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.Text, " Lula won a fourth term.") || len(r.Links) != 1 || len(covered) != 1 || !strings.Contains(m.asks[1], "isn't about event 2") {
		t.Errorf("%+v %d asks %v", r, len(covered), m.asks)
	}
}
