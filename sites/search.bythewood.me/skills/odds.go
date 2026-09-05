package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Polymarket's Gamma API is public, keyless and needs no wallet. Only order
// placement is authenticated, and nothing here places one.
const gammaSearchURL = "https://gamma-api.polymarket.com/public-search"

// Odds answers "what are the chances" from a prediction market.
//
// This exists because it is the question the web is worst at. A search for the
// odds on a tournament returns bookmaker affiliate pages, listicles written
// before the event started, and fractional odds nobody converts, and the model
// then paraphrases whichever it fetched. A prediction market publishes a single
// number that already means a probability, and it is current to the minute.
//
// The number is what people are betting, which is not the same thing as the
// truth, so the answer says so rather than presenting it as a forecast.
type Odds struct{}

func (Odds) Card() Card {
	return Card{
		Name: "odds",
		Does: "reports the current market-implied probability of a future event, such as who wins an election, a tournament or a title, from a prediction market.",
		Fires: []string{
			"what are the odds on the us open",
			"who is favoured to win the election",
			"chances of a rate cut this month",
			"is trump likely to win",
			"what are the odds the fed cuts rates",
		},
		NotFor: []string{
			"who won the us open",
			"what are the rules of the us open",
			"when is the election",
			"what happened in the last election",
			"how do betting odds work",
		},
		Keywords: []string{"odds", "chances of", "likely to win", "favourite to win",
			"favorite to win", "probability of", "who will win"},
	}
}

type gammaSearch struct {
	Events []struct {
		Title   string  `json:"title"`
		Slug    string  `json:"slug"`
		Closed  bool    `json:"closed"`
		Active  bool    `json:"active"`
		Volume  float64 `json:"volume"`
		EndDate string  `json:"endDate"`
		Markets []struct {
			GroupItemTitle string `json:"groupItemTitle"`
			Question       string `json:"question"`
			Closed         bool   `json:"closed"`
			// Both of these are JSON arrays encoded as strings inside the JSON,
			// so they need a second decode. Reading them as []string silently
			// yields nothing.
			Outcomes      string `json:"outcomes"`
			OutcomePrices string `json:"outcomePrices"`
		} `json:"markets"`
	} `json:"events"`
}

type contender struct {
	Name  string
	Prob  float64
	Fixed bool
}

func (Odds) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()

	q := oddsQuery(question)
	if q == "" {
		return nil, nil
	}

	// events_status=active drops the settled markets, which is what made a
	// search for a tournament return last year's.
	u := fmt.Sprintf("%s?q=%s&limit_per_type=4&events_status=active",
		gammaSearchURL, url.QueryEscape(q))
	var s gammaSearch
	if err := getJSON(ctx, d, u, &s); err != nil {
		return nil, err
	}

	for _, ev := range s.Events {
		if ev.Closed || len(ev.Markets) == 0 {
			continue
		}
		var picks []contender
		for _, m := range ev.Markets {
			var names, prices []string
			if json.Unmarshal([]byte(m.Outcomes), &names) != nil ||
				json.Unmarshal([]byte(m.OutcomePrices), &prices) != nil ||
				len(names) == 0 || len(prices) != len(names) {
				continue
			}
			label := m.GroupItemTitle
			if label == "" {
				label = m.Question
			}
			// A Yes/No market on one contender carries that contender's
			// probability in the Yes leg. A market with real outcome names
			// carries one row per outcome instead.
			if len(names) == 2 && strings.EqualFold(names[0], "yes") {
				p, err := strconv.ParseFloat(prices[0], 64)
				if err != nil {
					continue
				}
				picks = append(picks, contender{Name: label, Prob: p, Fixed: m.Closed})
				continue
			}
			for i, n := range names {
				p, err := strconv.ParseFloat(prices[i], 64)
				if err != nil {
					continue
				}
				picks = append(picks, contender{Name: n, Prob: p, Fixed: m.Closed})
			}
		}

		// A settled leg prices at exactly 0 or 1 and is an eliminated name
		// rather than a long shot, so it is dropped instead of listed at 0%.
		live := picks[:0]
		for _, p := range picks {
			if p.Fixed || p.Prob <= 0.001 {
				continue
			}
			live = append(live, p)
		}
		if len(live) == 0 {
			continue
		}
		sort.SliceStable(live, func(i, j int) bool { return live[i].Prob > live[j].Prob })

		return &Result{
			Skill: "odds", Shape: "factual",
			Text:    oddsText(ev.Title, ev.Slug, ev.EndDate, ev.Volume, live, d),
			Sources: []Source{{URL: "https://polymarket.com/event/" + ev.Slug, Title: ev.Title, Site: "polymarket.com"}},
			Elapsed: d.now().Sub(start).Round(10 * time.Millisecond).String(),
		}, nil
	}
	return nil, nil
}

func oddsText(title, slug, end string, volume float64, live []contender, d Deps) string {
	var b strings.Builder

	top := live[0]
	if len(live) == 1 {
		fmt.Fprintf(&b, "**%.0f%%** on %s.\n\n", top.Prob*100, strings.ToLower(title))
	} else {
		fmt.Fprintf(&b, "**%s** is the favourite at **%.0f%%** in %s.\n\n",
			top.Name, top.Prob*100, title)
	}

	shown := live
	if len(shown) > 8 {
		shown = shown[:8]
	}
	for _, p := range shown {
		fmt.Fprintf(&b, "- **%s** %.0f%%\n", p.Name, p.Prob*100)
	}
	if len(live) > len(shown) {
		fmt.Fprintf(&b, "- %d others below %.0f%%\n", len(live)-len(shown), shown[len(shown)-1].Prob*100)
	}

	// The one caveat that changes how the number should be read, and the only
	// thing here the bullets do not already carry.
	b.WriteString("\nThese are prices on Polymarket, so they say what people are betting rather than what will happen")
	if volume > 0 {
		fmt.Fprintf(&b, ", on **$%s** of volume", formatNumber(round2(volume)))
	}
	if t, err := time.Parse(time.RFC3339, end); err == nil {
		fmt.Fprintf(&b, ", resolving %s", t.Format("2 January 2006"))
	}
	fmt.Fprintf(&b, ". Read %s.", d.now().Format("3:04 PM on 2 January"))
	return b.String()
}

// oddsQuery strips the asking words so the search sees the subject. Sending the
// whole question matches on "what" and "the" and returns whatever is busiest.
func oddsQuery(question string) string {
	l := strings.ToLower(question)
	for _, cut := range []string{
		"what are the odds on", "what are the odds that", "what are the odds of",
		"what are the odds", "what is the probability of", "what are the chances of",
		"what are the chances", "chances of", "odds on", "odds of", "odds for",
		"who is favoured to win", "who is favored to win", "who is likely to win",
		"who will win", "is it likely that", "how likely is",
	} {
		l = strings.ReplaceAll(l, cut, " ")
	}
	var keep []string
	for _, w := range strings.Fields(l) {
		w = strings.Trim(w, "?.,!'\"")
		switch w {
		case "", "the", "a", "an", "of", "on", "in", "for", "to", "is", "are",
			"be", "will", "what", "who", "odds", "chance", "chances", "probability",
			"likely", "favourite", "favorite", "win", "wins", "winning", "this", "that":
			continue
		}
		keep = append(keep, w)
	}
	if len(keep) == 0 {
		return ""
	}
	if len(keep) > 6 {
		keep = keep[:6]
	}
	return strings.Join(keep, " ")
}
