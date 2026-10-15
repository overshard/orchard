package tools

import (
	"math"
	"testing"
)

func TestEvalExprArithmetic(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{
		{"275000 * 0.05", 13750},
		{"(1299 + 210) * 0.93", 1403.37},
		{"1200 + 16 + 270 + 134 + 40 + 30 + 70", 1760},
		{"7 % 3", 1},
		{"-4 + 10", 6},
	} {
		got, err := evalExpr(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}

// A real 30 year payment, which a model with no power to call guesses at and
// gets wrong by about fifty dollars a month.
func TestEvalExprMortgagePayment(t *testing.T) {
	const want = 1748.63
	got, err := evalExpr("261250 * (0.0706 / 12) / (1 - pow(1 + (0.0706 / 12), -360))")
	if err != nil {
		t.Fatalf("mortgage payment: %v", err)
	}
	if math.Abs(got-want) > 0.5 {
		t.Errorf("payment = %.2f, want about %.2f", got, want)
	}
}

// A model gets a percent of a value wrong by a factor of 100 and then by 12, and
// writes $16 for a monthly tax of $145.98.
func TestEvalExprPercentOfValue(t *testing.T) {
	year, err := evalExpr("275000 * 0.00637")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(year-1751.75) > 0.01 {
		t.Errorf("annual tax = %v, want 1751.75", year)
	}
	month, err := evalExpr("275000 * 0.00637 / 12")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(month-145.98) > 0.01 {
		t.Errorf("monthly tax = %v, want 145.98", month)
	}
}

func TestEvalExprFunctions(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{
		{"pow(2, 10)", 1024},
		{"pow(1.00588, -360)", 0.1211651},
		{"sqrt(144)", 12},
		{"abs(0 - 7.5)", 7.5},
		{"round(1748.63)", 1749},
		{"floor(1748.63)", 1748},
		{"ceil(1748.01)", 1749},
		{"min(3, 9)", 3},
		{"max(3, 9)", 9},
		{"round(275000 * 0.00637 / 12)", 146},
	} {
		got, err := evalExpr(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if math.Abs(got-c.want) > 1e-4 {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEvalExprRejects(t *testing.T) {
	for _, in := range []string{
		"1 / 0",
		"nope(2)",
		"pow(2)",
		"pow(2, 3, 4)",
		"sqrt(0 - 1)",
		"x + 1",
		"1 << 2",
	} {
		if v, err := evalExpr(in); err == nil {
			t.Errorf("%s was accepted as %v", in, v)
		}
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
