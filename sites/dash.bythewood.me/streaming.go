package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// What's being watched, where, and whether it's any good, off the GraphQL
// endpoint JustWatch's front end calls. It needs no key where TMDB and Trakt
// do, and hands over the tomatometer and the IMDb score together.
const (
	justwatchURL   = "https://apis.justwatch.com/graphql"
	streamingEvery = 3 * time.Hour
	streamingShown = 6

	// Asked for wider than shown, because titles with no score and titles on
	// nothing Isaac subscribes to both get dropped.
	streamingAsked = 24
)

// popularQuery is JustWatch's popularTitles ranking, driven by what their users
// are doing, which reads making waves better than a release date. The default
// sort barely moves for months, so this asks for the last seven days.
const popularQuery = `query Popular($country: Country!, $first: Int!) {
  popularTitles(country: $country, first: $first, sortBy: POPULAR_7_DAYS, filter: {objectTypes: [MOVIE, SHOW]}) {
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
	Provider string `json:"provider"`

	// Each score graded on its own, so a good number reads as good without
	// comparing it to the other rows.
	IMDbState   string `json:"imdb_state"`
	TomatoState string `json:"tomato_state"`

	// The two scores averaged onto one bar, quicker to scan than two numbers,
	// with IMDb multiplied up from ten. Source says which went in, since an
	// average of one isn't the claim an average of two is.
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

		out = append(out, t)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("justwatch: nothing streaming with a score")
	}
	return out, nil
}

// Where each score stops being fair and starts being good. IMDb runs about a
// point higher than a tomatometer for the same film, which is why they are not
// the same number.
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
// averages them. A title with no tomatometer is its IMDb score alone, since
// half of one would read as terrible for a film nobody reviewed.
func combineScores(imdb float64, tomato int) (int, string) {
	pct := imdb * 10
	from := "IMDB ONLY"
	if tomato > 0 {
		pct = (pct + float64(tomato)) / 2
		from = "IMDB+RT"
	}
	return int(math.Round(pct)), from
}

// The combined bar gets its own lines instead of either source's. Eighty is
// where the two good lines above average out, and sixty five sits between
// IMDb's fair at 6.5 and the tomatometer's fresh at 60.
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

// services is the subscription list in the order a title is attributed to one.
// JustWatch returns resold channels too, so "Paramount Plus Apple TV Channel"
// is Paramount and a title on Netflix and a reseller reads as Netflix.
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
