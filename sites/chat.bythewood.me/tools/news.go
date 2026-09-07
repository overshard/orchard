package tools

import (
	"context"
	"encoding/xml"
	"fmt"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The news reader. A question like "any big news today?" used to go to
// web_search, which returns whatever a search engine felt like that minute, so
// the same question twice gave two different days of news. This reads a fixed
// list of publishers instead, so the shape of an answer is the same every time
// and the only thing that varies is what happened.
//
// Two kinds of source, and they are ranked differently. Hacker News and
// Lobsters publish a score, so what got attention is a number and the cut is a
// threshold. A newsroom feed has no score, and the order it publishes in is the
// desk's own judgement of what leads, so position is the signal there.

const (
	// What a front page is worth reading down to. Below this an item is a
	// story a handful of people upvoted, which is not what "big news" means.
	hnMinPoints       = 150
	lobstersMinPoints = 25
	// How far down a newsroom feed still counts as the front page.
	deskDepth = 12
	// The ceiling on what comes back, since the model has to read all of it and
	// a hundred headlines crowd out the answer.
	maxNewsItems = 28
)

// newsWindows are the phrasings Isaac actually uses. Each resolves against a
// New York clock, since "today" means the day he is having and not UTC's.
var newsWindows = []string{"today", "yesterday", "weekend", "week", "month"}

var News = Tool{
	Name: "news",
	Description: "Read the news from a fixed list of publishers rather than searching the web. " +
		"Use this for any question asking what is happening or what happened over a period, " +
		"like news today, big stories this weekend, what did I miss this week, or any tech or AI news. " +
		"For one named story that is already known, use web_search instead.",
	Schema: obj(map[string]any{
		"window": map[string]any{"type": "string",
			"description": "the period the question asked for, taken literally: today means today",
			"enum":        newsWindows},
		"topic": map[string]any{"type": "string",
			"description": "the subject asked for, or general when the question named none",
			"enum": []string{"general", "world", "us", "tech", "ai", "politics",
				"business", "science"}},
	}, "window"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		window := strings.ToLower(argStr(a, "window"))
		if window == "" {
			window = "today"
		}
		topic := strings.ToLower(argStr(a, "topic"))
		if topic == "" {
			topic = "general"
		}

		now := d.Now().In(newYork())
		since, until, label := resolveWindow(now, window)

		items, tried, failed := gatherNews(ctx, d, topic, since, until)
		if len(items) == 0 {
			if failed >= tried && tried > 0 {
				return nil, fmt.Errorf("none of the %d news sources answered", tried)
			}
			return map[string]any{
				"window": label, "topic": topic, "items": []any{},
				"note": "Nothing was published in that window by any of the sources read. " +
					"Say so plainly rather than widening the window on your own or " +
					"answering from memory.",
			}, nil
		}

		sortNews(items, topic == "tech" || topic == "ai")
		if len(items) > maxNewsItems {
			items = items[:maxNewsItems]
		}
		return map[string]any{
			"window":  label,
			"topic":   topic,
			"sources": sourcesOf(items),
			"items":   items,
			"note": "This is a rundown and not one story. Answer with the whole list, every item " +
				"above, as a short bullet each, and do not pick one and write it up. Where several " +
				"publishers carried the same story, merge them into one bullet and say who ran it, " +
				"so the list is by story rather than by publisher. Order it as it arrived here, " +
				"since that is already what led.\n\n" +
				"Each bullet is one plain sentence of your own saying who did what, and then, on " +
				"the same bullet, the publisher's headline word for word in quotes with the " +
				"publisher after it, like: Five died when a cargo plane overran the runway at " +
				"Miami. (NPR: \"Investigators seek answers after Amazon cargo plane crash kills " +
				"five at Miami airport\"). Never drop that second half, it is what lets him see " +
				"how it was sold to him. Strip the loaded verbs, " +
				"the outrage framing and the party line, and put back the specifics they were " +
				"hiding, so \"SLAMS\" becomes what was actually said and a tariff story names the " +
				"rate and the goods. Take no side of your own, and invent no detail that is not in " +
				"the headline or the summary.\n\n" +
				"Do not go and research any of these. The rundown is the answer, and he will ask " +
				"if he wants one of them followed up.",
		}, nil
	},
}

// newYork is the clock every window is resolved against. A fixed offset would
// drift twice a year, and the answer to "today" would then be wrong for an hour
// on two mornings.
func newYork() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return loc
}

