package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Two feeds, two reasons. Hacker News is what Isaac asked for, and Lobsters
// sits next to it so the panel is not one community's view of the same day.
//
// Hacker News comes from Algolia rather than from the official Firebase API.
// Firebase hands back 500 bare story ids and charges one HTTP request per story
// to resolve each into a title, which is 30 requests for a panel that shows 10.
// Algolia's front_page search returns all of them, scored and with comment
// counts, in a single response.
const (
	hackerNewsURL = "https://hn.algolia.com/api/v1/search?tags=front_page"
	lobstersURL   = "https://lobste.rs/hottest.json"

	// Enough to fill the panel with a little room to drop anything malformed.
	storiesShown = 10
)

// Story is one row, from either feed.
type Story struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	Comments string `json:"comments"`
	Points   int    `json:"points"`
	Count    int    `json:"count"`
	Age      string `json:"age"`
}

type algoliaPayload struct {
	Hits []struct {
		ObjectID    string `json:"objectID"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		Points      int    `json:"points"`
		NumComments int    `json:"num_comments"`
		CreatedAtI  int64  `json:"created_at_i"`
	} `json:"hits"`
}

func fetchHackerNews(ctx context.Context, g *Guard, now time.Time) ([]Story, error) {
	var payload algoliaPayload
	if err := getJSON(ctx, g, "algolia", hackerNewsURL, &payload); err != nil {
		return nil, err
	}

	stories := make([]Story, 0, len(payload.Hits))
	for _, h := range payload.Hits {
		if h.Title == "" {
			continue
		}
		link := h.URL
		discuss := "https://news.ycombinator.com/item?id=" + h.ObjectID
		// An Ask HN or a Show HN with no link is its own discussion, so the
		// title has to point somewhere rather than nowhere.
		if link == "" {
			link = discuss
		}
		stories = append(stories, Story{
			Title:    h.Title,
			URL:      link,
			Host:     hostOf(link),
			Comments: discuss,
			Points:   h.Points,
			Count:    h.NumComments,
			Age:      humanAge(time.Unix(h.CreatedAtI, 0), now),
		})
	}

	// Algolia returns the front page in its own order, which is not by score.
	// Sorting here means the two feeds are ranked the same way and the panel
	// reads consistently.
	sort.SliceStable(stories, func(i, j int) bool { return stories[i].Points > stories[j].Points })
	return trim(stories), nil
}

type lobstersStory struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	ShortIDURL   string `json:"short_id_url"`
	CommentsURL  string `json:"comments_url"`
	Score        int    `json:"score"`
	CommentCount int    `json:"comment_count"`
	CreatedAt    string `json:"created_at"`
}

func fetchLobsters(ctx context.Context, g *Guard, now time.Time) ([]Story, error) {
	var payload []lobstersStory
	if err := getJSON(ctx, g, "lobsters", lobstersURL, &payload); err != nil {
		return nil, err
	}

	stories := make([]Story, 0, len(payload))
	for _, s := range payload {
		if s.Title == "" {
			continue
		}
		link := s.URL
		discuss := s.CommentsURL
		if discuss == "" {
			discuss = s.ShortIDURL
		}
		if link == "" {
			link = discuss
		}

		age := ""
		// Lobsters writes RFC 3339 with an offset. A value that will not parse
		// costs the row its age and nothing else.
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			age = humanAge(t, now)
		}

		stories = append(stories, Story{
			Title:    s.Title,
			URL:      link,
			Host:     hostOf(link),
			Comments: discuss,
			Points:   s.Score,
			Count:    s.CommentCount,
			Age:      age,
		})
	}

	sort.SliceStable(stories, func(i, j int) bool { return stories[i].Points > stories[j].Points })
	return trim(stories), nil
}

func trim(s []Story) []Story {
	if len(s) > storiesShown {
		return s[:storiesShown]
	}
	return s
}

// hostOf is what the row shows next to the title, so it is the readable host
// and not the authority: no port, no leading www.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func humanAge(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
