package main

import (
	"slices"
	"testing"

	"chat.bythewood.me/tools"
)

// The turn once matched these names itself, and every trait has to give the
// same answer for every tool that the name it replaced did.
var namedBefore = map[tools.Trait][]string{
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
}

func TestEveryToolsTraitsMatchTheNamesTheTurnChecked(t *testing.T) {
	for tr := tools.Trait(1); tr <= tools.Clock; tr <<= 1 {
		if _, ok := namedBefore[tr]; !ok {
			t.Errorf("trait %d has no pinned list of tools", tr)
		}
	}
	names := tools.Default().Names()
	for tr, want := range namedBefore {
		for _, n := range want {
			if !slices.Contains(names, n) {
				t.Errorf("%s is pinned to trait %d and is not a registered tool", n, tr)
			}
		}
		for _, n := range names {
			if got := tools.Of(n).Is(tr); got != slices.Contains(want, n) {
				t.Errorf("%s has trait %d %v, and the name check said %v", n, tr, got, !got)
			}
		}
	}
	cited := []string{"web_fetch", "web_search", "news"}
	for _, n := range names {
		if got := tools.Of(n).Cites != nil; got != slices.Contains(cited, n) {
			t.Errorf("%s cites pages %v, and the name check said %v", n, got, !got)
		}
	}
}
