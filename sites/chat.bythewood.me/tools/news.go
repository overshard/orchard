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
)

// newsWindows are the phrasings Isaac actually uses. Each resolves against a
// New York clock, since "today" means the day he is having and not UTC's.
var newsWindows = []string{"today", "yesterday", "weekend", "weekend-and-today", "week", "month"}

var News = Tool{
	Name: "news",
	Description: "Read the news from a fixed list of publishers rather than searching the web. " +
		"Use this for any question asking what is happening or what happened over a period, " +
		"like news today, big stories this weekend, what did I miss this week, or any tech or AI news. " +
		"For one named story that is already known, use web_search instead.",
	Schema: obj(map[string]any{
		"window": map[string]any{"type": "string",
			"description": "the period the question asked for, taken literally. today means today, " +
				"weekend means Saturday and Sunday only, and weekend-and-today is for a question " +
				"that asks for both, like over the weekend including today",
			"enum": newsWindows},
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

		sections, tried, failed := gatherNews(ctx, d, topic, since, until)
		if len(sections) == 0 {
			if failed >= tried && tried > 0 {
				return nil, fmt.Errorf("none of the %d news sources answered", tried)
			}
			return map[string]any{
				"window": label, "topic": topic, "sections": []any{},
				"note": "Nothing was published in that window by any of the sources read. " +
					"Say so plainly rather than widening the window on your own or " +
					"answering from memory.",
			}, nil
		}

		return map[string]any{
			"window":   label,
			"topic":    topic,
			"sections": sections,
			"sources":  sectionSources(sections),
			"count":    countItems(sections),
			"note": itemBudget(sections) +
				"Answer in exactly this shape and nothing else:\n\n" +
				"### <section name>\n" +
				"- **<the fact>** rest of the plain sentence. (Publisher: \"their headline\")\n" +
				"- **<the fact>** rest of the plain sentence. (Publisher: \"their headline\")\n\n" +
				"### <next section name>\n" +
				"- ...\n\n" +
				"Worked example of one bullet:\n" +
				"- **Five died** when a cargo plane overran the runway at **Miami International**. " +
				"(NPR: \"5 dead after crash at Miami Airport\")\n\n" +
				"Rules:\n" +
				"- One heading per section above, in the order given, keeping its name.\n" +
				"- Every item gets a bullet, including the sections further down. Dropping a " +
				"whole section is the failure to avoid here. This is a rundown, so do not pick " +
				"one and write it up, and do not collapse the list into a paragraph.\n" +
				"- One bullet per item and no more. The counts above are what is in the list, " +
				"so never split one story into two bullets or repeat one across sections to " +
				"reach a number. Every headline above is already distinct.\n" +
				"- Bold only the few words carrying the news, the number, the name, the place or " +
				"what changed, so it can be skimmed. Never bold a whole sentence.\n" +
				"- The publisher's headline goes after your sentence, word for word, in quotes. " +
				"Never drop it, it is what shows how the story was sold.\n" +
				"- Merge a story two publishers ran into one bullet and name both.\n" +
				"- Strip the loaded verbs and the party line, and put back the specifics they " +
				"hid, so \"SLAMS\" becomes what was actually said and a tariff names the rate " +
				"and the goods. Take no side and invent no detail.\n" +
				"- Do not research any of these. The rundown is the answer.",
		}, nil
	},
}

func countItems(sections []NewsSection) int {
	n := 0
	for _, sec := range sections {
		n += len(sec.Items)
	}
	return n
}

// itemBudget opens the note with the arithmetic, because "every item gets a
// bullet" on its own got four of twenty one and two sections of four. A model
// told it owes twenty one bullets under four headings can count what it wrote.
func itemBudget(sections []NewsSection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "There are %d items here across %d sections. Your answer has to carry all %d, "+
		"as %d bullets under %d headings:\n", countItems(sections), len(sections),
		countItems(sections), countItems(sections), len(sections))
	for _, sec := range sections {
		fmt.Fprintf(&b, "  %s: %d bullets\n", sec.Name, len(sec.Items))
	}
	b.WriteString("\n")
	return b.String()
}

