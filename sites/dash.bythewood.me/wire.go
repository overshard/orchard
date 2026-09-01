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

// The day's news, in the four kinds Isaac reads for: US general, US politics,
// world, and macro. The panel balances across those four rather than across
// outlets, because a panel that shows whoever posted most recently is a panel
// of one bucket on a busy news day.
//
// CNBC, Seeking Alpha and BBC business are the obvious additions here and none
// of them belong. The first two run mostly stock pick listicles and affiliate
// placements with a byline on them, and BBC business is written for a UK reader
// and leads on the Budget and the Bank of England more often than on anything
// American.
//
// AP and Reuters would be the obvious wire services to put here and neither is
// reachable. AP answers 403 to every feed path it used to serve and Reuters
// retired its agency feed, so the only route to either is a Google News search
// feed whose links land on a Google interstitial rather than the outlet.
//
// The endpoint is the guard bucket, so one outlet going down or answering 429
// costs its own headlines and leaves the others their budgets.
//
// Order matters, because the dedupe is first past the post and a story in two
// feeds keeps the bucket of whichever ran first. Specific topics are listed
// ahead of the generalists so that NPR's politics copy is tagged POL and NPR
// News supplies what is left, rather than the other way round.
var wireFeeds = []struct{ name, endpoint, url, bucket string }{
	{"NPR", "npr", "https://feeds.npr.org/1014/rss.xml", bucketPolitics},
	{"NPR", "npr", "https://feeds.npr.org/1006/rss.xml", bucketMacro},
	{"BBC", "bbc", "https://feeds.bbci.co.uk/news/world/rss.xml", bucketWorld},
	{"BBC", "bbc", "https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml", bucketUS},
	{"NPR", "npr", "https://feeds.npr.org/1001/rss.xml", bucketUS},
	{"PBS", "pbs", "https://www.pbs.org/newshour/feeds/rss/headlines", bucketUS},
}

// The four kinds, in the order the panel fills them.
const (
	bucketUS       = "US"
	bucketPolitics = "POL"
	bucketWorld    = "WORLD"
	bucketMacro    = "MKT"
)

var buckets = []string{bucketUS, bucketPolitics, bucketWorld, bucketMacro}

// press_monetary rather than press_all. The all feed is four fifths enforcement
// actions against individual bank employees and approvals of merger
// applications, which is how a Fed row on this panel used to read. The monetary
// feed is FOMC statements, the minutes, and the discount rate, and nothing else.
const fedFeedURL = "https://www.federalreserve.gov/feeds/press_monetary.xml"

const (
	wireEvery = 10 * time.Minute
	wireShown = 8

	// At most this many rows from any one outlet. NPR supplies three of the six
	// feeds, so without this the balance across buckets still comes back as a
	// column of NPR.
	wirePerSource = 3

	// Old enough to carry a slow bucket through a quiet weekend, since NPR
	// Business posts about twice a day, and short enough that nothing on a news
	// panel is stale. The age on every row is what says how fresh any of it is.
	wireMaxAge = 4 * 24 * time.Hour

	// The Fed posts to the monetary feed a couple of times a month and a rate
	// decision is the thing in effect until the next one, so its row is worth
	// keeping long after a news headline would have aged out. Past this the
	// panel is better off giving the slot back to the news.
	fedMaxAge = 90 * 24 * time.Hour
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

// dated is a headline with the timestamp the sort needs and the bucket the
// balance needs, neither of which the panel shows.
type dated struct {
	Headline
	at     time.Time
	bucket string
}

func fetchWire(ctx context.Context, g *Guard, now time.Time) ([]Headline, error) {
	seen := map[string]bool{}
	var all []dated

	// The Fed leads the panel rather than competing for it. It posts twice a
	// month against outlets that post hourly, so anything that ranks these by
	// time together gives the Fed no row at all.
	out := make([]Headline, 0, wireShown)
	if fed, ok := fetchFed(ctx, g, now); ok {
		seen[titleKey(fed.Title)] = true
		out = append(out, fed)
	}

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
			if promotional(title) || clip(it.Link) {
				continue
			}

			key := it.GUID
			if key == "" {
				key = it.Link
			}
			// NPR's news and politics feeds overlap by more than half, and the
			// same story reaches two outlets under headlines that differ by a
			// word, so the title is deduped as well as the identifier.
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
				at:     at,
				bucket: feed.bucket,
			})
		}
	}

	if len(all) == 0 {
		if len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("wire: nothing fresh in %d feeds", len(wireFeeds))
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	return balance(all, out), nil
}

