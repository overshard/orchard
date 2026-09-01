package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// What is being watched, where it is streaming, and whether it is any good.
//
// JustWatch's own front end talks to this GraphQL endpoint and it answers
// without a key, which is the only reason this panel exists: TMDB and Trakt
// both want one, and Rotten Tomatoes has had no public API since Fandango shut
// it down. This one hands over the tomatometer and the IMDb score together.
//
// It is unofficial, so it is the second most likely thing here to break after
// the pollen source, and its failure costs this panel and nothing else.
const (
	justwatchURL   = "https://apis.justwatch.com/graphql"
	streamingEvery = 3 * time.Hour
	streamingShown = 6

	// Asked for wider than shown, because titles with no score and titles on
	// nothing Isaac subscribes to both get dropped.
	streamingAsked = 24
)

// popularQuery is JustWatch's own popularTitles ranking, which is driven by
// what their users are actually doing. That is a better read on "making waves"
// than a release date is: a show in its fourth season is new news even though
// its release year is four years old.
const popularQuery = `query Popular($country: Country!, $first: Int!) {
  popularTitles(country: $country, first: $first, filter: {objectTypes: [MOVIE, SHOW]}) {
    edges { node {
      objectType
      content(country: $country, language: "en") {
        title
        originalReleaseYear
        scoring { imdbScore tomatoMeter }
        externalIds { imdbId }
      }
      offers(country: $country, platform: WEB) {
        monetizationType
        package { clearName }
      }
    } }
  }
}`

type Title struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Kind     string `json:"kind"`
	Year     int    `json:"year"`
	IMDb     string `json:"imdb"`
	Tomato   string `json:"tomato"`
	Hot      bool   `json:"hot"`
	Provider string `json:"provider"`

	// Each score graded on its own, so a good number reads as good without
	// having to be compared against the other five rows. The highlight on Hot
	// says both critics and the crowd rated it well, and it went unread as a
	// white row among orange ones until it was given a word.
	IMDbState   string `json:"imdb_state"`
	TomatoState string `json:"tomato_state"`
	Badge       string `json:"badge"`

	// The two scores averaged onto one bar, since the pair of them is what
	// anyone actually reads and a bar is quicker to scan down a column than two
	// numbers are. IMDb is out of ten so it is multiplied up first. Source says
	// which numbers went into it, because an average of one is not the same
	// claim as an average of two.
	Score     int    `json:"score"`
	ScoreBand string `json:"score_band"`
	ScoreFrom string `json:"score_from"`
}

