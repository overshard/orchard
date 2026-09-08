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

// The three answers with wrong sums in them on 2026-09-08, and the answers from
// the same day that must not be sent back. calc was called zero times that day
// with the contract asking for it in as many words, which is why this is a
// check rather than a line in the prompt.
func TestCountsUpInProseCatchesTheDayItWasWrong(t *testing.T) {
	adds := []struct{ name, draft string }{
		{"a day total that contradicts its own earlier figure",
			"**Total calories for the day so far: 1,190 calories.**\n\nThat's the Bojangles total of 930 " +
				"calories plus the Jimmy Dean Sausage, Egg & Cheese Maple Biscuit Roll of 280 calories."},
		{"a run of numbers summed wrong",
			"Two tablespoons of rice is about 60, two of beans about 80, two of cheese about 40, two of " +
				"corn about 40, and two of cooked chicken about 40, with the tortilla 250. That's around " +
				"510 for the fillings and 250 for the wrap, so about 760 total."},
		{"a list with a stated total",
			"- 4-piece Supreme — 500 cal [1]\n- Mashed potatoes — 120 cal [1]\n- Biscuit — 310 cal [1]\n\n" +
				"**Bojangles total: 930 calories.**"},
	}
	for _, c := range adds {
		if !countsUpInProse(c.draft) {
			t.Errorf("%s went through uncounted", c.name)
		}
	}

	leaves := []struct{ name, draft string }{
		{"a comparison, which is not a sum",
			"The pinto beans have 7 g protein and the dirty rice has 5 g, so the beans win."},
		{"prose with numbers and no total claimed",
			"The RTX 5090 MSRP is $1,999 and the card is sitting at $5,799.99 right now, which is 190% above it."},
		{"a total inside a fence, which is code and not a claim",
			"Here is the query:\n\n```sql\nSELECT total FROM t WHERE a=1 AND b=2 AND c=3;\n```\n\nRun that."},
		{"no numbers at all",
			"That's the total picture, and nothing in it is surprising."},
	}
	for _, c := range leaves {
		if countsUpInProse(c.draft) {
			t.Errorf("%s was sent back", c.name)
		}
	}
}
