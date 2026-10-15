package tools

import (
	"context"
	"fmt"
	"strings"

	"chat.bythewood.me/property"
)

type mortgageArgs struct {
	Price       int
	LoanType    string
	DownPct     float64
	DownAmount  float64
	RatePct     float64
	TermYears   int
	County      string
	CreditScore int
	HOAMonthly  float64
	TaxAnnual   float64
}

func (a *mortgageArgs) params() []param {
	return []param{
		intArg(&a.Price, "price", "the purchase price in dollars").required(),
		strArg(&a.LoanType, "loan_type", "which programme, or all of them, which is the default and usually the useful answer").
			oneOf(append(append([]string{}, property.LoanTypes...), "all")...),
		numArg(&a.DownPct, "down_payment_pct", "percent down, if he said a percent"),
		numArg(&a.DownAmount, "down_payment", "dollars down, if he said an amount"),
		numArg(&a.RatePct, "rate_pct", "a rate he has been quoted, which beats the survey"),
		intArg(&a.TermYears, "term_years", "30 unless he said otherwise"),
		strArg(&a.County, "county", "the NC county, which decides the property tax rate"),
		intArg(&a.CreditScore, "credit_score", "his score, if he said one"),
		numArg(&a.HOAMonthly, "hoa_monthly", "monthly HOA dues"),
		numArg(&a.TaxAnnual, "tax_annual", "the annual property tax bill, if it is known"),
	}
}

var Mortgage = Define(Tool{
	Name: "mortgage",
	Description: "What a price costs a month under a conventional, FHA, VA or USDA loan, worked out at this week's " +
		"published rate, with the down payment, the upfront fee each government programme charges, the mortgage insurance " +
		"and how long it lasts, the cash to close and the debt to income. " +
		"Use it for a question about money with no particular house behind it, like what can we afford or what would FHA cost. " +
		"For a real address use the property tool with section cost instead, since that one knows the county's own tax rate " +
		"and whether USDA will lend on that spot.",
}, func(ctx context.Context, d *Deps, a mortgageArgs) (any, error) {
	if d.Property == nil {
		return nil, fmt.Errorf("the mortgage figures are not available in this turn")
	}
	price := a.Price
	if price <= 0 {
		return nil, fmt.Errorf("a price is needed to work a payment out")
	}

	quotes, market, err := d.Property.Quote(ctx, property.LoanInput{
		Price:       price,
		County:      a.County,
		TaxAnnual:   a.TaxAnnual,
		HOAMonthly:  a.HOAMonthly,
		DownPct:     a.DownPct,
		DownAmount:  a.DownAmount,
		RatePct:     a.RatePct,
		TermYears:   a.TermYears,
		CreditScore: a.CreditScore,
	})
	if err != nil {
		return nil, err
	}

	want := strings.ToLower(a.LoanType)
	// A report is the thing that knows how to write a quote for a model, so
	// the loose quotes are hung on one instead of formatted twice.
	rep := &property.Report{Price: price, County: a.County, Quotes: quotes, Market: market}
	if want != "" && want != "all" {
		var kept []property.Quote
		for _, q := range quotes {
			if q.Type == want {
				kept = append(kept, q)
			}
		}
		if len(kept) > 0 {
			rep.Quotes = kept
		}
	}

	out := rep.Cost()
	delete(out, "address")
	out["note"] = "no address, so the property tax is the county rate on the price and USDA's rural area test was not run"
	return out, nil
})
