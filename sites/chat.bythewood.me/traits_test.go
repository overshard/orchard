package main

import (
	"slices"
	"testing"

	"chat.bythewood.me/tools"
)

// Each trait is a set of tools the turn treats alike, pinned here so a trait
// given to the wrong tool, or left off the right one, fails by name.
var traitTools = map[tools.Trait][]string{
	tools.Untrusted: {"web_search", "web_fetch", "x_search", "news"},
	tools.Private: {"chat_history", "remember", "orchard_logs", "orchard_status",
		"orchard_analytics", "orchard_repos", "orchard_code", "orchard_dash"},
	tools.Estate: {"orchard_logs", "orchard_status", "orchard_analytics",
		"orchard_repos", "orchard_code", "orchard_dash"},
	tools.OpenWeb: {"web_search", "web_fetch"},
	tools.Settles: {"property"},
	tools.Once:    {"news"},
	tools.Rundown: {"news"},
	tools.Notes:   {"remember"},
	tools.Lookup:  {"wikipedia", "web_search"},
	tools.Draws:   {"image"},
	tools.Tickers: {"markets"},
	tools.Sums:    {"calc"},
	tools.Clock:   {"now"},
	tools.Outbound: {"web_search", "web_fetch", "x_search", "markets", "property",
		"dictionary", "music_lookup", "odds", "weather"},
}

func TestEveryToolHasExactlyItsTraits(t *testing.T) {
	for tr := tools.Trait(1); tr <= tools.Outbound; tr <<= 1 {
		if _, ok := traitTools[tr]; !ok {
			t.Errorf("trait %d has no pinned list of tools", tr)
		}
	}
	names := tools.Default().Names()
	for tr, want := range traitTools {
		for _, n := range want {
			if !slices.Contains(names, n) {
				t.Errorf("%s is pinned to trait %d and is not a registered tool", n, tr)
			}
		}
		for _, n := range names {
			if got := tools.Of(n).Is(tr); got != slices.Contains(want, n) {
				t.Errorf("%s has trait %d %v, want %v", n, tr, got, !got)
			}
		}
	}
	cited := []string{"web_fetch", "web_search", "news"}
	for _, n := range names {
		if got := tools.Of(n).Cites != nil; got != slices.Contains(cited, n) {
			t.Errorf("%s cites pages %v, want %v", n, got, !got)
		}
	}
}
