package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// "Did the Panthers win last night" has an exact answer, and reading it off a
// scoreboard beats paraphrasing a recap someone wrote about it.
//
// The source is ESPN's own site data. Note the host: site.api.espn.com is the
// documented-by-folklore one and it answers 403 to anything off ESPN's network,
// while cdn.espn.com, which is what espn.com itself calls, serves the same
// scoreboard to anyone. That difference is the whole reason this works.
//
// No team list is hardcoded. Team names come out of the scoreboard responses,
// so a franchise moving or renaming needs no change here.

// Two URL shapes, which is not obvious and cost a round of debugging: the
// American leagues are a path (core/nfl/scoreboard) while soccer is one path
// with the competition as a parameter (core/soccer/scoreboard?league=eng.1).
// Asking for core/soccer/eng.1/scoreboard answers 200 with a payload that has
// no events in it at all, so it looks like a quiet day rather than a wrong URL.
const (
	espnLeagueURL = "https://cdn.espn.com/core/%s/scoreboard?xhr=1"
	espnSoccerURL = "https://cdn.espn.com/core/soccer/scoreboard?xhr=1&league=%s"
)

// Every league is checked in parallel, because a bare team name is often
// ambiguous. "Panthers" is Carolina in the NFL and Florida in the NHL, and
// answering for one silently would be wrong half the time.
//
// The soccer list is long on purpose. It is the sport most likely to come up in
// conversation with people outside the US, and a European league missing from
// this list is a question that falls through to a web search.
type league struct {
	path   string
	name   string
	soccer bool
}

var leagues = []league{
	{"nfl", "NFL", false},
	{"college-football", "college football", false},
	{"nba", "NBA", false},
	{"mlb", "MLB", false},
	{"nhl", "NHL", false},

	{"eng.1", "Premier League", true},
	{"esp.1", "La Liga", true},
	{"ita.1", "Serie A", true},
	{"ger.1", "Bundesliga", true},
	{"fra.1", "Ligue 1", true},
	{"por.1", "Primeira Liga", true},
	{"ned.1", "Eredivisie", true},
	{"usa.1", "MLS", true},
	{"eng.2", "Championship", true},
	{"uefa.champions", "Champions League", true},
	{"uefa.europa", "Europa League", true},
	{"fifa.world", "World Cup", true},
}

type espnPayload struct {
	Content struct {
		SBData struct {
			Events []espnEvent `json:"events"`
		} `json:"sbData"`
	} `json:"content"`
}

type espnEvent struct {
	Name   string `json:"name"`
	Date   string `json:"date"`
	Status struct {
		Type struct {
			Completed   bool   `json:"completed"`
			ShortDetail string `json:"shortDetail"`
			State       string `json:"state"`
		} `json:"type"`
	} `json:"status"`
	Competitions []struct {
		Competitors []struct {
			HomeAway string `json:"homeAway"`
			Score    string `json:"score"`
			Winner   bool   `json:"winner"`
			Team     struct {
				DisplayName string `json:"displayName"`
				Name        string `json:"name"`
				Location    string `json:"location"`
				Abbrev      string `json:"abbreviation"`
			} `json:"team"`
		} `json:"competitors"`
	} `json:"competitions"`
	Links []struct {
		Href string `json:"href"`
	} `json:"links"`
}

// gameResult is one match involving the team asked about.
type gameResult struct {
	League    string
	Date      time.Time
	Completed bool
	Detail    string
	Home      string
	Away      string
	HomeScore string
	AwayScore string
	Winner    string
	Link      string
}

// Sports reads a scoreboard. It answers what already happened, so a question
// about a fixture that has not been played yet has no result to give and falls
// through to the web.
type Sports struct{}

