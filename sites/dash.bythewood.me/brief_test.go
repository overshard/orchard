package main

import (
	"context"
	"errors"
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
	cases := []struct{ now, kind, at string }{
		{"2026-10-07 06:59", "close", "2026-10-06 16:05"},
		{"2026-10-07 07:00", "morning", "2026-10-07 07:00"},
		{"2026-10-07 10:30", "morning", "2026-10-07 07:00"},
		{"2026-10-07 11:00", "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:04", "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:05", "close", "2026-10-07 16:05"},
		// Weekends run like any other day.
		{"2026-10-10 08:00", "morning", "2026-10-10 07:00"},
		{"2026-10-12 06:00", "close", "2026-10-11 16:05"},
	}
	for _, c := range cases {
		slot, at := latestSlot(inNY(t, c.now))
		if slot.kind != c.kind || !at.Equal(inNY(t, c.at)) {
			t.Errorf("%s: got %s at %s, want %s at %s", c.now, slot.kind, at.Format("2006-01-02 15:04"), c.kind, c.at)
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

type fakeGateway struct {
	gpu      GPUState
	gpuErr   error
	asked    int
	unloads  int
	compiled []string
}

func (f *fakeGateway) Structured(context.Context, string, string, map[string]any, int, any) error {
	return nil
}
func (f *fakeGateway) GPU(context.Context) (GPUState, error) { f.asked++; return f.gpu, f.gpuErr }
func (f *fakeGateway) Unload(context.Context) error          { f.unloads++; return nil }

func testBriefer(t *testing.T, f *fakeGateway, fail bool) *Briefer {
	t.Helper()
	b := NewBriefer(NewStore(NewHub()), nil, nil, t.TempDir())
	b.model = f
	b.compile = func(_ context.Context, desk string, slot briefSlot, at time.Time) (Brief, error) {
		f.compiled = append(f.compiled, desk)
		if fail {
			return Brief{}, errors.New("model fell over")
		}
		return Brief{Slot: at.Unix(), Title: slot.title(), Points: []Point{{Text: desk}}}, nil
	}
	return b
}

// Gaming on a Saturday: the 7am slot is owed, the card is busy, nothing loads,
// and the next ask is an hour later, not every minute.
func TestBriefDefersAnHourWhenTheCardIsBusy(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Busy: true, Reason: "card at 95%"}}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-10 07:00")

	for m := 0; m < 59; m++ {
		b.tick(context.Background(), now.Add(time.Duration(m)*time.Minute))
	}
	if f.asked != 1 || len(f.compiled) != 0 || f.unloads != 0 {
		t.Fatalf("in the first hour: asked %d, compiled %v, unloaded %d", f.asked, f.compiled, f.unloads)
	}
	if w := b.store.Snapshot().Briefs.News.Waiting; w != "CARD IN USE, NEXT TRY 08:00" {
		t.Errorf("waiting %q", w)
	}

	f.gpu = GPUState{Reason: "card idle"}
	b.tick(context.Background(), now.Add(time.Hour))
	if f.asked != 2 || strings.Join(f.compiled, ",") != "markets,news" || f.unloads != 1 {
		t.Fatalf("at 8am: asked %d, compiled %v, unloaded %d", f.asked, f.compiled, f.unloads)
	}
	if w := b.store.Snapshot().Briefs.News.Waiting; w != "" {
		t.Errorf("still waiting %q after a run", w)
	}

	// Done for this slot, so nothing more until 11.
	b.tick(context.Background(), now.Add(2*time.Hour))
	if f.asked != 2 {
		t.Errorf("asked again with nothing owed")
	}
}

// A model already on the card is somebody's chat, and pulling it would kill
// their turn, so the brief uses it and leaves it.
func TestBriefLeavesAResidentModelAlone(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Loaded: true}}
	b := testBriefer(t, f, false)
	b.tick(context.Background(), inNY(t, "2026-10-07 11:00"))
	if len(f.compiled) != 2 || f.unloads != 0 {
		t.Errorf("compiled %v, unloaded %d", f.compiled, f.unloads)
	}
}

// A failed run still unloads what it loaded, and tries again in a quarter hour.
func TestBriefUnloadsAndRetriesAfterAFailure(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, true)
	now := inNY(t, "2026-10-07 16:05")
	b.tick(context.Background(), now)
	if f.unloads != 1 {
		t.Errorf("unloaded %d after a failed run", f.unloads)
	}
	b.tick(context.Background(), now.Add(14*time.Minute))
	b.tick(context.Background(), now.Add(15*time.Minute))
	if f.asked != 2 {
		t.Errorf("asked %d times, want a retry at 15 minutes", f.asked)
	}
}

// The gateway being down is not the card being busy, and is asked again sooner.
func TestBriefRetriesWhenTheGatewayIsDown(t *testing.T) {
	f := &fakeGateway{gpuErr: errors.New("connection refused")}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-07 07:00")
	b.tick(context.Background(), now)
	b.tick(context.Background(), now.Add(15*time.Minute))
	if f.asked != 2 || len(f.compiled) != 0 {
		t.Errorf("asked %d, compiled %v", f.asked, f.compiled)
	}
}

func TestWidelyCarriedKeepsSinglesOnAQuietDay(t *testing.T) {
	two := cluster{stories: []story{{source: "AP"}, {source: "FOX"}}}
	one := cluster{stories: []story{{source: "NPR"}}}

	quiet := []cluster{two, one, one}
	if len(widelyCarried(quiet)) != 3 {
		t.Error("dropped singles with too few shared events to replace them")
	}

	var busy []cluster
	for i := 0; i < 12; i++ {
		busy = append(busy, two)
	}
	busy = append(busy, one)
	if got := widelyCarried(busy); len(got) != 12 {
		t.Errorf("kept %d, want the 12 shared", len(got))
	}
}

func TestKeepMajor(t *testing.T) {
	trial := cluster{stories: []story{{source: "AP", title: "Court denies stay"}, {source: "CBS"}}}
	clancy := cluster{stories: []story{{source: "AP"}, {source: "REUTERS"}, {source: "WSJ"}, {source: "HILL"},
		{source: "NPR"}, {source: "CBS"}, {source: "FOX"}}}
	fed := cluster{stories: []story{{source: "AP"}, {source: "WSJ"}}}

	got := keepMajor([]cluster{trial, clancy, fed}, []string{"crime_or_court", "crime_or_court", "economy"})
	if len(got) != 2 || len(got[0].outlets()) != 7 {
		t.Errorf("kept %d, want the trial everyone ran and the Fed", len(got))
	}
	if len(keepMajor([]cluster{trial, fed}, []string{"economy"})) != 2 {
		t.Error("a short label list should keep everything")
	}
}
