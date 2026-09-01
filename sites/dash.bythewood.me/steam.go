package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What is selling on Steam. The store's own front end API, no key, no account.
const (
	// The store search behind the top sellers tab. category1=998 is Steam's
	// own Games filter, applied before the list is cut, which keeps out the
	// hardware and DLC the store flags type 0 the same as a game.
	steamURL   = "https://store.steampowered.com/search/results/?query&start=0&count=50&filter=topsellers&category1=998&cc=us&l=en&json=1"
	steamEvery = time.Hour
	steamShown = 6

	// The search answers a name and an image and no price, so appdetails is
	// where the money and the genres come from. It also says what a thing is,
	// which catches whatever the store filter let through.
	detailsURL = "https://store.steampowered.com/api/appdetails?cc=us&l=en&appids="
	playersURL = "https://api.steampowered.com/ISteamUserStats/GetNumberOfCurrentPlayers/v1/?appid="

	// The store's own review summary. num_per_page=0 asks for the counts
	// without the reviews themselves, which is the whole of what a row needs
	// and a fraction of the response.
	reviewsURL = "https://store.steampowered.com/appreviews/%d?json=1&language=all&purchase_type=all&num_per_page=0"

	// How far down the chart to look for six games, which is headroom for a
	// lookup that fails rather than for the chart itself.
	steamCandidates = 12
)

// The search hands back a capsule image rather than an app id, and the id is
// the only part of the URL that is stable across the several CDN hosts and
// path shapes Valve has used.
var steamAppID = regexp.MustCompile(`/apps/(\d+)/`)

type Game struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Price    string   `json:"price"`
	Discount int      `json:"discount"`
	Tags     []string `json:"tags"`
	Players  string   `json:"players"`

	// What Steam's own users make of it. Rating is the percentage positive and
	// Verdict is Valve's word for that percentage, which is the label anyone
	// who uses the store already reads. Reviews is how much it rests on, since
	// 100% of nine reviews and 94% of four hundred thousand are different
	// claims.
	Rating   int    `json:"rating"`
	Verdict  string `json:"verdict"`
	Reviews  string `json:"reviews"`
	Reviewed bool   `json:"reviewed"`
}

type steamPayload struct {
	Items []struct {
		Name string `json:"name"`
		Logo string `json:"logo"`
	} `json:"items"`
}

type appDetails struct {
	Success bool `json:"success"`
	Data    struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		IsFree bool   `json:"is_free"`
		Price  *struct {
			Final           int `json:"final"`
			DiscountPercent int `json:"discount_percent"`
		} `json:"price_overview"`
		Genres []struct {
			Description string `json:"description"`
		} `json:"genres"`
	} `json:"data"`
}

type reviewSummary struct {
	Success int `json:"success"`
	Summary struct {
		Score    int    `json:"review_score"`
		Desc     string `json:"review_score_desc"`
		Positive int    `json:"total_positive"`
		Total    int    `json:"total_reviews"`
	} `json:"query_summary"`
}

type playerCount struct {
	Response struct {
		PlayerCount int `json:"player_count"`
		Result      int `json:"result"`
	} `json:"response"`
}

func fetchSteam(ctx context.Context, g *Guard) ([]Game, error) {
	var payload steamPayload
	if err := getJSON(ctx, g, "steam", steamURL, &payload); err != nil {
		return nil, err
	}

	seen := map[int]bool{}
	out := make([]Game, 0, steamShown)
	looked := 0

	for _, it := range payload.Items {
		if len(out) == steamShown || looked >= steamCandidates {
			break
		}

		m := steamAppID.FindStringSubmatch(it.Logo)
		if m == nil {
			continue
		}
		id, err := strconv.Atoi(m[1])
		if err != nil || id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		looked++

		d, err := details(ctx, g, id)
		if err != nil {
			// The four Steam endpoints share one breaker, so a refusal here
			// refuses every lookup left in the loop. Carrying on would hand
			// back the two rows it has as though that were the chart.
			break
		}
		if d == nil {
			// Real entry, not a game. The store filter usually catches these.
			continue
		}

		name := strings.TrimSpace(d.Data.Name)
		if name == "" {
			name = strings.TrimSpace(it.Name)
		}
		if name == "" {
			continue
		}

		game := Game{
			Name:     name,
			URL:      fmt.Sprintf("https://store.steampowered.com/app/%d/", id),
			Price:    steamPrice(d),
			Discount: discount(d),
			Tags:     gameTags(d),
			Players:  playersOnline(ctx, g, id),
		}
		rating(ctx, g, id, &game)
		out = append(out, game)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("steam: no games in the top sellers list")
	}
	return out, nil
}

