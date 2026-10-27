package main

import (
	"testing"
	"time"
)

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
