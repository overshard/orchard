package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// leagues maps a name to ESPN's path. The host matters more than anything else
// here: site.api.espn.com is what every write-up points at and it answers 403,
// while cdn.espn.com is what espn.com itself calls and serves the same data to
// anyone. Soccer is a query parameter rather than a path, and the wrong shape
// answers 200 with no events, which looks like a quiet day rather than a bug.
var leagues = map[string]string{
	"nfl": "football/nfl", "college-football": "football/college-football",
	"nba": "basketball/nba", "wnba": "basketball/wnba", "mlb": "baseball/mlb",
	"nhl": "hockey/nhl", "tennis": "tennis", "golf": "golf",
	"nascar": "racing/nascar-premier", "f1": "racing/f1",
	"epl": "soccer?league=eng.1", "mls": "soccer?league=usa.1",
	"champions-league": "soccer?league=uefa.champions",
}

var SportsScores = Tool{
	Name:        "sports_scores",
	Description: "Live and recent scoreboard for one league. For anything not listed, use web_search instead.",
	Schema: obj(map[string]any{
		"league": map[string]any{"type": "string", "description": "which league",
			"enum": []string{"nfl", "college-football", "nba", "wnba", "mlb", "nhl",
				"tennis", "golf", "nascar", "f1", "epl", "mls", "champions-league"}},
	}, "league"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		key := strings.ToLower(argStr(a, "league"))
		path, ok := leagues[key]
		if !ok {
			names := make([]string, 0, len(leagues))
			for k := range leagues {
				names = append(names, k)
			}
			return nil, fmt.Errorf("no league called %q, known: %s", key, strings.Join(names, ", "))
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		var raw struct {
			Content struct {
				SBData struct {
					Events json.RawMessage `json:"events"`
				} `json:"sbData"`
				Events json.RawMessage `json:"events"`
			} `json:"content"`
		}
		if err := getJSON(ctx, d, "https://cdn.espn.com/core/"+path+sep+"xhr=1", &raw); err != nil {
			return nil, fmt.Errorf("%w (try web_search for the scores)", err)
		}
		blob := raw.Content.SBData.Events
		if len(blob) == 0 {
			blob = raw.Content.Events
		}
		var evs []struct {
			Name      string `json:"name"`
			ShortName string `json:"shortName"`
			Date      string `json:"date"`
			Status    struct {
				Type struct {
					Detail    string `json:"detail"`
					Completed bool   `json:"completed"`
				} `json:"type"`
			} `json:"status"`
			Competitions []struct {
				Competitors []struct {
					Team struct {
						DisplayName string `json:"displayName"`
					} `json:"team"`
					Score   string `json:"score"`
					Athlete struct {
						DisplayName string `json:"displayName"`
					} `json:"athlete"`
				} `json:"competitors"`
			} `json:"competitions"`
		}
		if len(blob) > 0 {
			_ = json.Unmarshal(blob, &evs)
		}
		type side struct {
			Name  string `json:"name"`
			Score string `json:"score,omitempty"`
		}
		type game struct {
			Name      string `json:"name"`
			Date      string `json:"date"`
			Status    string `json:"status"`
			Completed bool   `json:"completed"`
			Sides     []side `json:"sides,omitempty"`
		}
		out := make([]game, 0, 16)
		for _, e := range evs {
			g := game{Name: firstNonEmpty(e.ShortName, e.Name), Date: e.Date,
				Status: e.Status.Type.Detail, Completed: e.Status.Type.Completed}
			if len(e.Competitions) > 0 {
				for _, c := range e.Competitions[0].Competitors {
					n := firstNonEmpty(c.Team.DisplayName, c.Athlete.DisplayName)
					if n != "" {
						g.Sides = append(g.Sides, side{Name: n, Score: c.Score})
					}
				}
			}
			out = append(out, g)
			if len(out) >= 16 {
				break
			}
		}
		if len(out) == 0 {
			return map[string]any{"league": key, "events": out,
				"note": "no events on the board for this league right now"}, nil
		}
		return map[string]any{"league": key, "events": out}, nil
	},
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------- odds

var Odds = Tool{
	Name: "odds",
	Description: "What a real betting market implies about an event, as a percentage. " +
		"This is a price people are paying, not a forecast, and it should be said that way. " +
		"Only for things people bet on: an election, a match, a nomination, a rate decision. " +
		"It is not a price check and knows nothing about what a product costs, so use " +
		"web_search for anything on sale.",
	Schema: obj(map[string]any{
		"query": str("the event, like \"US Open winner\" or \"government shutdown\""),
	}, "query"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := argStr(a, "query")
		if q == "" {
			return nil, fmt.Errorf("query is required")
		}
		// /events?search= is silently ignored by Polymarket and hands back the
		// top volume list whatever you ask, which is how a question about
		// tennis came back with a presidential nomination market.
		// /public-search is the endpoint that actually searches.
		var res struct {
			Events []struct {
				Title   string `json:"title"`
				Volume  any    `json:"volume"`
				Markets []struct {
					Question  string `json:"question"`
					Outcomes  string `json:"outcomes"`
					Prices    string `json:"outcomePrices"`
					VolumeNum any    `json:"volumeNum"`
				} `json:"markets"`
			} `json:"events"`
		}
		u := "https://gamma-api.polymarket.com/public-search?limit_per_type=10&events_status=active&q=" + url.QueryEscape(q)
		if err := getJSON(ctx, d, u, &res); err != nil {
			return nil, fmt.Errorf("%w (try web_search)", err)
		}
		type leg struct {
			Outcome string  `json:"outcome"`
			Pct     float64 `json:"implied_pct"`
		}
		type mkt struct {
			Event  string  `json:"event"`
			Market string  `json:"market"`
			Volume float64 `json:"volume_usd"`
			Legs   []leg   `json:"legs"`
		}
		var out []mkt
		for _, e := range res.Events {
			for _, m := range e.Markets {
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
				// to one with twenty million, and answering from the first is
				// how "how is the US Open going" got a Chipotle market.
				if vol < 10000 {
					continue
				}
				var legs []leg
				for i := range names {
					if i >= len(prices) {
						break
					}
					var p float64
					fmt.Sscanf(prices[i], "%g", &p)
					// Exactly 0 or 1 is a settled leg, an eliminated name
					// rather than a long shot, so it is dropped instead of
					// being listed at 0%.
					if p > 0 && p < 1 {
						legs = append(legs, leg{Outcome: names[i], Pct: round1(p * 100)})
					}
				}
				if len(legs) > 0 {
					out = append(out, mkt{Event: e.Title, Market: m.Question, Volume: vol, Legs: legs})
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
			return nil, fmt.Errorf("no active betting market matching %q, try web_search", q)
		}
		return map[string]any{"markets": out,
			"note": "implied probability from a betting market, which is a price and not a forecast"}, nil
	},
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