func (Sports) Card() Card {
	return Card{
		Name: "sports",
		Does: "reports the score and result of a recent match for a named team, from the scoreboard.",
		Fires: []string{
			"did Liverpool win their last match",
			"what was the score in the chiefs game",
			"who won the arsenal match",
			"did the yankees win last night",
			"final score of the lakers game",
		},
		NotFor: []string{
			"when do liverpool play next",
			"who is the best premier league team",
			"how many super bowls have the patriots won",
			"what are the odds on the us open",
			"explain the offside rule",
		},
		Keywords: []string{"did the ", "who won", "final score", "score in the",
			"win last night", "won last night", "match result", "did they win"},
	}
}

func looksLikeSport(q string) bool {
	l := strings.ToLower(q)
	return containsAny(l,
		"did the ", "who won", "score", "did we win", "final score",
		"beat the ", "play last night", "playing tonight", "game last night",
		"win last night", "won last night", "won yesterday", "win yesterday",
		// Football in the rest of the world, and the words around a match.
		"match result", "did they win", "premier league", "la liga", "serie a",
		"bundesliga", "champions league", "europa league", "fixture", "kick off",
		"how did ", " draw ", "nil nil", "full time")
}

func (Sports) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()

	// A date-oriented league needs to be asked for the right day. Week-oriented
	// ones (the NFL) ignore it and return the current week, which is what a
	// question about last night wants anyway.
	when := targetDate(question, d)

	var (
		mu        sync.Mutex
		all       []gameResult
		throttled bool
		wg        sync.WaitGroup
		team      = teamWords(question)
	)
	if len(team) == 0 {
		return nil, nil
	}

	// One question used to fetch every league at once, seventeen requests of
	// about a megabyte, and ESPN answered the lot with 202 and an empty body.
	// It reads as an empty scoreboard rather than as a refusal, so the skill
	// looked like it simply had no game to report.
	//
	// So the sweep is ordered and stops early. A question naming its
	// competition goes straight to it, and otherwise the leagues are tried a
	// few at a time, most likely first, until one has the team in it.
	order := leagueOrder(question)
	for i := 0; i < len(order); i += 3 {
		wave := order[i:min(i+3, len(order))]
		for _, lg := range wave {
			wg.Add(1)
			go func(path, name string, soccer bool) {
				defer wg.Done()

				url := fmt.Sprintf(espnLeagueURL, path)
				if soccer {
					url = fmt.Sprintf(espnSoccerURL, path)
				}
				if !when.IsZero() {
					url += "&dates=" + when.Format("20060102")
				}
				var p espnPayload
				if err := getJSONCached(ctx, d, "espn.com", url, &p); err != nil {
					if errors.Is(err, errThrottled) {
						mu.Lock()
						throttled = true
						mu.Unlock()
					}
					return
				}
				for _, ev := range p.Content.SBData.Events {
					if g, ok := matchTeam(ev, team, name); ok {
						mu.Lock()
						all = append(all, g)
						mu.Unlock()
					}
				}
			}(lg.path, lg.name, lg.soccer)
		}
		wg.Wait()
		// A team in two leagues is nearly always in two of the same wave, so
		// stopping here still catches the case the answer mentions.
		mu.Lock()
		done := len(all) > 0
		mu.Unlock()
		if done {
			break
		}
	}
	// A throttled upstream and a team with no fixture both come back empty, and
	// telling them apart matters: the first is worth retrying and worth seeing
	// in a log, and the second is a real answer the web can give instead.
	if len(all) == 0 {
		if throttled {
			return nil, errThrottled
		}
		return nil, nil
	}
	// Most recent first, and a finished game beats a scheduled one, because
	// "did they win" is a question about something that already happened.
	sortGames(all)

	var b strings.Builder
	head := all[0]
	// Asking about last night and being shown a fixture is an answer to a
	// different question, so say which one it is.
	if asksAboutPast(question) && !head.Completed {
		fmt.Fprintf(&b, "**No game has been played yet.** %s\n\n",
			strings.TrimPrefix(headline(head, d), "Not played yet. "))
	} else {
		b.WriteString(headline(head, d) + "\n\n")
	}
	for _, g := range all {
		line := fmt.Sprintf("- **%s %s, %s %s** (%s), %s, %s",
			g.Away, g.AwayScore, g.Home, g.HomeScore, g.League,
			g.Detail, g.Date.In(d.now().Location()).Format("Mon 2 Jan"))
		if !g.Completed {
			line = fmt.Sprintf("- **%s at %s** (%s), %s", g.Away, g.Home, g.League, g.Detail)
		}
		b.WriteString(line + "\n")
	}
	if len(distinctLeagues(all)) > 1 {
		b.WriteString("\nThat name belongs to a team in more than one league, so every match is listed.\n")
	}
	fmt.Fprintf(&b, "\nFrom ESPN, read %s.", d.now().Format("3:04 PM on 2 January"))

	text := b.String()
	var sources []Source
	for i, g := range all {
		if g.Link == "" || i >= 3 {
			continue
		}
		sources = append(sources, Source{
			URL:   g.Link,
			Title: fmt.Sprintf("%s at %s", g.Away, g.Home), Site: "espn.com",
		})
	}

	return &Result{
		Skill: "sports", Shape: "news", Text: text, Sources: sources,
		Elapsed: d.now().Sub(start).Round(10 * time.Millisecond).String(),
	}, nil
}

