package main

import "testing"

func TestTidyCitations(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"- Assemble by placing cheese [14] on the tortillas [14], adding bacon [14] and eggs [14] [14].",
			"- Assemble by placing cheese on the tortillas, adding bacon and eggs [14].",
		},
		{ // two different passages on one line are real evidence and stay put
			"The bridge opened in 1937 [2] and carries six lanes [5].",
			"The bridge opened in 1937 [2] and carries six lanes [5].",
		},
		{ // one citation is already right
			"Beat the eggs with a pinch of salt [3].",
			"Beat the eggs with a pinch of salt [3].",
		},
		{ // a repeat plus a distinct one keeps both, once each
			"Cook the sausage [7] and season it [7], then rest it [9].",
			"Cook the sausage and season it, then rest it [7][9].",
		},
	}
	for _, c := range cases {
		if got := tidyCitations(c.in); got != c.want {
			t.Errorf("\n in   %s\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}
