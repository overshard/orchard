package tools

import (
	"context"
	"strings"
	"testing"
)

func calcOK(t *testing.T, expr string) float64 {
	t.Helper()
	out, err := Calc.Run(context.Background(), nil, map[string]any{"expression": expr})
	if err != nil {
		t.Fatalf("calc(%q) = %v", expr, err)
	}
	return out.(map[string]any)["value"].(float64)
}

func calcErr(t *testing.T, expr string) string {
	t.Helper()
	_, err := Calc.Run(context.Background(), nil, map[string]any{"expression": expr})
	if err == nil {
		t.Fatalf("calc(%q) was accepted and should not have been", expr)
	}
	return err.Error()
}

func near(a, b float64) bool { return a-b < 0.01 && b-a < 0.01 }

// The shape a model reaches for when asked for a mortgage payment. Both
// spellings were refused before, which is how a housing budget came back with
// every figure invented.
func TestCalcTakesNamedParts(t *testing.T) {
	for _, expr := range []string{
		"r = 0.05/12; 275000 * r / (1 - pow(1 + r, -360))",
		"r = 0.05/12\n275000 * r / (1 - pow(1 + r, -360))",
		"r=0.05/12;p=275000;p*r/(1-pow(1+r,-360))",
	} {
		if got := calcOK(t, expr); !near(got, 1476.26) {
			t.Errorf("calc(%q) = %.2f, want 1476.26", expr, got)
		}
	}
}

func TestCalcCarriesNamesAcrossManyLines(t *testing.T) {
	expr := "r = 0.05/12\nloan = 275000\npi = loan * r / (1 - pow(1 + r, -360))\n" +
		"tax = 275000 * 0.006 / 12\npi + tax + 115 + 165 + 85 + 250 + 70"
	if got := calcOK(t, expr); !near(got, 2298.76) {
		t.Errorf("full budget = %.2f, want 2298.76", got)
	}
}

// The last statement is the answer, so an expression that only assigns has one
// too rather than coming back empty.
func TestTheLastStatementIsTheAnswer(t *testing.T) {
	if got := calcOK(t, "a = 2\nb = 3\na * b"); got != 6 {
		t.Errorf("= %v, want 6", got)
	}
	if got := calcOK(t, "a = 2 + 5"); got != 7 {
		t.Errorf("a bare assignment = %v, want 7", got)
	}
}

// A refusal a model cannot act on is what made it write the same call twice.
func TestARefusalNamesWhatToDoAboutIt(t *testing.T) {
	if e := calcErr(t, "nope * 2"); !strings.Contains(e, "nope = ") {
		t.Errorf("an unset name did not say how to set it: %s", e)
	}
	if e := calcErr(t, "2 $ 2"); !strings.Contains(e, `'$'`) {
		t.Errorf("a bad character was not named: %s", e)
	}
	if e := calcErr(t, "2^10"); !strings.Contains(e, "pow(") {
		t.Errorf("^ did not point at pow: %s", e)
	}
	if e := calcErr(t, "  "); !strings.Contains(e, "nothing to work out") {
		t.Errorf("an empty expression: %s", e)
	}
}

// Functions were always supported and the refusal said they were not, which is
// the sentence that sent it looking for a different tool.
func TestTheRefusalNoLongerDeniesFunctions(t *testing.T) {
	e := calcErr(t, "2 $ 2")
	if strings.Contains(e, "no names or functions") {
		t.Errorf("still claiming functions are unsupported: %s", e)
	}
	for _, name := range []string{"pow", "sqrt", "min", "max"} {
		if !strings.Contains(e, name) {
			t.Errorf("%s is available and the refusal does not say so: %s", name, e)
		}
	}
}

// Which advice helps depends on what was too long, and a mortgage written in
// steps is not a column of numbers to group.
func TestTooLongSaysSomethingUseful(t *testing.T) {
	sum := strings.Repeat("1234 + ", 150) + "1"
	if e := calcErr(t, sum); !strings.Contains(e, "groups of about twenty") {
		t.Errorf("a long sum: %s", e)
	}
	derive := "x = " + strings.Repeat("1234 * 1.05 / ", 90) + "1"
	e := calcErr(t, derive)
	if strings.Contains(e, "groups of about twenty") {
		t.Errorf("a long derivation got sum advice: %s", e)
	}
	if !strings.Contains(e, "too long") {
		t.Errorf("a long derivation: %s", e)
	}
}

// 683 characters of named steps is a real mortgage program and was refused.
func TestARealProgramFitsTheLimit(t *testing.T) {
	expr := "price = 275000\nloan_usda = 275000\nloan_fha = 275000 * (1 - 0.035)\n" +
		"r5 = 0.05 / 12\nr6 = 0.06 / 12\n" +
		"pi_usda = loan_usda * r5 / (1 - pow(1 + r5, -360))\n" +
		"pi_fha = loan_fha * r6 / (1 - pow(1 + r6, -360))\n" +
		"tax = price * 0.006 / 12\npi_usda + tax + 115 + 165 + 85 + 250 + 70"
	if len(expr) > maxExprChars {
		t.Fatalf("the limit is %d and a real program is %d", maxExprChars, len(expr))
	}
	if got := calcOK(t, expr); !near(got, 2298.76) {
		t.Errorf("= %.2f, want 2298.76", got)
	}
}
