package skills

import "testing"

func TestCalculator(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"30*27", "810"},
		{"what is 30 * 27", "810"},
		{"30 times 27?", "810"},
		{"1,234 + 766", "2,000"},
		{"2^10", "1,024"},
		{"(3+4)*5", "35"},
		{"10/4", "2.5"},
		{"what's 15% of 200", "30"},
		{"100 - 250", "-150"},
		{"7 % 3", "1"},
		{"2^3^2", "512"}, // right associative
		{"-5 + 3", "-2"},
		{"1000000*1000", "1,000,000,000"},
	}
	for _, c := range ok {
		got, hit := TryCalculate(c.in)
		if !hit {
			t.Errorf("%q: not recognised as arithmetic", c.in)
			continue
		}
		if got.Pretty != c.want {
			t.Errorf("%q = %s, want %s", c.in, got.Pretty, c.want)
		}
	}

	// The important half: things that must go to the web instead. A calculator
	// that guesses at these is worse than no calculator.
	no := []string{
		"what is the deepest river in the US",
		"how do i make a breakfast burrito",
		"5 star hotels in paris",
		"42",              // a bare number is not a question
		"go 1.24 release", // version numbers are not arithmetic
		"3 +",
		"(3+4",
		"10/0",
		"what is sqlite wal mode",
		"top 10 go projects 2026",
	}
	for _, q := range no {
		if got, hit := TryCalculate(q); hit {
			t.Errorf("%q was answered as arithmetic: %s", q, got.Pretty)
		}
	}
}
