package main

import (
	"testing"
	"time"

	"chat.bythewood.me/tools"
)

func TestADraftThatInventsAYearOrACastGoesBack(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	used := []tools.Result{{Name: "web_search", Content: map[string]any{"results": []any{
		map[string]any{"title": "Caveat (film) - Wikipedia",
			"snippet": "Caveat is a 2020 horror film written, directed, and edited by Damian Mc Carthy."},
	}}}}
	draft := "Your friend means the 1969 film Caveat, starring Richard Chamberlain as a young Shakespeare. " +
		"Laurence Olivier made it from Nevill Coghill's novel."
	got := unsupported(draft, "what does my friend mean by Caveat", nil, used, now)
	if len(got) == 0 || got[0] != "1969" {
		t.Fatalf("unsupported = %v, want 1969 first", got)
	}

	fine := "Caveat is a 2020 horror film by Damian Mc Carthy.[1] It is a strange pick for a wild card."
	if got := unsupported(fine, "what does my friend mean by Caveat", nil, used, now); len(got) != 0 {
		t.Errorf("a draft the results support went back over %v", got)
	}
}

func TestOneUnknownNameIsNotEnough(t *testing.T) {
	used := []tools.Result{{Name: "web_search", Content: map[string]any{"snippet": "Debian 13 trixie was released in August 2025."}}}
	draft := "Debian Trixie came out in 2025 and Forky is still in testing."
	if got := unsupported(draft, "when was trixie released", nil, used, time.Now()); len(got) != 0 {
		t.Errorf("went back over %v", got)
	}
}

func TestARemarkIsNotSentToResearch(t *testing.T) {
	prev := []string{"The CLI was compromised for about 93 minutes."}
	for q, want := range map[string]bool{
		"that's pretty major is it not?":                                           true,
		"334 devs is still massive in my opinion?":                                 true,
		"hmmmm tempting it's not that big really to have all that knowledge local": true,
		"150 GB doesn't seem like that much in the grand scheme of thins?":         true,
		"ah so trixie is still fairly new -- when is it's EOL?":                    false,
		"is the cli built in and you don't have to use the web control panel":      false,
		"can syncthing sync over tailscale?":                                       false,
	} {
		if got := isRemark(q, prev); got != want {
			t.Errorf("isRemark(%q) = %v, want %v", q, got, want)
		}
	}
	if isRemark("that's pretty major is it not?", nil) {
		t.Error("an opening message read as a remark on an answer")
	}
}

func TestAReplyToARemarkAddsNothingNew(t *testing.T) {
	prev := []string{"On April 22, 2026 a malicious @bitwarden/cli was up for about 93 minutes and 334 developers installed it."}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if !remarkAddsFacts("It is, since it ran 93 minutes and 33 seconds and hit 334 people.", "that's pretty major", prev, now) {
		t.Error("a new figure went through")
	}
	if remarkAddsFacts("It is, 334 developers is a lot for 93 minutes, but your vault was never touched.", "that's pretty major", prev, now) {
		t.Error("a reply using only what was said went back")
	}
}
