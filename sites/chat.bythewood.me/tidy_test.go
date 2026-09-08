package main

import "testing"

// Isaac's stored preference says no closing offer of more help, the contract
// repeats it and the final turn repeats it again. Four answers on 2026-09-08
// still ended on one.
func TestDropClosingOffer(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"a trailing offer on its own line",
			"Sure. Here's a small sample table.\n\n| a | b |\n\nWant it wider, with more rows, or styled differently?",
			"Sure. Here's a small sample table.\n\n| a | b |"},
		{"an offer at the end of a paragraph",
			"That's the whole requirement list. Want me to lay out the exact Gradle project?",
			"That's the whole requirement list."},
		{"a conditional offer",
			"So the honest shape of this tool is a fetch from X's public pages. If you want, I can write it for you.",
			"So the honest shape of this tool is a fetch from X's public pages."},
	}
	for _, c := range cases {
		if got := dropClosingOffer(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}

	// A question the turn genuinely needs answered is not an offer, and neither
	// is a question that is the answer.
	keep := []string{
		"Which of the two branches did you mean?",
		"The trial lowered Lp(a) by 80%. So why did it fail? Because the events did not follow.",
		"- Want: a working APK\n- Have: a Gradle project",
	}
	for _, k := range keep {
		if got := dropClosingOffer(k); got != k {
			t.Errorf("a real question was cut:\n got %q\nwant %q", got, k)
		}
	}
}

// The model picked the shape up from the numbered sources and filled it with a
// name, which landed in an answer as a bracket that links to nothing.
func TestDropLabelMarks(t *testing.T) {
	in := "The S&P 500 is down 26.05 to 7692.55 [S&P 500]."
	if got, want := dropLabelMarks(in), "The S&P 500 is down 26.05 to 7692.55."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A real citation is a number and survives, and so does a markdown link and
	// anything inside a fence.
	keep := []string{
		"Novartis fell 12% on the trial result. [2]",
		"See [the BBC piece](https://bbc.co.uk/news/1) for the detail.",
		"```go\nrows[1] = \"x\"\n```",
		"Use `m[\"sections\"]` to get at them.",
		"- [x] done\n- [ ] not done",
	}
	for _, k := range keep {
		if got := dropLabelMarks(k); got != k {
			t.Errorf("something real was cut:\n got %q\nwant %q", got, k)
		}
	}
}

// The sidebar held "Biopharma Selloff" next to "nix config file sharing".
func TestSentenceCaseTitles(t *testing.T) {
	cases := map[string]string{
		"nix config file sharing": "Nix config file sharing",
		"Biopharma Selloff":       "Biopharma Selloff",
		"VXUS market":             "VXUS market",
		"iPhone battery life":     "iPhone battery life",
		"":                        "",
	}
	for in, want := range cases {
		if got := sentenceCase(in); got != want {
			t.Errorf("sentenceCase(%q) = %q, want %q", in, got, want)
		}
	}
}
