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
		{title: "Whistle: Speech to Text in 16.9 MB", points: 814, comments: 164, age: 20 * time.Hour},
		{title: "OpenAI withdraws three mathematical results", points: 337, comments: 584, age: 30 * time.Hour},
	}
	if p := feedPulse(busy, hnScale, hnTheme); p.Level != "busy" || !strings.Contains(p.Text, "OpenAI withdraws") {
		t.Errorf("busy: %+v", p)
	}

	// Five hundred points in four hours is taking off whatever the total.
	fresh := append(busy, feedItem{title: "Claude outage", points: 520, comments: 300, age: 4 * time.Hour})
	if p := feedPulse(fresh, hnScale, hnTheme); p.Level != "hot" || !strings.HasPrefix(p.Text, "“Claude outage”, 520 points") {
		t.Errorf("hot: %+v", p)
	}
}

func TestYesterdaysBigThreadIsNotBlowingUp(t *testing.T) {
	old := []feedItem{{title: "DeepSeek 4.1 Flash", points: 865, comments: 774, age: 36 * time.Hour}}
	if p := feedPulse(old, hnScale, hnTheme); p.Level != "big" {
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
