package tools

import "testing"

func TestADisambiguationPageIsMarked(t *testing.T) {
	for text, want := range map[string]bool{
		"Look up pareto in Wiktionary, the free dictionary. Pareto may refer to:": true,
		"AH and variants may refer to: Ah!, a song":                               true,
		"The Pareto principle states that for many outcomes roughly 80%":          false,
	} {
		if got := isDisambiguation(text); got != want {
			t.Errorf("isDisambiguation(%q) = %v, want %v", text, got, want)
		}
	}
}
