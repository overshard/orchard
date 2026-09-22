package property

import "fmt"

// What else there is to ask about this house.
//
// A report holds eleven sections and a first answer shows one, so the rest are
// invisible to anybody who does not already know they are there. Asked about a
// household member's work, the model looked the name up in the encyclopedia, and
// the reason was the same: nothing ever said the drives were in here. Naming the
// questions is the general fix, and it beats teaching the model one household.

// Prompt is a chip. The label is what it reads as, and the ask is what gets sent,
// which is a whole question carrying the address because it arrives as the next
// turn with none of this conversation behind it.
//
// Both come from here rather than the label being derived from the question in
// the page, which turned "What is there to do near X" into "Is there to do".
type Prompt struct {
	Label string `json:"label"`
	Ask   string `json:"ask"`
}

// Prompts are the follow ups worth offering, only for the sections that have
// something in them, since an offer that comes back empty is worse than none.
func (r *Report) Prompts() []Prompt {
	// The whole address, not the street line. A chip arrives as the next turn
	// with none of this conversation behind it, and "402 Sample Rd" on its own
	// does not geocode, so every chip came back "could not place" and the model
	// started inventing towns to bolt on.
	where := r.Full()
	if where == "" {
		return nil
	}
	var out []Prompt
	add := func(label, q string) {
		out = append(out, Prompt{Label: label, Ask: fmt.Sprintf(q, where)})
	}

	if len(r.Quotes) > 0 {
		add("All-in cost", "What would %s cost a month under each loan?")
	} else {
		add("All-in cost", "What would %s cost a month? Ask me for the asking price if you need it.")
	}
	if r.Zones.Elementary != "" || r.Zones.High != "" {
		add("Schools", "What schools is %s zoned for, and what is the drop off detour?")
	}
	if r.Morning.Commute.Minutes > 0 || len(r.Drives) > 0 {
		// Phrased to cover the whole household without naming anybody, which is
		// the half of this that keeps working when the config changes.
		add("Commutes", "How long are the commutes and the school run from %s, for everyone in the house?")
	}
	if r.Flood.Measured {
		add("Flood risk", "What is the flood risk at %s, and how close is the water?")
	}
	if r.Road.Measured {
		add("The road", "What road is %s on, how busy is it, and how close is the interstate?")
	}
	if r.Street.Found || r.Area.Found {
		add("Neighbourhood", "What is the neighbourhood around %s like, and what is the crime?")
	}
	if r.Parcel.Found || r.Terrain.MeanSlopePct > 0 {
		add("The lot", "What is the lot at %s like, how flat is it, and what is it assessed at?")
	}
	if len(r.Outings.Nearest) > 0 {
		add("Things to do", "What is there to do near %s?")
	}
	add("Listing links", "Give me the listing and map links for %s")
	return out
}