// targetDate reads the day a question is about. Zero means today, which is what
// the scoreboard returns without a date.
func targetDate(q string, d Deps) time.Time {
	l := strings.ToLower(q)
	now := d.now()
	switch {
	case containsAny(l, "last night", "yesterday"):
		return now.AddDate(0, 0, -1)
	case containsAny(l, "tonight", "today"):
		return now
	case containsAny(l, "this weekend", "saturday"):
		return now
	}
	return time.Time{}
}

// teamWords pulls the capitalised words that could be a team name. Common
// question words are dropped so "did the Panthers win" leaves "panthers".
func teamWords(q string) []string {
	skip := map[string]bool{
		"did": true, "the": true, "win": true, "won": true, "lose": true, "lost": true,
		"last": true, "night": true, "yesterday": true, "today": true, "tonight": true,
		"who": true, "what": true, "was": true, "score": true, "of": true, "game": true,
		"play": true, "playing": true, "beat": true, "against": true, "vs": true,
		"final": true, "and": true, "for": true, "their": true, "this": true, "weekend": true,
		"how": true, "many": true, "points": true, "did the": true, "we": true, "our": true,
	}
	var out []string
	for _, f := range strings.Fields(strings.ToLower(q)) {
		w := strings.Trim(f, ".,?!'\"")
		if len(w) < 3 || skip[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// matchTeam looks for the asked-about team in an event. Matching is on the
// nickname, the city, or the abbreviation, so "panthers", "carolina" and "car"
// all find the same game.
func matchTeam(ev espnEvent, words []string, league string) (gameResult, bool) {
	if len(ev.Competitions) == 0 || len(ev.Competitions[0].Competitors) < 2 {
		return gameResult{}, false
	}
	hit := false
	for _, c := range ev.Competitions[0].Competitors {
		fields := []string{
			strings.ToLower(c.Team.Name),
			strings.ToLower(c.Team.Location),
			strings.ToLower(c.Team.Abbrev),
			strings.ToLower(c.Team.DisplayName),
		}
		for _, w := range words {
			for _, f := range fields {
				if f == w || (len(w) > 4 && strings.Contains(f, w)) {
					hit = true
				}
			}
		}
	}
	if !hit {
		return gameResult{}, false
	}

	g := gameResult{
		League:    league,
		Completed: ev.Status.Type.Completed,
		Detail:    ev.Status.Type.ShortDetail,
	}
	if t, err := time.Parse("2006-01-02T15:04Z", ev.Date); err == nil {
		g.Date = t
	} else if t, err := time.Parse(time.RFC3339, ev.Date); err == nil {
		g.Date = t
	}
	for _, c := range ev.Competitions[0].Competitors {
		if c.HomeAway == "home" {
			g.Home, g.HomeScore = c.Team.DisplayName, c.Score
		} else {
			g.Away, g.AwayScore = c.Team.DisplayName, c.Score
		}
		if c.Winner {
			g.Winner = c.Team.DisplayName
		}
	}
	if len(ev.Links) > 0 {
		g.Link = ev.Links[0].Href
	}
	return g, true
}

// headline is the sentence that answers the question. A draw is a real result
// and not a missing winner, which is what an empty Winner field means in
// football and what printed a row of asterisks before.
func asksAboutPast(q string) bool {
	l := strings.ToLower(q)
	return containsAny(l, "last night", "yesterday", "did the", "did they",
		"who won", "final score", "was the score", "did we")
}

func headline(g gameResult, d Deps) string {
	when := g.Date.In(d.now().Location()).Format("Monday 2 January")

	if !g.Completed {
		if g.Detail != "" {
			return fmt.Sprintf("Not played yet. **%s at %s**, %s.", g.Away, g.Home, g.Detail)
		}
		return fmt.Sprintf("Not played yet. **%s at %s**.", g.Away, g.Home)
	}

	if g.Winner == "" {
		return fmt.Sprintf("**Drew %s to %s.** %s and %s, on %s.",
			g.HomeScore, g.AwayScore, g.Home, g.Away, when)
	}

	loser, winScore, loseScore := g.Away, g.HomeScore, g.AwayScore
	if g.Winner == g.Away {
		loser, winScore, loseScore = g.Home, g.AwayScore, g.HomeScore
	}
	return fmt.Sprintf("**%s beat %s, %s to %s**, on %s.",
		g.Winner, loser, winScore, loseScore, when)
}

func sortGames(g []gameResult) {
	for i := 1; i < len(g); i++ {
		for j := i; j > 0 && better(g[j], g[j-1]); j-- {
			g[j], g[j-1] = g[j-1], g[j]
		}
	}
}

// better ranks a finished game above a scheduled one, then by recency.
func better(a, b gameResult) bool {
	if a.Completed != b.Completed {
		return a.Completed
	}
	return a.Date.After(b.Date)
}

func distinctLeagues(g []gameResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range g {
		if !seen[x.League] {
			seen[x.League] = true
			out = append(out, x.League)
		}
	}
	return out
}

// leagueOrder puts the league a question names first, and otherwise sorts by
// how often a team gets asked about. The sweep stops at the first wave with a
// hit, so the order is what decides how many requests a question costs.
func leagueOrder(question string) []league {
	l := strings.ToLower(question)
	named := map[string]string{
		"premier league": "eng.1", "epl": "eng.1",
		"la liga": "esp.1", "serie a": "ita.1", "bundesliga": "ger.1",
		"ligue 1": "fra.1", "eredivisie": "ned.1", "primeira": "por.1",
		"championship": "eng.2", "mls": "usa.1",
		"champions league": "uefa.champions", "europa league": "uefa.europa",
		"world cup": "fifa.world",
		"nfl":       "nfl", "nba": "nba", "mlb": "mlb", "nhl": "nhl",
		"college football": "college-football",
	}
	var first string
	for word, path := range named {
		if strings.Contains(l, word) && len(word) > len(firstWord(named, first)) {
			first = path
		}
	}
	out := make([]league, 0, len(leagues))
	if first != "" {
		for _, lg := range leagues {
			if lg.path == first {
				out = append(out, lg)
			}
		}
	}
	for _, lg := range leagues {
		if lg.path != first {
			out = append(out, lg)
		}
	}
	return out
}

func firstWord(m map[string]string, path string) string {
	for w, p := range m {
		if p == path {
			return w
		}
	}
	return ""
}
