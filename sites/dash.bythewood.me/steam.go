package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// What is selling on Steam. The store's own front end API, no key, no account.
const (
	steamURL   = "https://store.steampowered.com/api/featuredcategories/?cc=us&l=en"
	steamEvery = time.Hour
	steamShown = 6

	// The store list flags hardware as type 0 like every game, so a Steam
	// Machine sits in the top sellers four times over. appdetails does say what
	// a thing is, so that is what the filter uses rather than guessing from the
	// price.
	detailsURL = "https://store.steampowered.com/api/appdetails?cc=us&l=en&appids="
	playersURL = "https://api.steampowered.com/ISteamUserStats/GetNumberOfCurrentPlayers/v1/?appid="

	// How far down the chart to look for six games, since hardware and the odd
	// bundle push real entries down it.
	steamCandidates = 12
)

type Game struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Price    string   `json:"price"`
	Discount int      `json:"discount"`
	Tags     []string `json:"tags"`
	Players  string   `json:"players"`
}

type steamPayload struct {
	TopSellers struct {
		Items []steamItem `json:"items"`
	} `json:"top_sellers"`
}

type steamItem struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	FinalPrice      int    `json:"final_price"`
	DiscountPercent int    `json:"discount_percent"`
}

type appDetails struct {
	Success bool `json:"success"`
	Data    struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		Genres []struct {
			Description string `json:"description"`
		} `json:"genres"`
	} `json:"data"`
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

	for _, it := range payload.TopSellers.Items {
		if len(out) == steamShown || looked >= steamCandidates {
			break
		}
		if it.ID == 0 || seen[it.ID] || strings.TrimSpace(it.Name) == "" {
			continue
		}
		seen[it.ID] = true
		looked++

		tags, ok := gameTags(ctx, g, it.ID)
		if !ok {
			// Either it is hardware, a bundle or a soundtrack, or the lookup
			// failed. Neither is worth a row.
			continue
		}

		out = append(out, Game{
			Name:     it.Name,
			URL:      fmt.Sprintf("https://store.steampowered.com/app/%d/", it.ID),
			Price:    steamPrice(it.FinalPrice),
			Discount: it.DiscountPercent,
			Tags:     tags,
			Players:  playersOnline(ctx, g, it.ID),
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("steam: no games in the top sellers list")
	}
	return out, nil
}

// gameTags reports the genres, and whether this is a game at all. appdetails is
// keyed by the app id as a string, which is why the response is a map.
func gameTags(ctx context.Context, g *Guard, id int) ([]string, bool) {
	var payload map[string]appDetails
	if err := getJSON(ctx, g, "steam", detailsURL+strconv.Itoa(id), &payload); err != nil {
		return nil, false
	}

	d, ok := payload[strconv.Itoa(id)]
	if !ok || !d.Success || d.Data.Type != "game" {
		return nil, false
	}

	// Three at most. The genre list runs to six and the rest is noise at this
	// width.
	tags := make([]string, 0, 3)
	for _, genre := range d.Data.Genres {
		if len(tags) == 3 {
			break
		}
		tags = append(tags, strings.ToUpper(genre.Description))
	}
	return tags, true
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

func steamPrice(cents int) string {
	if cents <= 0 {
		return "FREE"
	}
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}