// balance fills the panel a bucket at a time rather than in time order, so all
// four kinds appear on a day when one of them is the whole news cycle.
//
// Within a bucket the newest story from the least represented outlet wins.
// Newest alone is not enough, because three of these feeds carry US news and
// the slowest of them never places against the other two.
//
// pinned rows lead the panel and are not part of the balance. Everything chosen
// after them is sorted back into time order at the end, because a wire that
// reads down in the order four buckets happened to fill is not a wire.
func balance(all []dated, pinned []Headline) []Headline {
	taken := make([]bool, len(all))
	perSource := map[string]int{}
	var picked []dated

	take := func(i int) {
		taken[i] = true
		perSource[all[i].Source]++
		picked = append(picked, all[i])
	}

	// best is the newest item in a bucket from the outlet with the fewest rows
	// so far, or -1 when the bucket has nothing left to give.
	best := func(b string) int {
		found, fewest := -1, 0
		for i, d := range all {
			if taken[i] || d.bucket != b || perSource[d.Source] == wirePerSource {
				continue
			}
			if found == -1 || perSource[d.Source] < fewest {
				found, fewest = i, perSource[d.Source]
			}
		}
		return found
	}

	for len(picked)+len(pinned) < wireShown {
		filled := false
		for _, b := range buckets {
			if len(picked)+len(pinned) == wireShown {
				break
			}
			if i := best(b); i >= 0 {
				take(i)
				filled = true
			}
		}
		// Every bucket is either empty or capped, so another pass takes nothing
		// and this would spin.
		if !filled {
			break
		}
	}

	// A quiet morning or a run of one outlet can leave the panel short, so
	// anything still missing is filled from what the cap passed over.
	for i := range all {
		if len(picked)+len(pinned) == wireShown {
			break
		}
		if !taken[i] {
			take(i)
		}
	}

	sort.SliceStable(picked, func(i, j int) bool { return picked[i].at.After(picked[j].at) })

	out := make([]Headline, 0, wireShown)
	out = append(out, pinned...)
	for _, d := range picked {
		out = append(out, d.Headline)
	}
	return out
}

// fetchFed returns the newest monetary policy release, which is the one still
// in effect. A failure here is not worth a log line of its own, since the
// guard already records it and the panel simply runs without the row.
func fetchFed(ctx context.Context, g *Guard, now time.Time) (Headline, bool) {
	items, err := fetchRSS(ctx, g, "fed", fedFeedURL)
	if err != nil {
		return Headline{}, false
	}

	var newest time.Time
	var out Headline
	for _, it := range items.Channel.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" || it.Link == "" {
			continue
		}
		at, err := parseRSSTime(it.PubDate)
		if err != nil || now.Sub(at) > fedMaxAge || !at.After(newest) {
			continue
		}
		newest = at
		out = Headline{Title: title, URL: it.Link, Source: "FED", Age: humanAge(at, now)}
	}
	return out, out.URL != ""
}

// promoted is the genre Isaac asked to keep off the panel: advertising that
// reads as news in a feed. These are the shapes any outlet can produce, rather
// than the stock pick listicle, which is a market feed's genre. Matching on the
// headline is the only lever available, since none of these feeds mark it.
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

// clip drops the video items BBC mixes into its news feeds. They are a minute
// of footage with a headline on it rather than a story, and eight rows cannot
// spare one for the viral snack of the day.
func clip(link string) bool {
	return strings.Contains(link, "/news/videos/")
}

func promotional(title string) bool {
	lower := strings.ToLower(title)
	return slices.ContainsFunc(promoted, func(p string) bool { return strings.Contains(lower, p) })
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