type justwatchPayload struct {
	Data struct {
		PopularTitles struct {
			Edges []struct {
				Node struct {
					ObjectType string `json:"objectType"`
					Content    struct {
						Title   string `json:"title"`
						Year    int    `json:"originalReleaseYear"`
						Scoring struct {
							IMDb   float64 `json:"imdbScore"`
							Tomato int     `json:"tomatoMeter"`
						} `json:"scoring"`
						ExternalIDs struct {
							IMDbID string `json:"imdbId"`
						} `json:"externalIds"`
					} `json:"content"`
					Offers []struct {
						MonetizationType string `json:"monetizationType"`
						Package          struct {
							ClearName string `json:"clearName"`
						} `json:"package"`
					} `json:"offers"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"popularTitles"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func fetchStreaming(ctx context.Context, g *Guard) ([]Title, error) {
	body := map[string]any{
		"query": popularQuery,
		"variables": map[string]any{
			"country": "US",
			"first":   streamingAsked,
		},
	}

	var payload justwatchPayload
	if err := postJSON(ctx, g, "justwatch", justwatchURL, body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("justwatch: %s", payload.Errors[0].Message)
	}

	out := make([]Title, 0, streamingShown)
	for _, e := range payload.Data.PopularTitles.Edges {
		if len(out) == streamingShown {
			break
		}
		n := e.Node
		c := n.Content
		if strings.TrimSpace(c.Title) == "" || c.Scoring.IMDb == 0 {
			continue
		}

		names := make([]string, 0, len(n.Offers))
		for _, o := range n.Offers {
			// A subscription only. Isaac is not looking for what he can rent.
			if o.MonetizationType == "FLATRATE" {
				names = append(names, o.Package.ClearName)
			}
		}
		provider := pickProvider(names)
		if provider == "" {
			continue
		}

		t := Title{
			Name:     c.Title,
			Kind:     "FILM",
			Year:     c.Year,
			IMDb:     fmt.Sprintf("%.1f", c.Scoring.IMDb),
			Provider: provider,
		}
		if n.ObjectType == "SHOW" {
			t.Kind = "TV"
		}
		t.URL = imdbURL(c.ExternalIDs.IMDbID)
		t.IMDbState = gradeIMDb(c.Scoring.IMDb)
		if c.Scoring.Tomato > 0 {
			t.Tomato = fmt.Sprintf("%d%%", c.Scoring.Tomato)
			t.TomatoState = gradeTomato(c.Scoring.Tomato)
		}
		t.Score, t.ScoreFrom = combineScores(c.Scoring.IMDb, c.Scoring.Tomato)
		t.ScoreBand = gradeScore(t.Score)

		// Worth stopping on. Both numbers agreeing is the signal, since either
		// one alone is regularly wrong in a way the other is not.
		t.Hot = c.Scoring.IMDb >= imdbGood && c.Scoring.Tomato >= tomatoGood
		if t.Hot {
			t.Badge = "BOTH AGREE"
		}

		out = append(out, t)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("justwatch: nothing streaming with a score")
	}
	return out, nil
}

// The two lines a title has to clear to be highlighted. IMDb runs about a point
// higher than a tomatometer for the same film, which is why they are not the
// same number.
const (
	imdbGood   = 7.5
	tomatoGood = 85
)

func gradeIMDb(v float64) string {
	switch {
	case v == 0:
		return ""
	case v >= imdbGood:
		return "good"
	case v >= 6.5:
		return "fair"
	default:
		return "poor"
	}
}

// combineScores puts both ratings on the same nought to a hundred scale and
// averages them. A title with no tomatometer is its IMDb score alone rather
// than half of one, which would read as terrible for a film nobody reviewed.
func combineScores(imdb float64, tomato int) (int, string) {
	pct := imdb * 10
	from := "IMDB ONLY"
	if tomato > 0 {
		pct = (pct + float64(tomato)) / 2
		from = "IMDB+RT"
	}
	return int(math.Round(pct)), from
}

// The combined bar gets its own lines rather than either source's. Eighty is
// where the two good lines above average out, and sixty five sits between IMDb
// being fair at 6.5 and the tomatometer being fresh at 60.
func gradeScore(pct int) string {
	switch {
	case pct >= 80:
		return "good"
	case pct >= 65:
		return "fair"
	default:
		return "poor"
	}
}

func gradeTomato(v int) string {
	switch {
	case v >= tomatoGood:
		return "good"
	// Sixty is Rotten Tomatoes' own line between fresh and rotten.
	case v >= 60:
		return "fair"
	default:
		return "poor"
	}
}

// services is the subscription list in the order a title should be attributed
// to one. JustWatch returns every way to watch something, including a dozen
// resold channels, so "Paramount Plus Apple TV Channel" has to resolve to
// Paramount and a title on both Netflix and a reseller has to read as Netflix.
var services = []struct{ match, name string }{
	{"netflix", "NETFLIX"},
	{"hbo max", "HBO MAX"},
	{"max", "HBO MAX"},
	{"disney", "DISNEY+"},
	{"hulu", "HULU"},
	{"apple tv+", "APPLE TV+"},
	{"paramount", "PARAMOUNT+"},
	{"peacock", "PEACOCK"},
	{"amazon prime video", "PRIME"},
	{"prime video", "PRIME"},
	{"starz", "STARZ"},
	{"showtime", "SHOWTIME"},
	{"apple tv", "APPLE TV+"},
}

func pickProvider(names []string) string {
	// Sorted so the answer does not depend on the order JustWatch happened to
	// return the offers in.
	sort.Strings(names)

	for _, svc := range services {
		for _, name := range names {
			lower := strings.ToLower(name)
			// A resold channel is still that service, but "with Ads" is the
			// same service and should not become its own row.
			if strings.Contains(lower, svc.match) {
				return svc.name
			}
		}
	}
	return ""
}

// imdbURL builds the link for a title, and returns nothing for an id that is
// not one. A row labelled IMDB either opens IMDb or does not open at all, since
// a link that says one thing and goes somewhere else is worse than no link.
func imdbURL(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "tt") || len(id) < 3 {
		return ""
	}
	for _, r := range id[2:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return "https://www.imdb.com/title/" + id + "/"
}
