package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFeedPulse(t *testing.T) {
	quiet := []feedItem{{title: "DuckDB Ducklake", points: 196, comments: 23, age: 40 * time.Hour}}
	if p := feedPulse(quiet, hnScale); p.Level != "quiet" || p.Text != "" {
		t.Errorf("quiet: %+v", p)
	}

	busy := []feedItem{
		{title: "OpenAI withdraws three mathematical results", points: 337, comments: 584, age: 30 * time.Hour},
		{title: "Whistle: Speech to Text in 16.9 MB", points: 814, comments: 164, age: 20 * time.Hour},
	}
	if p := feedPulse(busy, hnScale); p.Level != "busy" || p.Text != "" {
		t.Errorf("busy: %+v", p)
	}

	// Five hundred points in four hours is taking off whatever the total.
	fresh := append(busy, feedItem{title: "Claude outage", points: 520, comments: 30, age: 4 * time.Hour})
	if p := feedPulse(fresh, hnScale); p.Level != "hot" || !strings.HasPrefix(p.Text, "No. 2 hit 520 points in 4 hours.") {
		t.Errorf("hot: %+v", p)
	}
}

// The biggest thread is the top row already, so the line doesn't repeat it.
func TestYesterdaysBigThreadIsNotBlowingUp(t *testing.T) {
	old := []feedItem{{title: "DeepSeek 4.1 Flash", points: 865, comments: 774, age: 36 * time.Hour}}
	if p := feedPulse(old, hnScale); p.Level != "big" || p.Text != "" {
		t.Errorf("%+v", p)
	}
}

func TestAttachFeedReads(t *testing.T) {
	st := State{HNPulse: Pulse{Label: "BUSY"}}
	st.Briefs.Feeds.Points = []Point{{Label: "HN", Text: "Commenters doubt the benchmark."}}
	st.attachFeedReads()
	if st.HNPulse.Read != "Commenters doubt the benchmark." || st.LobstersPulse.Read != "" {
		t.Errorf("%+v %+v", st.HNPulse, st.LobstersPulse)
	}
}

type scriptedModel struct {
	answers []string
	asks    []string
}

func (m *scriptedModel) Structured(_ context.Context, _, user string, _ map[string]any, _ int, out any) error {
	m.asks = append(m.asks, user)
	a := m.answers[0]
	m.answers = m.answers[1:]
	return json.Unmarshal([]byte(a), out)
}
func (m *scriptedModel) GPU(context.Context) (GPUState, error) { return GPUState{}, nil }
func (m *scriptedModel) Unload(context.Context) error          { return nil }

// A read run into the cap as one sentence with its only comma too early to cut
// at, which would leave the panel with no read at all.
func TestFeedReadRetriesACutRead(t *testing.T) {
	cut := "The Hacker News front page is a mix of niche software projects, infrastructure incidents, and a science study, with commenters debating the practical limits of code-as-math tools and warning about the hidden costs of self-hosting on free AI"
	m := &scriptedModel{answers: []string{
		`{"read": "` + cut + `", "sources": [1]}`,
		`{"read": "On self-hosting, commenters warn about hidden costs.", "sources": [1]}`,
	}}
	b := &Briefer{model: m}
	ts := []thread{{title: "Self-hosting on free AI", discuss: "https://news.ycombinator.com/item?id=1"}}
	pt, err := b.feedRead(context.Background(), "Hacker News", ts)
	if err != nil || pt.Text != "On self-hosting, commenters warn about hidden costs." || len(m.asks) != 2 {
		t.Fatalf("%+v %v asks=%d", pt, err, len(m.asks))
	}
	if !strings.Contains(m.asks[1], "cut off") {
		t.Errorf("retry didn't name the problem: %q", m.asks[1])
	}
}

func TestCommentText(t *testing.T) {
	if got := commentText("I doubt it.<p>The &quot;benchmark&quot; is <i>cherry</i> picked."); got != "I doubt it. The \"benchmark\" is cherry picked." {
		t.Errorf("%q", got)
	}
}

func TestReadPicksSkipWhatChangesNothing(t *testing.T) {
	mk := func(title string, n int) cluster {
		var c cluster
		for _, o := range []string{"AP", "REUTERS", "WSJ", "HILL", "BBC", "NPR", "CBS", "FOX"}[:n] {
			c.stories = append(c.stories, story{source: o, title: title, url: title})
		}
		return c
	}
	hurricane, nobel, strike, solo := mk("Hurricane", 6), mk("Nobel", 5), mk("Strike", 4), mk("Royal wedding", 8)
	rated := map[string]eventRating{"Hurricane": {Impact: 4}, "Nobel": {Impact: 1}, "Strike": {Impact: 3}, "Royal wedding": {Impact: 1}}
	var got []string
	for _, c := range readPicks([]cluster{solo, hurricane, nobel, strike}, rated, 3) {
		got = append(got, c.lead().title)
	}
	if strings.Join(got, ",") != "Royal wedding,Hurricane,Strike" {
		t.Errorf("got %v", got)
	}
}
