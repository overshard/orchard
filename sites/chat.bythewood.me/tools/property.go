package tools

import (
	"context"
	"fmt"

	"chat.bythewood.me/property"
)

// House hunting works as a conversation since every question about a house is a
// follow up. The engine caches every lookup and the report, so only the first
// question about an address costs a minute of somebody else's servers.

// Property is an interface so a test can fake it, and a turn where it isn't
// wired must say so instead of panicking.
type Property interface {
	Lookup(ctx context.Context, address string, opt property.Options) (*property.Report, error)
	Quote(ctx context.Context, in property.LoanInput) ([]property.Quote, property.Market, error)
	Spend(ctx context.Context) (hourLeft, dayLeft int)
}

type propertyArgs struct {
	Address    string
	Section    string
	Price      int
	HOAMonthly float64
	TaxAnnual  float64
}

func (a *propertyArgs) params() []param {
	return []param{
		strArg(&a.Address, "address", "the street address, with the town and state if he gave them").required(),
		strArg(&a.Section, "section", "which part to return. summary first, then the one the question is about. "+
			"cost is every loan priced all in. flood is the FEMA zone and the water. road is what it fronts and how busy. "+
			"schools is the zoned schools and the drop off detour. commutes is how long every drive from the house takes and whose it is, "+
			"who lives there, and where the nursing work is, so any question naming a person belongs here. "+
			"area is the county census figures and reported crime. land is the lot, the slope and the assessed value. "+
			"neighbours is how much of the street is owner occupied. outings is what there is to do nearby. "+
			"industry is anything unpleasant to live near, like data centres, factories, landfills, quarries, power plants and Superfund sites. "+
			"links is the Redfin, Zillow, Realtor, map and street view pages, built from the address, so never search for those").
			oneOf(property.Aspects...),
		intArg(&a.Price, "price", "the asking price in dollars, whenever he has said one"),
		numArg(&a.HOAMonthly, "hoa_monthly", "monthly HOA dues, if the listing carried any"),
		numArg(&a.TaxAnnual, "tax_annual", "the annual property tax bill off the listing, which beats the county rate estimate"),
	}
}

var PropertyTool = Define(Tool{
	Name:   "property",
	Traits: Settles,
	Description: "Everything the public record knows about a house at an address: the FEMA flood zone and the water near it, " +
		"the road it fronts and how busy that road is, the schools it is zoned for, the drive to work with the school run in it, " +
		"the lot size and how flat it is, what the county has it assessed at, how much of the street is owner occupied, the county " +
		"census figures and reported crime, whether USDA will lend there, and what it costs a month all in under a conventional, " +
		"FHA or USDA loan. It comes back with a finished markdown table: print that table as it is and do not " +
		"retype the figures into prose, because the all-in number means nothing without the rows it is the sum of. " +
		"What it returns is the whole answer about the house, so never search the web to check it or to fill it out, " +
		"and never state a fact about the house it did not give you. " +
		"The page draws buttons under your answer for the other sections, so do not list them. " +
		"Call it whenever Isaac names an address or asks anything about a particular house, and keep calling it for every follow up " +
		"about that house. A question naming somebody in his family is about their drive, their work or their school from the house " +
		"being discussed, so it is this tool with section drives, never a search and never an encyclopedia lookup. " +
		"Pass the price whenever he has said one, because nothing about the money is worked out without it. " +
		"Ask for one section at a time rather than everything: the first call does the lookups and every call after it about the same " +
		"address is free, so a follow up costs nothing. " +
		"A new address costs about forty requests to other people's free servers and only a few are allowed an hour, so call it once " +
		"with the address as he wrote it. If it says the ceiling is spent, say so and stop, because spelling the street a different way " +
		"is a new address and spends another one. Read only, and it buys nothing and tells nobody.",
}, func(ctx context.Context, d *Turn, a propertyArgs) (any, error) {
	if d.Property == nil {
		return nil, fmt.Errorf("the property lookups are not available in this turn")
	}
	addr := a.Address
	if addr == "" {
		return nil, fmt.Errorf("an address is needed, the street and the town at least")
	}

	if d.Incognito {
		ctx = property.Incognito(ctx)
	}
	rep, err := d.Property.Lookup(ctx, addr, property.Options{
		Price:      a.Price,
		HOAMonthly: a.HOAMonthly,
		TaxAnnual:  a.TaxAnnual,
	})
	if err != nil {
		return nil, err
	}

	hourLeft, dayLeft := d.Property.Spend(ctx)

	out, ok := rep.Aspect(a.Section).(map[string]any)
	if !ok {
		return rep.Aspect(a.Section), nil
	}
	if !rep.Complete && len(rep.Missing) > 0 {
		out["note"] = "some of this is still being looked up. Say what is here and that the rest is coming, " +
			"and ask the same thing again in a minute rather than guessing at the gaps."
	}
	// The chips under the answer, instead of a list the model has to repeat and
	// can mangle, since sections nobody knows are there go unasked and a question
	// about a person in the house ends up at the encyclopedia.
	if asks := rep.Prompts(); len(asks) > 0 {
		d.Widgets.Add(Widget{Kind: "prompts", Label: rep.Address, Asks: asks})
	}
	out["other_sections"] = property.Aspects
	// What is left to spend, so a model deciding whether to look up a second
	// address can see the cost instead of finding out by being refused.
	if hourLeft <= 2 {
		out["new_addresses_left"] = fmt.Sprintf(
			"%d more this hour and %d today. A section of an address already looked up is free.",
			hourLeft, dayLeft)
	}
	return out, nil
})
