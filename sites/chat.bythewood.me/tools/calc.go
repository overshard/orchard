package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Letters and commas are for the function names in exprFuncs, and `=` and `;`
// for naming a part. evalExpr decides which names exist, and this only keeps the
// obvious rubbish away from the parser.
const exprChars = `[0-9a-z_\.\+\-\*/\(\)\s%,^;=]`

var safeExpr = regexp.MustCompile(`^` + exprChars + `+$`)

// The same class unanchored, so a rejection can name what it choked on.
var exprChar = regexp.MustCompile(exprChars)

// Long enough for a formula written out in named steps and short enough that
// writing one cannot use up a whole round's token budget.
const maxExprChars = 1000

type calcArgs struct {
	Expression string
}

func (a *calcArgs) params() []param {
	return []param{
		strArg(&a.Expression, "expression", "arithmetic, like (1299 + 210) * 0.93, or named steps like r = 0.05/12; 275000 * r / (1 - pow(1 + r, -360))").required(),
	}
}

var Calc = Define(Tool{
	Name:   "calc",
	Traits: Sums,
	Description: "Evaluate arithmetic. Use it rather than doing sums in your head, especially for totals and " +
		"budgets. Every total you are about to write down goes through here first, including the total row of a " +
		"table and any figure you call an average, and the number you print is the number it returned. Changing " +
		"one line of a table means totalling it again. pow, sqrt, abs, round, floor, ceil, min and max are " +
		"available. You can name a part before using it, one statement per line or separated by semicolons, and " +
		"the last line is the answer: `r = 0.05/12` then `275000 * r / (1 - pow(1 + r, -360))` gives the monthly " +
		"payment on a 275000 loan at 5% over 30 years, which is 1476.26. r is the monthly rate and n the number " +
		"of payments, so do not divide the result by 12 again. To total a long list of numbers, add them in " +
		"groups of about twenty and total the groups rather than writing every number into one call.",
}, func(ctx context.Context, d *Turn, a calcArgs) (any, error) {
	e := a.Expression
	if e == "" {
		return nil, fmt.Errorf("there is nothing to work out, pass the arithmetic as expression")
	}
	// A model totalling a bank statement writes every line into one expression
	// and runs out of tokens partway, so saying so beats evaluating whatever
	// survived, and the advice depends on whether it was summing or deriving.
	if len(e) > maxExprChars {
		if strings.Count(e, "+") > 20 {
			return nil, fmt.Errorf("that expression is too long at %d characters, add the numbers in groups of about twenty and total the groups", len(e))
		}
		return nil, fmt.Errorf("that expression is too long at %d characters, work it out in fewer steps or across more than one call", len(e))
	}
	// Naming the character gets it fixed on the next call, where a flat "only
	// arithmetic is supported" got the same thing written again.
	if !safeExpr.MatchString(e) {
		return nil, fmt.Errorf("calc cannot read %s in that expression. Numbers, + - * / %% ( ) and the "+
			"functions pow, sqrt, abs, round, floor, ceil, min and max are allowed, and a part can be named "+
			"with = on its own line", quoteChars(exprChar.ReplaceAllString(e, "")))
	}
	v, err := evalExpr(e)
	if err != nil {
		return nil, err
	}
	return map[string]any{"expression": e, "value": v}, nil
})

// quoteChars lists the offending characters once each, in the order they turned
// up, since a model told which character to drop fixes it on the next call.
func quoteChars(s string) string {
	var seen []string
	for _, r := range s {
		q := strconv.QuoteRune(r)
		if !slices.Contains(seen, q) {
			seen = append(seen, q)
		}
	}
	if len(seen) == 0 {
		return "that"
	}
	return strings.Join(seen, " and ")
}
