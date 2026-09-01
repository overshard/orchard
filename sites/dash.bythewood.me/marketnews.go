package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// The day's macro headlines, which is the half of the news Isaac reads markets
// for. Four outlets rather than one, because CNBC alone leans hard on the stock
// pick listicle and a panel of nothing but those is a worse read than a panel
// with an outlet name on every row.
//
// The endpoint is the guard bucket, so one outlet going down or answering 429
// costs its own headlines and leaves the other three their budgets.
//
// MarketWatch and Yahoo were both tried and neither is usable. MarketWatch's
// top stories feed is personal finance advice columns, and Yahoo's rssindex has
// not published since November 2024. Checking the newest pubDate before
// trusting a feed is worth doing every time, since nothing about a stale feed
// announces itself.
var marketFeeds = []struct{ name, endpoint, url string }{
	{"CNBC", "cnbc", "https://www.cnbc.com/id/15839069/device/rss/rss.html"},
	{"CNBC", "cnbc", "https://www.cnbc.com/id/10000664/device/rss/rss.html"},
	{"BBC", "bbc", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	{"SA", "seekingalpha", "https://seekingalpha.com/market_currents.xml"},
	{"FED", "fed", "https://www.federalreserve.gov/feeds/press_all.xml"},
}

const (
	marketNewsEvery = 10 * time.Minute
	marketNewsShown = 8

	// At most this many rows from any one outlet. BBC publishes forty business
	// stories a day and CNBC publishes thirty, so sorting the merged list by
	// time alone hands the whole panel to whoever posted most recently and the
	// other three never appear.
	marketNewsPerSource = 3

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
		items, err := fetchRSS(ctx, g, feed.endpoint, feed.url)
		if err != nil {
			// One dead feed costs its own headlines and not the panel.
			continue
		}

		for _, it := range items.Channel.Items {
			title := strings.TrimSpace(it.Title)
			if title == "" || it.Link == "" {
				continue
			}
			if promotional(title) {
				continue
			}

			key := it.GUID
			if key == "" {
				key = it.Link
			}
			// The two CNBC feeds overlap by design, and the same story reaches
			// two outlets under headlines that differ by a word, so the title
			// is deduped as well as the identifier.
			if seen[key] || seen[titleKey(title)] {
				continue
			}
			seen[key], seen[titleKey(title)] = true, true

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
	perSource := map[string]int{}
	for _, d := range all {
		if len(out) == marketNewsShown {
			break
		}
		if perSource[d.Source] == marketNewsPerSource {
			continue
		}
		perSource[d.Source]++
		out = append(out, d.Headline)
	}

	// The cap can leave the panel short on a quiet morning when only one outlet
	// has posted, so anything still missing is filled from what was passed over.
	for _, d := range all {
		if len(out) == marketNewsShown {
			break
		}
		if perSource[d.Source] > marketNewsPerSource {
			continue
		}
		if slices.ContainsFunc(out, func(h Headline) bool { return h.URL == d.URL }) {
			continue
		}
		perSource[d.Source]++
		out = append(out, d.Headline)
	}
	return out, nil
}

// promoted is the genre Isaac asked to keep off the panel: the stock pick
// listicle and the affiliate placement, which read as news in a feed and are
// advertising with a byline. Matching on the headline is the only lever
// available, since CNBC serves these off the same /2026/ path as real
// reporting and marks them nowhere in the item.
var promoted = []string{
	"top wall street analysts",
	"analysts suggest these",
	"analysts believe in",
	"stocks to buy",
	"best stocks",
	"top picks",
	"top stock picks",
	"buy these",
	"these dividend stocks",
	"here are hedge funds",
	"our top",
	"promo code",
	"sponsored",
	"deal of the day",
	"prime day",
	"black friday",
	"% off",
	"sign up for",
	"subscribe to",
	"newsletter",
}

// "these 3 stocks", "these 5 names", and the rest of the same shape.
var pickList = regexp.MustCompile(`(?i)these \d+ (stocks|names|picks|companies|funds)`)

func promotional(title string) bool {
	lower := strings.ToLower(title)
	for _, p := range promoted {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return pickList.MatchString(lower)
}

// titleKey reduces a headline to what two outlets covering one story share, so
// punctuation and a trailing outlet name do not defeat the dedupe.
func titleKey(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	key := b.String()
	// Long enough to be the story and short enough that a differing tail does
	// not make two copies of it look like two stories.
	if len(key) > 60 {
		key = key[:60]
	}
	return key
}

// fetchRSS is the XML twin of getJSON, guarded the same way. It is separate
// rather than a flag on that function because the decode differs and nothing
// else about the two is worth sharing.
func fetchRSS(ctx context.Context, g *Guard, endpoint, url string) (*rssFeed, error) {
	// Feeds are fetched back to back, so this has to wait out the pace between
	// two on the same endpoint rather than lose the second one.
	if err := g.Reserve(ctx, endpoint); err != nil {
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
		g.Fail(endpoint, 0, 0)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return nil, fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return nil, fmt.Errorf("%s: %w", endpoint, err)
	}

	g.Succeed(endpoint)
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
