package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type oddsArgs struct {
	Query string
}

func (a *oddsArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "the event or team, like \"US Open winner\", \"government shutdown\" or \"Panthers\"").required(),
	}
}

var Odds = Define(Tool{
	Name:   "odds",
	Traits: Outbound,
	Description: "What a Polymarket prediction market implies about an event, as a percentage. " +
		"This is a price people are paying, not a forecast, and it should be said that way. " +
		"Only for things people bet on: an election, a match, a nomination, a rate decision. " +
		"A single game is listed by nicknames, like \"Lions vs. Panthers\", with the date it closes, so search " +
		"with the nickname alone. For a sportsbook spread or over/under on a game, sports_scores has the line. " +
		"It is not a price check and knows nothing about what a product costs, so use " +
		"web_search for anything on sale. It knows nothing about when anybody streams, posts or releases " +
		"something either.",
}, func(ctx context.Context, d *Turn, a oddsArgs) (any, error) {
	q := a.Query
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	// Polymarket silently ignores /events?search= and hands back the top
	// volume list whatever you ask, so this uses /public-search.
	type result struct {
		Events []struct {
			Title   string `json:"title"`
			Volume  any    `json:"volume"`
			EndDate string `json:"endDate"`
			Markets []struct {
				Question  string `json:"question"`
				Outcomes  string `json:"outcomes"`
				Prices    string `json:"outcomePrices"`
				VolumeNum any    `json:"volumeNum"`
				GameStart string `json:"gameStartTime"`
			} `json:"markets"`
		} `json:"events"`
	}
	search := func(q string) (result, error) {
		var res result
		u := "https://gamma-api.polymarket.com/public-search?limit_per_type=10&events_status=active&q=" + url.QueryEscape(q)
		return res, getJSON(ctx, d.Deps, u, &res)
	}
	hasGame := func(r result) bool {
		for _, e := range r.Events {
			for _, m := range e.Markets {
				if m.GameStart != "" {
					return true
				}
			}
		}
		return false
	}
	q = strings.TrimSpace(oddsFiller.ReplaceAllString(q, " "))
	res, err := search(q)
	if err != nil {
		return nil, fmt.Errorf("%w (try web_search)", err)
	}
	// Games are titled by nickname, so "Carolina Panthers" finds only the
	// season long futures and "Panthers" finds the games.
	if words := strings.Fields(q); len(words) > 1 && !hasGame(res) {
		if more, err := search(words[len(words)-1]); err == nil && hasGame(more) {
			res = more
		}
	}
	type leg struct {
		Outcome string  `json:"outcome"`
		Pct     float64 `json:"implied_pct"`
	}
	type mkt struct {
		Event  string  `json:"event"`
		Market string  `json:"market"`
		Closes string  `json:"closes,omitempty"`
		Volume float64 `json:"volume_usd"`
		Legs   []leg   `json:"legs"`
	}
	var out []mkt
	words := oddsWords(q)
	for _, e := range res.Events {
		for _, m := range e.Markets {
			// Polymarket matches any one word, so it hands back markets with
			// nothing to do with the question.
			if !aboutWords(e.Title+" "+m.Question, words) {
				continue
			}
			// Polymarket encodes these arrays as JSON strings inside its
			// JSON, so reading them as []string silently yields nothing.
			var names []string
			var prices []string
			if json.Unmarshal([]byte(m.Outcomes), &names) != nil {
				continue
			}
			if json.Unmarshal([]byte(m.Prices), &prices) != nil {
				continue
			}
			vol := asFloat(m.VolumeNum)
			if vol == 0 {
				vol = asFloat(e.Volume)
			}
			// A novelty market with two hundred dollars in it is noise next
			// to one with twenty million, but a single game trades thinly
			// until the week of it and is still the market that was asked about.
			if vol < 10000 && (m.GameStart == "" || vol < 100) {
				continue
			}
			var legs []leg
			for i := range names {
				if i >= len(prices) {
					break
				}
				var p float64
				fmt.Sscanf(prices[i], "%g", &p)
				// Exactly 0 or 1 is a settled leg, an eliminated name and
				// not a long shot, so it's dropped instead of listed at 0%.
				if p > 0 && p < 1 {
					legs = append(legs, leg{Outcome: names[i], Pct: round1(p * 100)})
				}
			}
			if len(legs) > 0 {
				out = append(out, mkt{Event: e.Title, Market: m.Question, Closes: dateOnly(e.EndDate), Volume: vol, Legs: legs})
			}
			if len(out) >= 8 {
				break
			}
		}
		if len(out) >= 8 {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no betting market is about %q. odds only covers what people bet on, "+
			"like elections, games and rate decisions, so anything else needs another tool", q)
	}
	return map[string]any{"markets": out,
		"note": "implied probability from a betting market, which is a price and not a forecast"}, nil
})

// oddsFiller is the words a question about a bet puts around the name, and none
// of them are in a market's title.
var oddsFiller = regexp.MustCompile(`(?i)\b(betting|odds|next|game|match|line|lines|spread|moneyline|who will win|chances?|of|the|for|on)\b`)

var oddsStop = map[string]bool{"a": true, "an": true, "and": true, "in": true, "to": true, "is": true,
	"be": true, "by": true, "at": true, "vs": true, "who": true, "what": true, "when": true, "will": true,
	"win": true, "wins": true, "does": true, "do": true, "top": true, "me": true, "my": true, "i": true}

func oddsWords(q string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if !oddsStop[w] {
			out = append(out, w)
		}
	}
	return out
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// aboutWords is a title carrying at least half of the words asked about, where
// a plural or a longer form of a word counts as that word.
func aboutWords(title string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	have := wordRe.FindAllString(strings.ToLower(title), -1)
	hit := 0
	for _, w := range words {
		for _, h := range have {
			if h == w || (len(w) >= 4 && strings.HasPrefix(h, w)) || (len(h) >= 4 && strings.HasPrefix(w, h)) {
				hit++
				break
			}
		}
	}
	return hit*2 >= len(words)
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		var f float64
		fmt.Sscanf(n, "%g", &f)
		return f
	}
	return 0
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
