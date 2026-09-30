package tools

import "testing"

func TestAppendingToAListKeepsItASentence(t *testing.T) {
	fact := "Isaac's friends movie night watch list: Heretic, Terrifier and Clayface."
	oxford := "Isaac's friends movie night watch list: Heretic, Terrifier, and Clayface."
	for items, want := range map[string]string{
		"The Long Walk":                    "Isaac's friends movie night watch list: Heretic, Terrifier, Clayface, and The Long Walk.",
		"Weapons, Clown in a Cornfield":    "Isaac's friends movie night watch list: Heretic, Terrifier, Clayface, Weapons, and Clown in a Cornfield.",
		"Weapons and Clown in a Cornfield": "Isaac's friends movie night watch list: Heretic, Terrifier, Clayface, Weapons, and Clown in a Cornfield.",
	} {
		if got := appendItems(fact, items); got != want {
			t.Errorf("appendItems(%q) = %q, want %q", items, got, want)
		}
		if got := appendItems(oxford, items); got != want {
			t.Errorf("appendItems on the Oxford comma list (%q) = %q, want %q", items, got, want)
		}
	}
}