// resolveWindow turns a word into a half open range and a label saying what it
// decided, because a reader who asked for the weekend deserves to see which
// days that was rather than trusting it.
func resolveWindow(now time.Time, window string) (time.Time, time.Time, string) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := func(t time.Time) string { return t.Format("Mon 2 Jan 2006") }

	switch window {
	case "yesterday":
		start := midnight.AddDate(0, 0, -1)
		return start, midnight, "yesterday, " + day(start)
	case "weekend":
		// The most recent Saturday and Sunday. Asked on one of them it means
		// the one being had, and asked on a Wednesday it means the one just
		// gone, which is what a person means by "this weekend" either way.
		back := (int(now.Weekday()) + 1) % 7
		sat := midnight.AddDate(0, 0, -back)
		end := sat.AddDate(0, 0, 2)
		if end.After(now) {
			end = now
		}
		return sat, end, "the weekend of " + day(sat) + " and " + day(sat.AddDate(0, 0, 1))
	case "week":
		start := midnight.AddDate(0, 0, -6)
		return start, now, "the week from " + day(start) + " to " + day(now)
	case "month":
		start := midnight.AddDate(0, 0, -29)
		return start, now, "the thirty days from " + day(start) + " to " + day(now)
	default:
		return midnight, now, "today, " + day(now)
	}
}

// NewsItem is one story. Points is left out of the JSON when there is none,
// since a newsroom feed has no score and a zero would read as unpopular rather
// than unscored.
type NewsItem struct {
	Source    string `json:"source"`
	Headline  string `json:"headline"`
	URL       string `json:"url"`
	Published string `json:"published"`
	Summary   string `json:"summary,omitempty"`
	Points    int    `json:"points,omitempty"`
	Comments  int    `json:"comments,omitempty"`
	Discuss   string `json:"discuss,omitempty"`

	at   time.Time
	rank int
}

// feed is one newsroom source. The desk orders its own front page, so rank is
// the position an item arrived in.
type feed struct {
	name, url string
}

// newsFeeds is the whole source list, by topic. Reuters and AP are missing
// because both retired their public feeds: every documented Reuters path is
// dead and apnews.com answers 401 or 404 to all of them.
var newsFeeds = map[string][]feed{
	"general": {
		{"NPR", "https://feeds.npr.org/1001/rss.xml"},
		{"BBC", "https://feeds.bbci.co.uk/news/rss.xml"},
		{"BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"},
	},
	"world": {
		{"NPR World", "https://feeds.npr.org/1004/rss.xml"},
		{"BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"},
	},
	"us": {
		{"NPR", "https://feeds.npr.org/1001/rss.xml"},
		{"NPR Politics", "https://feeds.npr.org/1014/rss.xml"},
	},
	"politics": {
		{"NPR Politics", "https://feeds.npr.org/1014/rss.xml"},
		{"BBC", "https://feeds.bbci.co.uk/news/rss.xml"},
	},
	"business": {
		{"NPR Business", "https://feeds.npr.org/1006/rss.xml"},
		{"BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml"},
		{"MarketWatch", "https://feeds.content.dowjones.io/public/rss/mw_topstories"},
	},
	"science": {
		{"NPR Science", "https://feeds.npr.org/1007/rss.xml"},
		{"BBC Science", "https://feeds.bbci.co.uk/news/science_and_environment/rss.xml"},
	},
	"tech": {
		{"BBC Technology", "https://feeds.bbci.co.uk/news/technology/rss.xml"},
		{"NPR Technology", "https://feeds.npr.org/1019/rss.xml"},
		{"Ars Technica", "https://feeds.arstechnica.com/arstechnica/index"},
		{"TechCrunch", "https://techcrunch.com/feed/"},
	},
}

