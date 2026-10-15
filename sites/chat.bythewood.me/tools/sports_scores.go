package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// leagues maps a name to ESPN's sport and league. The scoreboard is read off
// site.api.espn.com, since the cdn.espn.com pages redirect and then 404.
var leagues = map[string]string{
	"nfl": "football/nfl", "college-football": "football/college-football",
	"nba": "basketball/nba", "wnba": "basketball/wnba", "mlb": "baseball/mlb",
	"nhl": "hockey/nhl", "tennis": "tennis/atp", "golf": "golf/pga",
	"nascar": "racing/nascar-premier", "f1": "racing/f1",
	"epl": "soccer/eng.1", "mls": "soccer/usa.1",
	"champions-league": "soccer/uefa.champions",
}

type sportsScoresArgs struct {
	League string
	Team   string
	Date   string
}

func (a *sportsScoresArgs) params() []param {
	return []param{
		strArg(&a.League, "league", "which league").oneOf("nfl", "college-football", "nba", "wnba", "mlb", "nhl",
			"tennis", "golf", "nascar", "f1", "epl", "mls", "champions-league").required(),
		strArg(&a.Team, "team", "optional, a team or player name to keep only their games, like Panthers"),
		strArg(&a.Date, "date", "optional, YYYY-MM-DD, the day to show instead of the current round"),
	}
}

var SportsScores = Define(Tool{
	Name: "sports_scores",
	Description: "Scoreboard for one league, with the sportsbook line and over/under on games that have one. " +
		"With no date it is the current round of games. For a team's next game pass date, the day it is " +
		"probably played, worked out from now. Pass team to see only that team's games. " +
		"For anything not listed, use web_search instead.",
}, func(ctx context.Context, d *Turn, a sportsScoresArgs) (any, error) {
	key := strings.ToLower(a.League)
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
	if day := a.Date; day != "" {
		t, err := time.Parse("2006-01-02", day)
		if err != nil {
			return nil, fmt.Errorf("date %q is not YYYY-MM-DD", day)
		}
		u += "&dates=" + t.Format("20060102")
	}
	team := strings.ToLower(a.Team)
	if err := getJSON(ctx, d.Deps, u, &raw); err != nil {
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
})

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
