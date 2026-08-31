package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The day's macro headlines, which is the half of the news Isaac reads markets
// for. Two CNBC feeds merged: Markets is the freshest and Economy carries the
// rate and inflation stories that actually move a quarterly position.
//
// MarketWatch was the obvious first choice and is not usable. Its top stories
// feed is mostly personal finance advice columns, and both of its markets feeds
// are abandoned: the newest item in mw_marketpulse is from July 2025. A stale
// feed is worse than no feed, because nothing about the page would say so.
var marketFeeds = []struct{ name, url string }{
	{"MARKETS", "https://www.cnbc.com/id/15839069/device/rss/rss.html"},
	{"ECONOMY", "https://www.cnbc.com/id/10000664/device/rss/rss.html"},
}

const (
	marketNewsEvery = 10 * time.Minute
	marketNewsShown = 7

	// CNBC's feeds mix the day's stories with ones three weeks old, so this is
	// wide enough to fill the panel and the age on every row is what says how
	// fresh any of it actually is.
	marketNewsMaxAge = 8 * 24 * time.Hour
)

// Headline is one story on the wire panel.
type Headline struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source"`
	Age    string `json:"age"`
}

type rssFeed struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
			GUID    string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

func fetchMarketNews(ctx context.Context, g *Guard, now time.Time) ([]Headline, error) {
	seen := map[string]bool{}
	type dated struct {
		Headline
		at time.Time
	}
	var all []dated

	for _, feed := range marketFeeds {
		items, err := fetchRSS(ctx, g, feed.url)
		if err != nil {
			// One dead feed costs its own headlines and not the panel.
			continue
		}

		for _, it := range items.Channel.Items {
			title := strings.TrimSpace(it.Title)
			if title == "" || it.Link == "" {
				continue
			}

			key := it.GUID
			if key == "" {
				key = it.Link
			}
			// The two feeds overlap by design, so the same story arrives twice
			// and the first feed listed wins.
			if seen[key] {
				continue
			}
			seen[key] = true

			at, err := parseRSSTime(it.PubDate)
			if err != nil || now.Sub(at) > marketNewsMaxAge {
				continue
			}

			all = append(all, dated{
				Headline: Headline{
					Title:  title,
					URL:    it.Link,
					Source: feed.name,
					Age:    humanAge(at, now),
				},
				at: at,
			})
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("market news: nothing fresh in %d feeds", len(marketFeeds))
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })

	out := make([]Headline, 0, marketNewsShown)
	for _, d := range all {
		if len(out) == marketNewsShown {
			break
		}
		out = append(out, d.Headline)
	}
	return out, nil
}

// fetchRSS is the XML twin of getJSON, guarded the same way. It is separate
// rather than a flag on that function because the decode differs and nothing
// else about the two is worth sharing.
func fetchRSS(ctx context.Context, g *Guard, url string) (*rssFeed, error) {
	// Two feeds back to back, so this has to wait out the pace between them
	// rather than lose the second one.
	if err := g.Reserve(ctx, "cnbc"); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")

	resp, err := client.Do(req)
	if err != nil {
		g.Fail("cnbc", 0, 0)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail("cnbc", resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return nil, fmt.Errorf("cnbc: http %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		g.Fail("cnbc", resp.StatusCode, 0)
		return nil, fmt.Errorf("cnbc: %w", err)
	}

	g.Succeed("cnbc")
	return &feed, nil
}

// parseRSSTime reads the several date formats RSS feeds actually ship, rather
// than the one RFC 822 says they should.
func parseRSSTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
		"Mon, 2 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 -0700",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", v)
}