// keepSteam reports whether a fresh poll should replace what is on screen. A
// short list means the lookups gave out partway rather than the store selling
// out, and the poll is hourly, so two rows would sit there for the rest of it.
func keepSteam(fresh, shown []Game) bool {
	return len(fresh) >= steamShown || len(fresh) >= len(shown)
}

// details looks one app up. A nil payload with a nil error means the lookup
// landed and the thing is not a game, which costs a row. An error means the
// lookup did not land at all, which is a different answer and one the caller
// must not read as an empty chart.
func details(ctx context.Context, g *Guard, id int) (*appDetails, error) {
	var payload map[string]appDetails
	if err := getJSON(ctx, g, "steam", detailsURL+strconv.Itoa(id), &payload); err != nil {
		return nil, err
	}

	// appdetails is keyed by the app id as a string, which is why the response
	// is a map. It answers success false both for things that are not apps and
	// for an app the store will not show this region, and neither is a row.
	d, ok := payload[strconv.Itoa(id)]
	if !ok || !d.Success || d.Data.Type != "game" {
		return nil, nil
	}
	return &d, nil
}

// gameTags reports the genres, three at most. The list runs to six and the
// rest is noise at this width.
func gameTags(d *appDetails) []string {
	tags := make([]string, 0, 3)
	for _, genre := range d.Data.Genres {
		if len(tags) == 3 {
			break
		}
		tags = append(tags, strings.ToUpper(genre.Description))
	}
	return tags
}

func discount(d *appDetails) int {
	if d.Data.Price == nil {
		return 0
	}
	return d.Data.Price.DiscountPercent
}

// rating fills in the review summary, and leaves the row alone if it cannot.
// A game with almost no reviews gets none: Valve does not call a percentage a
// verdict under ten of them either, and "100% POSITIVE" off three reviews is
// the most misleading thing this panel could print.
func rating(ctx context.Context, g *Guard, id int, game *Game) {
	var payload reviewSummary
	if err := getJSON(ctx, g, "steam", fmt.Sprintf(reviewsURL, id), &payload); err != nil {
		return
	}

	q := payload.Summary
	if payload.Success != 1 || q.Total < 10 {
		return
	}

	game.Rating = int(math.Round(float64(q.Positive) / float64(q.Total) * 100))
	game.Verdict = strings.ToUpper(q.Desc)
	game.Reviews = compactCount(q.Total)
	game.Reviewed = true
}

// playersOnline is a nice to have, so a failure costs the number and not the
// row it would have sat on.
func playersOnline(ctx context.Context, g *Guard, id int) string {
	var payload playerCount
	if err := getJSON(ctx, g, "steam", playersURL+strconv.Itoa(id), &payload); err != nil {
		return ""
	}
	if payload.Response.Result != 1 || payload.Response.PlayerCount <= 0 {
		return ""
	}
	return compactCount(payload.Response.PlayerCount)
}

// compactCount keeps a player count to four characters, since the column is
// narrow and nobody needs the last three digits of 431,908.
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fK", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return strconv.Itoa(n)
	}
}

// steamPrice reads the money off appdetails. A game that is neither free nor
// priced has not been released yet, and the top sellers chart carries plenty
// of those on preorder.
func steamPrice(d *appDetails) string {
	if d.Data.IsFree {
		return "FREE"
	}
	if d.Data.Price == nil || d.Data.Price.Final <= 0 {
		return "TBA"
	}
	return fmt.Sprintf("$%.2f", float64(d.Data.Price.Final)/100)
}
