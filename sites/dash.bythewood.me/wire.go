package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// The day's major headlines from NPR and BBC, newest first, and nothing else.
// It was a market news panel before and then a four bucket balance across six
// outlets, and Isaac's read on both was that they were confusing to look at,
// so this reads like a wire and is sorted the way a wire is.
//
// These three feeds are the editor picked ones rather than a topic list, since
// what lands on a front page is the closest thing an RSS feed has to a signal
// that a story is big. BBC's own top stories feed is the UK edition and leads
// on the Budget and Reform UK, so the two regional editions are the American
// reader's version of it.
//
// The endpoint is the guard bucket, so one outlet going down or answering 429
// costs its own headlines and leaves the other its budget.
var wireFeeds = []struct{ name, endpoint, url string }{
	{"NPR", "npr", "https://feeds.npr.org/1001/rss.xml"},
	{"BBC", "bbc", "https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml"},
	{"BBC", "bbc", "https://feeds.bbci.co.uk/news/world/rss.xml"},
}

const (
	wireEvery = 10 * time.Minute
	wireShown = 10

	// At most this many rows from one outlet. BBC supplies two of the three
	// feeds and posts about twice as often as NPR does, so without this a busy
	// afternoon is a column of BBC.
	wirePerSource = 6

	// Both outlets post enough that ten rows never reach back this far, so
	// this is the floor that keeps a dead feed off the panel rather than a
	// window anything is chosen inside.
	wireMaxAge = 2 * 24 * time.Hour
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

// dated is a headline with the timestamp the sort needs, which the panel shows
// only as an age.
type dated struct {
	Headline
	at time.Time
}

func fetchWire(ctx context.Context, g *Guard, now time.Time) ([]Headline, error) {
	seen := map[string]bool{}
	var all []dated

	for _, feed := range wireFeeds {
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
			if promotional(title) || sidebar(title) || clip(it.Link) {
				continue
			}

			key := it.GUID
			if key == "" {
				key = it.Link
			}
			// BBC's world and US feeds overlap by about half, and the same
			// story reaches two outlets under headlines that differ by a word,
			// so the title is deduped as well as the identifier.
			if seen[key] || seen[titleKey(title)] {
				continue
			}
			seen[key], seen[titleKey(title)] = true, true

			at, err := parseRSSTime(it.PubDate)
			if err != nil || now.Sub(at) > wireMaxAge {
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
		return nil, fmt.Errorf("wire: nothing fresh in %d feeds", len(wireFeeds))
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	return pick(dedupe(all)), nil
}

// dedupe drops the second telling of a story two outlets worded differently,
// which the title key cannot catch. It runs over the sorted list so the copy
// that survives is the newer one.
func dedupe(all []dated) []dated {
	kept := make([]dated, 0, len(all))
	keys := make([][]string, 0, len(all))

	for _, d := range all {
		w := significant(d.Title)
		if slices.ContainsFunc(keys, func(k []string) bool { return sameStory(k, w) }) {
			continue
		}
		kept = append(kept, d)
		keys = append(keys, w)
	}
	return kept
}

// pick takes the newest rows in order, holding one outlet to its cap, and then
// gives the leftover slots back to whatever the cap passed over, since a short
// panel is worse than a lopsided one. Both passes mark rows rather than emit
// them, because the panel still has to read newest first afterwards.
func pick(all []dated) []Headline {
	taken := make([]bool, len(all))
	perSource := map[string]int{}
	count := 0

	for i, d := range all {
		if count == wireShown {
			break
		}
		if perSource[d.Source] == wirePerSource {
			continue
		}
		taken[i], perSource[d.Source], count = true, perSource[d.Source]+1, count+1
	}

	for i := range all {
		if count == wireShown {
			break
		}
		if !taken[i] {
			taken[i], count = true, count+1
		}
	}

	out := make([]Headline, 0, count)
	for i, d := range all {
		if taken[i] {
			out = append(out, d.Headline)
		}
	}
	return out
}

// Four words in common is enough to be one story, and rare enough that two
// stories about the same person on the same day do not collide.
const sameStoryWords = 4

// The words long enough to carry a story and common enough to say nothing.
var filler = []string{"about", "after", "against", "amid", "been", "before", "could", "does", "during", "from", "have", "into", "more", "over", "said", "says", "than", "that", "their", "them", "then", "there", "these", "they", "this", "were", "what", "when", "which", "will", "with", "would", "your"}

// significant reduces a headline to the words worth comparing across outlets,
// which is the long ones with the filler taken out. They are cut to a stem
// because one outlet writes charged where the other writes charges.
func significant(title string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) < 4 || slices.Contains(filler, w) {
			continue
		}
		if len(w) > 5 {
			w = w[:5]
		}
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

func sameStory(a, b []string) bool {
	shared := 0
	for _, w := range a {
		if slices.Contains(b, w) {
			shared++
		}
	}
	return shared >= sameStoryWords
}

// promoted is the genre Isaac asked to keep off the panel: advertising that
// reads as news in a feed. Matching on the headline is the only lever
// available, since none of these feeds mark it.
var promoted = []string{
	"promo code",
	"sponsored",
	"deal of the day",
	"prime day",
	"black friday",
	"sign up for",
	"subscribe to",
	"newsletter",
}

// sidebars are the formats that are a page rather than a story, a minute of
// footage or a rolling feed, and ten rows cannot spare a slot for one.
// A question mark anywhere in a headline is the explainer beside the story.
var sidebars = []string{
	"watch:",
	"watch live",
	"listen:",
	"in pictures",
	"in photos",
	"video:",
	"live updates",
	"your questions answered",
}

// clip drops BBC's video and live pages, which its news feeds mix in with the
// stories.
func clip(link string) bool {
	return strings.Contains(link, "/news/videos/") || strings.Contains(link, "/news/live/")
}

func promotional(title string) bool {
	lower := strings.ToLower(title)
	return slices.ContainsFunc(promoted, func(p string) bool { return strings.Contains(lower, p) })
}

func sidebar(title string) bool {
	lower := strings.ToLower(title)
	if strings.Contains(lower, "?") {
		return true
	}
	return slices.ContainsFunc(sidebars, func(p string) bool { return strings.Contains(lower, p) })
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
	req.Header.Set("User-Agent", feedAgent)
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