// Whether the aggregators are worth reading for a topic. They are where a
// story breaks first on anything technical, and they are noise on politics.
func wantsAggregators(topic string) bool {
	switch topic {
	case "tech", "ai", "general":
		return true
	}
	return false
}

// gatherNews reads every source for a topic at once and keeps what landed in
// the window. A source that fails is counted and skipped rather than failing
// the tool, because eight publishers answering out of nine is still the news.
func gatherNews(ctx context.Context, d *Deps, topic string, since, until time.Time) ([]NewsItem, int, int) {
	feeds := newsFeeds[topic]
	if topic == "ai" {
		feeds = newsFeeds["tech"]
	}
	if len(feeds) == 0 {
		feeds = newsFeeds["general"]
	}

	var (
		mu     sync.Mutex
		out    []NewsItem
		tried  int
		failed int
		wg     sync.WaitGroup
	)
	collect := func(got []NewsItem, err error) {
		mu.Lock()
		defer mu.Unlock()
		tried++
		if err != nil {
			failed++
			return
		}
		out = append(out, got...)
	}

	for _, f := range feeds {
		wg.Add(1)
		go func(f feed) {
			defer wg.Done()
			collect(readFeed(ctx, d, f, since, until))
		}(f)
	}
	if wantsAggregators(topic) {
		wg.Add(2)
		go func() { defer wg.Done(); collect(readHackerNews(ctx, d, since, until)) }()
		go func() { defer wg.Done(); collect(readLobsters(ctx, d, since, until)) }()
	}
	wg.Wait()
	return dedupeNews(out), tried, failed
}

// rssFeed covers RSS 2.0 and Atom in one shape, since BBC and NPR publish the
// first and plenty of others publish the second, and the difference is not
// worth two parsers.
type rssFeed struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
		Date        string `xml:"date"`
	} `xml:"channel>item"`
	Entries []struct {
		Title   string `xml:"title"`
		Summary string `xml:"summary"`
		Updated string `xml:"updated"`
		Link    struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func readFeed(ctx context.Context, d *Deps, f feed, since, until time.Time) ([]NewsItem, error) {
	body, err := get(ctx, d, f.url, "application/rss+xml, application/xml, text/xml")
	if err != nil {
		return nil, err
	}
	var parsed rssFeed
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", f.name, err)
	}

	var out []NewsItem
	add := func(title, link, summary, when string, pos int) {
		title = strings.TrimSpace(title)
		if title == "" || pos >= deskDepth {
			return
		}
		at, ok := parseFeedTime(when)
		if !ok || at.Before(since) || !at.Before(until) {
			return
		}
		out = append(out, NewsItem{
			Source: f.name, Headline: title, URL: strings.TrimSpace(link),
			Published: at.In(since.Location()).Format("Mon 2 Jan 15:04"),
			Summary:   trimSummary(summary), at: at, rank: pos,
		})
	}
	for i, it := range parsed.Items {
		when := it.PubDate
		if when == "" {
			when = it.Date
		}
		add(it.Title, it.Link, it.Description, when, i)
	}
	for i, e := range parsed.Entries {
		add(e.Title, e.Link.Href, e.Summary, e.Updated, i)
	}
	return out, nil
}

// parseFeedTime covers what publishers actually send. RSS is meant to be
// RFC1123 with a numeric zone and several send a named one or leave it off.
func parseFeedTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC822Z, time.RFC822,
		"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
		"2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func trimSummary(s string) string {
	s = stripTags(s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 280 {
		s = strings.TrimSpace(s[:280]) + "…"
	}
	return s
}

