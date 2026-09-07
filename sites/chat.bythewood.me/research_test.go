package main

import "testing"

// The two that Isaac hit, verbatim in shape, plus the answers they must not be
// confused with. A detector that fires on a real answer is worse than one that
// misses a deferral, since a miss costs a bad turn and a false positive costs
// every turn that mentions searching.
func TestIsDeferral(t *testing.T) {
	defer_ := []string{
		"I don't have anything to report on major news over the weekend, but I can search for the latest headlines if you'd like.",
		"I do not have access to real time information. Would you like me to look that up?",
		"Let me search for that and get back to you.",
		"I'll check the current price for you.",
		"My training data only goes up to early 2025, so I cannot say what happened last weekend.",
		"Shall I look up the schedule?",
		"I can look that up for you.",
		"Do you want me to fetch the page?",
		"I'm going to search for recent coverage of this.",
		"I don't have any details about that. Just let me know and I'll dig into it.",
	}
	for _, s := range defer_ {
		if !isDeferral(s) {
			t.Errorf("missed a deferral: %q", s)
		}
	}

	answers := []string{
		"The S&P closed at 6,412, up 0.4% on the day. https://example.com/quote",
		"You can search their site for the part number, it is under Support.",
		"Use ripgrep here, since grep -r will walk node_modules and take a minute.",
		"gofmt found nothing. The build passes and the three test packages are green.",
		"", // nothing at all is not a deferral, it is an empty turn
		"Saturday is 61F and dry, Sunday drops to 44F with rain after noon, so pack a shell and a warm layer for the night. https://api.weather.gov/x",
		// A long answer that offers more at the end has still answered.
		"The backup container was not running cron because the image dropped to the dev user and crond needs root, which is why the nightly restic snapshot stopped on the 14th and nothing alerted. " +
			"Switching the container back to root and keeping the restic call itself under the dev user fixes it without giving the backup script more than it needs. " +
			"The tag in the snapshot list will still say the old host until the next run, so the gap in the history is real and not a display problem. " +
			"You will want to run one by hand to close it. After that the schedule takes over again and there is nothing else to change. " +
			"If you want I can check whether the other two containers have the same problem.",
	}
	for _, s := range answers {
		if isDeferral(s) {
			t.Errorf("ate a real answer: %q", s)
		}
	}
}

func TestResearchNudge(t *testing.T) {
	if got := researchNudge(""); got == "" {
		t.Fatal("an empty query still has to produce a nudge")
	}
	got := researchNudge(`major news september 6 2026`)
	if want := `"major news september 6 2026"`; !contains(got, want) {
		t.Errorf("nudge %q does not carry the query %s", got, want)
	}
	// A query carrying a quote must not break out of the one around it.
	if contains(researchNudge(`say "hi"`), `""`) {
		t.Error("inner quotes were not stripped")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
