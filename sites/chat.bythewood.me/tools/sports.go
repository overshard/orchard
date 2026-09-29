package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// leagues maps a name to ESPN's sport and league. The scoreboard is read off
// site.api.espn.com, since the cdn.espn.com pages espn.com itself used started
// redirecting and then 404ing.
var leagues = map[string]string{
	"nfl": "football/nfl", "college-football": "football/college-football",
	"nba": "basketball/nba", "wnba": "basketball/wnba", "mlb": "baseball/mlb",
	"nhl": "hockey/nhl", "tennis": "tennis/atp", "golf": "golf/pga",
	"nascar": "racing/nascar-premier", "f1": "racing/f1",
	"epl": "soccer/eng.1", "mls": "soccer/usa.1",
	"champions-league": "soccer/uefa.champions",
}

var SportsScores = Tool{
	Name: "sports_scores",
	Description: "Scoreboard for one league, with the sportsbook line and over/under on games that have one. " +
		"With no date it is the current round of games. For a team's next game pass date, the day it is " +
		"probably played, worked out from now. Pass team to see only that team's games. " +
		"For anything not listed, use web_search instead.",
	Schema: obj(map[string]any{
		"league": map[string]any{"type": "string", "description": "which league",
			"enum": []string{"nfl", "college-football", "nba", "wnba", "mlb", "nhl",
				"tennis", "golf", "nascar", "f1", "epl", "mls", "champions-league"}},
		"team": str("optional, a team or player name to keep only their games, like Panthers"),
		"date": str("optional, YYYY-MM-DD, the day to show instead of the current round"),
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
		var raw struct {
			Events json.RawMessage `json:"events"`
		}
		u := "https://site.api.espn.com/apis/site/v2/sports/" + path + "/scoreboard?limit=300"
		if day := strings.TrimSpace(argStr(a, "date")); day != "" {
			t, err := time.Parse("2006-01-02", day)
			if err != nil {
				return nil, fmt.Errorf("date %q is not YYYY-MM-DD", day)
			}
			u += "&dates=" + t.Format("20060102")
		}
		team := strings.ToLower(strings.TrimSpace(argStr(a, "team")))
		if err := getJSON(ctx, d, u, &raw); err != nil {
			return nil, fmt.Errorf("%w (try web_search for the scores)", err)
		}
		blob := raw.Events
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
				Odds []struct {
					Details   string  `json:"details"`
					OverUnder float64 `json:"overUnder"`
					Provider  struct {
						Name string `json:"name"`
					} `json:"provider"`
				} `json:"odds"`
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
			Name      string  `json:"name"`
			Date      string  `json:"date"`
			Status    string  `json:"status"`
			Completed bool    `json:"completed"`
			Sides     []side  `json:"sides,omitempty"`
			Line      string  `json:"line,omitempty"`
			OverUnder float64 `json:"over_under,omitempty"`
			Book      string  `json:"sportsbook,omitempty"`
		}
		out := make([]game, 0, 16)
		for _, e := range evs {
			g := game{Name: firstNonEmpty(e.ShortName, e.Name), Date: e.Date,
				Status: e.Status.Type.Detail, Completed: e.Status.Type.Completed}
			if len(e.Competitions) > 0 {
				c := e.Competitions[0]
				for _, p := range c.Competitors {
					n := firstNonEmpty(p.Team.DisplayName, p.Athlete.DisplayName)
					if n != "" {
						g.Sides = append(g.Sides, side{Name: n, Score: p.Score})
					}
				}
				if len(c.Odds) > 0 && !g.Completed {
					g.Line, g.OverUnder, g.Book = c.Odds[0].Details, c.Odds[0].OverUnder, c.Odds[0].Provider.Name
				}
			}
			if team != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.ShortName), team) {
				continue
			}
			out = append(out, g)
			if len(out) >= 16 {
				break
			}
		}
		if len(out) == 0 {
			note := "no events on the board for this league right now"
			if team != "" {
				note = "no game for " + team + " on this board, try the date of their next game"
			}
			return map[string]any{"league": key, "events": out, "note": note}, nil
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
	Description: "What a Polymarket prediction market implies about an event, as a percentage. " +
		"This is a price people are paying, not a forecast, and it should be said that way. " +
		"Only for things people bet on: an election, a match, a nomination, a rate decision. " +
		"A single game is listed by nicknames, like \"Lions vs. Panthers\", with the date it closes, so search " +
		"with the nickname alone. For a sportsbook spread or over/under on a game, sports_scores has the line. " +
		"It is not a price check and knows nothing about what a product costs, so use " +
		"web_search for anything on sale. It knows nothing about when anybody streams, posts or releases " +
		"something either.",
	Schema: obj(map[string]any{
		"query": str("the event or team, like \"US Open winner\", \"government shutdown\" or \"Panthers\""),
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
			return res, getJSON(ctx, d, u, &res)
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
				// A single game trades thinly until the week of it and is still
				// the market that was asked about.
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
					// Exactly 0 or 1 is a settled leg, an eliminated name
					// rather than a long shot, so it is dropped instead of
					// being listed at 0%.
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
			return nil, fmt.Errorf("no active betting market matching %q, try web_search", q)
		}
		return map[string]any{"markets": out,
			"note": "implied probability from a betting market, which is a price and not a forecast"}, nil
	},
}

// oddsFiller is what a question about a bet says around the name, and none of it
// is in a market's title.
var oddsFiller = regexp.MustCompile(`(?i)\b(betting|odds|next|game|match|line|lines|spread|moneyline|who will win|chances?|of|the|for|on)\b`)

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