// A feed description carries markup often enough that leaving it in wastes the
// model's window on span tags.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func readHackerNews(ctx context.Context, d *Deps, since, until time.Time) ([]NewsItem, error) {
	// search_by_date with a timestamp filter rather than the front page, since
	// the front page is whatever is on it now and says nothing about a window
	// that closed on Sunday.
	// The comparison operators have to be percent encoded. Sent raw, Algolia's
	// front door answers 400 with an HTML body, which parses as no stories
	// rather than as an error.
	filters := fmt.Sprintf("created_at_i>%d,created_at_i<%d,points>%d",
		since.Unix(), until.Unix(), hnMinPoints)
	url := "https://hn.algolia.com/api/v1/search?tags=story&hitsPerPage=40&numericFilters=" +
		neturl.QueryEscape(filters)
	var payload struct {
		Hits []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Points    int    `json:"points"`
			Comments  int    `json:"num_comments"`
			ObjectID  string `json:"objectID"`
			CreatedAt int64  `json:"created_at_i"`
		} `json:"hits"`
	}
	if err := getJSON(ctx, d, url, &payload); err != nil {
		return nil, err
	}
	out := make([]NewsItem, 0, len(payload.Hits))
	for _, h := range payload.Hits {
		if strings.TrimSpace(h.Title) == "" {
			continue
		}
		at := time.Unix(h.CreatedAt, 0)
		link := h.URL
		discuss := "https://news.ycombinator.com/item?id=" + h.ObjectID
		if link == "" {
			link = discuss
		}
		out = append(out, NewsItem{
			Source: "Hacker News", Headline: h.Title, URL: link,
			Published: at.In(since.Location()).Format("Mon 2 Jan 15:04"),
			Points:    h.Points, Comments: h.Comments, Discuss: discuss, at: at,
		})
	}
	return out, nil
}

func readLobsters(ctx context.Context, d *Deps, since, until time.Time) ([]NewsItem, error) {
	var payload []struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Score       int    `json:"score"`
		Comments    int    `json:"comment_count"`
		ShortID     string `json:"short_id_url"`
		CreatedAt   string `json:"created_at"`
		CommentsURL string `json:"comments_url"`
	}
	if err := getJSON(ctx, d, "https://lobste.rs/hottest.json", &payload); err != nil {
		return nil, err
	}
	var out []NewsItem
	for _, s := range payload {
		if s.Score < lobstersMinPoints || strings.TrimSpace(s.Title) == "" {
			continue
		}
		at, ok := parseFeedTime(s.CreatedAt)
		if !ok || at.Before(since) || !at.Before(until) {
			continue
		}
		link := s.URL
		if link == "" {
			link = s.CommentsURL
		}
		out = append(out, NewsItem{
			Source: "Lobsters", Headline: s.Title, URL: link,
			Published: at.In(since.Location()).Format("Mon 2 Jan 15:04"),
			Points:    s.Score, Comments: s.Comments, Discuss: s.CommentsURL, at: at,
		})
	}
	return out, nil
}

// Two publishers carrying one story is worth knowing and two copies of one
// publisher's own item is not, so this drops by url and by headline and leaves
// the rest alone.
func dedupeNews(in []NewsItem) []NewsItem {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, it := range in {
		key := it.Source + "\x00" + strings.ToLower(strings.TrimSpace(it.Headline))
		if it.URL != "" {
			key = it.Source + "\x00" + it.URL
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

// What leads depends on the topic. On tech the aggregators are the story and a
// score is the best measure of that. On general news they are not: a post about
// converting timestamps outranking a fatal plane crash is the wrong answer to
// "any big news today", so there the desks lead and the scored items follow.
func sortNews(items []NewsItem, scoredFirst bool) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.Points > 0) != (b.Points > 0) {
			return (a.Points > 0) == scoredFirst
		}
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.at.After(b.at)
	})
}

func sourcesOf(items []NewsItem) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it.Source] {
			seen[it.Source] = true
			out = append(out, it.Source)
		}
	}
	sort.Strings(out)
	return out
}
