package main

import "time"

// A new pond opens up over its first days, Works when the egg hatches and
// the market three days after that. Only create sets staged, so a pond made
// before this is fully open.
const (
	worksOpenAfter  = 24 * time.Hour
	marketOpenAfter = 4 * 24 * time.Hour
)

// openView is presentation only. No move is refused for a closed place.
type openView struct {
	Works  bool `json:"works"`
	Market bool `json:"market"`
}

func openOf(p *pond, now time.Time) openView {
	if !p.Staged {
		return openView{true, true}
	}
	age := now.Sub(time.Unix(p.CreatedAt, 0))
	return openView{age >= worksOpenAfter, age >= marketOpenAfter}
}

// has says whether the page's view a row points at is open.
func (o openView) has(to string) bool {
	switch to {
	case "reeds", "garden", "bank":
		return o.Works
	case "market":
		return o.Works && o.Market
	}
	return true
}

// openingNotice is the gazette's line for a place through its first day open.
func openingNotice(p *pond, now time.Time) string {
	if !p.Staged {
		return ""
	}
	age := now.Sub(time.Unix(p.CreatedAt, 0))
	switch {
	case age >= marketOpenAfter && age < marketOpenAfter+24*time.Hour:
		return "The pond market opens its board today, behind the new Market key at the bank"
	case age >= worksOpenAfter && age < worksOpenAfter+24*time.Hour:
		return "The works open along the bank today, with the reeds, a garden and a gravel bar on the new Works key"
	}
	return ""
}
