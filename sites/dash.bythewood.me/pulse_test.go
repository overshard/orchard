package main

import (
	"strings"
	"testing"
	"time"
)

func TestFeedPulse(t *testing.T) {
	quiet := []feedItem{{title: "DuckDB Ducklake", points: 196, comments: 23, age: 40 * time.Hour}}
	if p := feedPulse(quiet, hnScale, hnTheme); p.Level != "quiet" || p.Text != "Nothing taking off." {
		t.Errorf("quiet: %+v", p)
	}

	busy := []feedItem{
		{title: "OpenAI withdraws three mathematical results", points: 337, comments: 584, age: 30 * time.Hour},
		{title: "Whistle: Speech to Text in 16.9 MB", points: 814, comments: 164, age: 20 * time.Hour},
	}
	if p := feedPulse(busy, hnScale, hnTheme); p.Level != "busy" || p.Text != "No. 2 has the argument, 584 comments." {
		t.Errorf("busy: %+v", p)
	}

	// Five hundred points in four hours is taking off whatever the total.
	fresh := append(busy, feedItem{title: "Claude outage", points: 520, comments: 30, age: 4 * time.Hour})
	if p := feedPulse(fresh, hnScale, hnTheme); p.Level != "hot" || !strings.HasPrefix(p.Text, "No. 2 hit 520 points in 4 hours.") {
		t.Errorf("hot: %+v", p)
	}
}

// The biggest thread is the top row already, so the line doesn't repeat it.
func TestYesterdaysBigThreadIsNotBlowingUp(t *testing.T) {
	old := []feedItem{{title: "DeepSeek 4.1 Flash", points: 865, comments: 774, age: 36 * time.Hour}}
	if p := feedPulse(old, hnScale, hnTheme); p.Level != "big" || p.Text != "" {
		t.Errorf("%+v", p)
	}
}

func TestThemes(t *testing.T) {
	var hn []feedItem
	for _, title := range []string{"Claude Code tips", "Why DeepSeek 4.1 matters", "An LLM in 16MB", "OpenAI withdraws results", "Agents are hard", "Beauty in DVD menus"} {
		hn = append(hn, feedItem{title: title})
	}
	if name, n := hnTheme(hn); name != "AI" || n != 5 {
		t.Errorf("hn theme %s %d", name, n)
	}

	lb := []feedItem{{tags: []string{"rust"}}, {tags: []string{"rust", "release"}}, {tags: []string{"vibecoding"}}}
	if name, n := lobstersTheme(lb); name != "Rust" || n != 2 {
		t.Errorf("lobsters theme %s %d", name, n)
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
