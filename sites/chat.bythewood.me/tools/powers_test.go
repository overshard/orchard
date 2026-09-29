package tools

import (
	"math"
	"testing"
)

func TestACaretIsAPower(t *testing.T) {
	for expr, want := range map[string]float64{
		"2^10":                   1024,
		"2 * 3 ^ 2":              18,
		"2^3^2":                  512,
		"12741808764 / 1024^3":   12741808764 / math.Pow(1024, 3),
		"(1 + 0.05/12)^(360)":    math.Pow(1+0.05/12, 360),
		"r = 0.004; (1 + r)^-12": math.Pow(1.004, -12),
		"2**8":                   256,
		"sqrt(16)^2":             16,
	} {
		got, err := evalExpr(expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
}
