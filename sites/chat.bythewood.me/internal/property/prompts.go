package property

import "fmt"

// A first answer shows one section, so the rest are invisible to anybody who
// doesn't already know they're there, the model included.

// Prompt is a chip. Ask carries the whole address since it arrives as the next
// turn with none of this conversation behind it, and Label is written here since
// deriving it in the page turned "What is there to do near X" into "Is there to do".
type Prompt struct {
	Label string `json:"label"`
	Ask   string `json:"ask"`
}

// Prompts skips empty sections, since an offer that comes back empty is worse
// than none.
func (r *Report) Prompts() []Prompt {
	// The street line alone doesn't geocode, and the model then invents towns to
	// bolt on.
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
		// Names nobody, so it keeps working when the config changes.
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
	if r.Industry.Measured {
		add("Industry nearby", "Is there anything I would not want to live near around %s, like data centres, factories, landfills or quarries?")
	}
	if len(r.Outings.Nearest) > 0 {
		add("Things to do", "What is there to do near %s?")
	}
	add("Listing links", "Give me the listing and map links for %s")
	return out
}
