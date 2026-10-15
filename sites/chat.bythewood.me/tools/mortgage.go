package tools

import (
	"context"
	"fmt"
	"strings"

	"chat.bythewood.me/property"
)

var Mortgage = Tool{
	Name: "mortgage",
	Description: "What a price costs a month under a conventional, FHA, VA or USDA loan, worked out at this week's " +
		"published rate, with the down payment, the upfront fee each government programme charges, the mortgage insurance " +
		"and how long it lasts, the cash to close and the debt to income. " +
		"Use it for a question about money with no particular house behind it, like what can we afford or what would FHA cost. " +
		"For a real address use the property tool with section cost instead, since that one knows the county's own tax rate " +
		"and whether USDA will lend on that spot.",
	Schema: obj(map[string]any{
		"price": integer("the purchase price in dollars"),
		"loan_type": map[string]any{"type": "string",
			"description": "which programme, or all of them, which is the default and usually the useful answer",
			"enum":        append(append([]string{}, property.LoanTypes...), "all")},
		"down_payment_pct": num("percent down, if he said a percent"),
		"down_payment":     num("dollars down, if he said an amount"),
		"rate_pct":         num("a rate he has been quoted, which beats the survey"),
		"term_years":       integer("30 unless he said otherwise"),
		"county":           str("the NC county, which decides the property tax rate"),
		"credit_score":     integer("his score, if he said one"),
		"hoa_monthly":      num("monthly HOA dues"),
		"tax_annual":       num("the annual property tax bill, if it is known"),
	}, "price"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		if d.Property == nil {
			return nil, fmt.Errorf("the mortgage figures are not available in this turn")
		}
		price := int(argNum(a, "price", 0))
		if price <= 0 {
			return nil, fmt.Errorf("a price is needed to work a payment out")
		}

		quotes, market, err := d.Property.Quote(ctx, property.LoanInput{
			Price:       price,
			County:      argStr(a, "county"),
			TaxAnnual:   argNum(a, "tax_annual", 0),
			HOAMonthly:  argNum(a, "hoa_monthly", 0),
			DownPct:     argNum(a, "down_payment_pct", 0),
			DownAmount:  argNum(a, "down_payment", 0),
			RatePct:     argNum(a, "rate_pct", 0),
			TermYears:   int(argNum(a, "term_years", 0)),
			CreditScore: int(argNum(a, "credit_score", 0)),
		})
		if err != nil {
			return nil, err
		}

		want := strings.ToLower(argStr(a, "loan_type"))
		// A report is the thing that knows how to write a quote for a model, so
		// the loose quotes are hung on one instead of formatted twice.
		rep := &property.Report{Price: price, County: argStr(a, "county"), Quotes: quotes, Market: market}
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
	},
}
