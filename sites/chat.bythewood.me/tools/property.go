package tools

import (
	"context"
	"fmt"
	"strings"

	"chat.bythewood.me/property"
)

// House hunting, which used to be a dashboard of its own and is better as a
// conversation. The dashboard could show everything at once and could not answer
// a question, and every question about a house is a follow up: what about the
// flood zone, what if we put 10% down, what would USDA cost instead.
//
// The engine behind this caches every lookup and the assembled report, so the
// first question about an address costs a minute of somebody else's servers and
// every question after it is free.

// Property is the lookup engine, an interface for the same reason Memory is: a
// test wants a fake, and a turn where it is not wired must say so rather than
// panic.
type Property interface {
	Lookup(ctx context.Context, address string, opt property.Options) (*property.Report, error)
	Quote(ctx context.Context, in property.LoanInput) ([]property.Quote, property.Market, error)
}

var PropertyTool = Tool{
	Name: "property",
	Description: "Everything the public record knows about a house at an address: the FEMA flood zone and the water near it, " +
		"the road it fronts and how busy that road is, the schools it is zoned for, the drive to work with the school run in it, " +
		"the lot size and how flat it is, what the county has it assessed at, how much of the street is owner occupied, the county " +
		"census figures and reported crime, whether USDA will lend there, and what it costs a month all in under a conventional, " +
		"FHA, VA or USDA loan. " +
		"Call it whenever Isaac names an address or asks anything about a particular house. Pass the price whenever he has said one, " +
		"because nothing about the money is worked out without it. " +
		"Ask for one section at a time rather than everything: the first call does the lookups and every call after it about the same " +
		"address is free, so a follow up costs nothing. Read only, and it buys nothing and tells nobody.",
	Schema: obj(map[string]any{
		"address": str("the street address, with the town and state if he gave them"),
		"section": map[string]any{"type": "string",
			"description": "which part to return. summary first, then the one the question is about",
			"enum":        property.Aspects},
		"price":       integer("the asking price in dollars, whenever he has said one"),
		"hoa_monthly": num("monthly HOA dues, if the listing carried any"),
		"tax_annual":  num("the annual property tax bill off the listing, which beats the county rate estimate"),
	}, "address"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		if d.Property == nil {
			return nil, fmt.Errorf("the property lookups are not available in this turn")
		}
		addr := argStr(a, "address")
		if addr == "" {
			return nil, fmt.Errorf("an address is needed, the street and the town at least")
		}

		rep, err := d.Property.Lookup(ctx, addr, property.Options{
			Price:      int(argNum(a, "price", 0)),
			HOAMonthly: argNum(a, "hoa_monthly", 0),
			TaxAnnual:  argNum(a, "tax_annual", 0),
		})
		if err != nil {
			return nil, err
		}

		out, ok := rep.Aspect(argStr(a, "section")).(map[string]any)
		if !ok {
			return rep.Aspect(argStr(a, "section")), nil
		}
		if !rep.Complete && len(rep.Missing) > 0 {
			out["note"] = "some of this is still being looked up. Say what is here and that the rest is coming, " +
				"and ask the same thing again in a minute rather than guessing at the gaps."
		}
		out["other_sections"] = property.Aspects
		return out, nil
	},
}

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
		// the loose quotes are hung on one rather than formatted twice.
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