// sectionSources says which publishers are in the answer, so a reader can see
// at a glance whether a source they expected was reachable.
func sectionSources(sections []NewsSection) []string {
	seen := map[string]bool{}
	var out []string
	for _, sec := range sections {
		for _, it := range sec.Items {
			if !seen[it.Source] {
				seen[it.Source] = true
				out = append(out, it.Source)
			}
		}
	}
	sort.Strings(out)
	return out
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
	case "weekend-and-today":
		// "over the weekend including today" has no answer in either of the
		// other two, and asked on a Monday the plain weekend window excludes
		// today by definition, which quietly dropped the day he asked about.
		back := (int(now.Weekday()) + 1) % 7
		sat := midnight.AddDate(0, 0, -back)
		return sat, now, "the weekend of " + day(sat) + " and " + day(sat.AddDate(0, 0, 1)) +
			", plus today, " + day(now)
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

// section is one heading in the answer. Each keeps its own slots, which is what
// stops the newsrooms crowding the aggregators out: a flat cap over everything
// sorted desks first took 28 of 45 items and left all fourteen Hacker News
// stories and both Lobsters ones on the floor.
type section struct {
	name        string
	feeds       []feed
	aggregators bool
	slots       int
}

var (
	nprNews     = feed{"NPR", "https://feeds.npr.org/1001/rss.xml"}
	nprWorld    = feed{"NPR World", "https://feeds.npr.org/1004/rss.xml"}
	nprBusiness = feed{"NPR Business", "https://feeds.npr.org/1006/rss.xml"}
	nprScience  = feed{"NPR Science", "https://feeds.npr.org/1007/rss.xml"}
	nprPolitics = feed{"NPR Politics", "https://feeds.npr.org/1014/rss.xml"}
	nprTech     = feed{"NPR Technology", "https://feeds.npr.org/1019/rss.xml"}

	bbcNews     = feed{"BBC", "https://feeds.bbci.co.uk/news/rss.xml"}
	bbcWorld    = feed{"BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"}
	bbcTech     = feed{"BBC Technology", "https://feeds.bbci.co.uk/news/technology/rss.xml"}
	bbcBusiness = feed{"BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml"}
	bbcScience  = feed{"BBC Science", "https://feeds.bbci.co.uk/news/science_and_environment/rss.xml"}

	ars        = feed{"Ars Technica", "https://feeds.arstechnica.com/arstechnica/index"}
	techcrunch = feed{"TechCrunch", "https://techcrunch.com/feed/"}
	marketch   = feed{"MarketWatch", "https://feeds.content.dowjones.io/public/rss/mw_topstories"}
)

// newsSections is the whole source list. Reuters and AP are missing because
// both retired their public feeds: every documented Reuters path is dead at the
// connection and apnews.com answers 401 or 404 to all of theirs. Reddit is
// missing because old.reddit.com redirects every logged out request to a login
// page and www.reddit.com rate limits its rss after a handful of calls, and
// carries no score, so there is no way to ask it what got big numbers.
var newsSections = map[string][]section{
	"general": {
		{name: "Top stories", feeds: []feed{nprNews, bbcNews}, slots: 10},
		{name: "World", feeds: []feed{nprWorld, bbcWorld}, slots: 7},
		{name: "Business", feeds: []feed{nprBusiness, bbcBusiness}, slots: 4},
		{name: "Tech and what the forums are reading",
			feeds: []feed{bbcTech, ars, techcrunch}, aggregators: true, slots: 9},
	},
	"us": {
		{name: "Top stories", feeds: []feed{nprNews, bbcNews}, slots: 12},
		{name: "Politics", feeds: []feed{nprPolitics}, slots: 8},
	},
	"world": {
		{name: "World", feeds: []feed{nprWorld, bbcWorld}, slots: 20},
	},
	"politics": {
		{name: "Politics", feeds: []feed{nprPolitics, bbcNews}, slots: 20},
	},
	"business": {
		{name: "Business", feeds: []feed{nprBusiness, bbcBusiness, marketch}, slots: 20},
	},
	"science": {
		{name: "Science", feeds: []feed{nprScience, bbcScience}, slots: 20},
	},
	"tech": {
		{name: "What the forums are reading", aggregators: true, slots: 12},
		{name: "Tech press", feeds: []feed{bbcTech, nprTech, ars, techcrunch}, slots: 10},
	},
}

// NewsSection is one heading and what belongs under it.
type NewsSection struct {
	Name  string     `json:"section"`
	Items []NewsItem `json:"items"`
}

// gatherNews reads every source for a topic at once and keeps what landed in
// the window. A source that fails is counted and skipped rather than failing
// the tool, because eight publishers answering out of nine is still the news.
//
// The aggregators are fetched once however many sections asked for them, since
// two sections both wanting Hacker News is not a reason to fetch it twice.
func gatherNews(ctx context.Context, d *Deps, topic string, since, until time.Time) ([]NewsSection, int, int) {
	sections := newsSections[topic]
	if topic == "ai" {
		sections = newsSections["tech"]
	}
	if len(sections) == 0 {
		sections = newsSections["general"]
	}

	var (
		mu     sync.Mutex
		tried  int
		failed int
		wg     sync.WaitGroup
		byFeed = map[string][]NewsItem{}
		scored []NewsItem
	)
	note := func(err error) {
		tried++
		if err != nil {
			failed++
		}
	}

	wantAgg := false
	seenFeed := map[string]bool{}
	for _, sec := range sections {
		if sec.aggregators {
			wantAgg = true
		}
		for _, f := range sec.feeds {
			if seenFeed[f.url] {
				continue
			}
			seenFeed[f.url] = true
			wg.Add(1)
			go func(f feed) {
				defer wg.Done()
				got, err := readFeed(ctx, d, f, since, until)
				mu.Lock()
				defer mu.Unlock()
				note(err)
				byFeed[f.url] = got
			}(f)
		}
	}
	if wantAgg {
		wg.Add(2)
		go func() {
			defer wg.Done()
			got, err := readHackerNews(ctx, d, since, until)
			mu.Lock()
			defer mu.Unlock()
			note(err)
			scored = append(scored, got...)
		}()
		go func() {
			defer wg.Done()
			got, err := readLobsters(ctx, d, since, until)
			mu.Lock()
			defer mu.Unlock()
			note(err)
			scored = append(scored, got...)
		}()
	}
	wg.Wait()

	// One story reaching two sections reads as the tool repeating itself, so a
	// headline is placed in the first section that wanted it and nowhere else.
	placed := map[string]bool{}
	take := func(items []NewsItem, slots int, scoredFirst bool) []NewsItem {
		items = dedupeNews(items)
		sortNews(items, scoredFirst)
		out := make([]NewsItem, 0, slots)
		for _, it := range items {
			if len(out) >= slots {
				break
			}
			key := strings.ToLower(strings.TrimSpace(it.Headline))
			if it.URL != "" {
				key = it.URL
			}
			if placed[key] {
				continue
			}
			placed[key] = true
			out = append(out, it)
		}
		return out
	}

	out := make([]NewsSection, 0, len(sections))
	for _, sec := range sections {
		var desks []NewsItem
		for _, f := range sec.feeds {
			desks = append(desks, byFeed[f.url]...)
		}
		if got := fillSection(sec, desks, scored, take); len(got) > 0 {
			out = append(out, NewsSection{Name: sec.name, Items: got})
		}
	}
	return out, tried, failed
}

// fillSection shares one section's slots out between the newsrooms and the
// aggregators. A section holding both has to split them, or the desks sort
// first and eat every one, which is the same starvation the per section slots
// were added to stop. Whatever one side does not use, the other takes.
func fillSection(sec section, desks, scored []NewsItem, take func([]NewsItem, int, bool) []NewsItem) []NewsItem {
	switch {
	case !sec.aggregators:
		return take(desks, sec.slots, false)
	case len(sec.feeds) == 0:
		return take(scored, sec.slots, true)
	}
	got := take(scored, sec.slots/2, true)
	got = append(got, take(desks, sec.slots-len(got), false)...)
	if short := sec.slots - len(got); short > 0 {
		got = append(got, take(scored, short, true)...)
	}
	return got
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
