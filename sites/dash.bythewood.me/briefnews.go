package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"
)

var newsFeeds = []briefFeed{
	{"AP", "C", "googlenews", googleNews("apnews.com")},
	{"REUTERS", "C", "googlenews", googleNews("reuters.com")},
	{"WSJ", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/RSSUSnews"},
	{"HILL", "C", "thehill", "https://thehill.com/news/feed/"},
	{"NEWSNATION", "C", "newsnation", "https://www.newsnationnow.com/feed/"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/world/rss.xml"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1001/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1014/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1004/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1006/rss.xml"},
	{"PBS", "L", "pbs", "https://www.pbs.org/newshour/feeds/rss/headlines"},
	{"CBS", "L", "cbs", "https://www.cbsnews.com/latest/rss/main"},
	{"FOX", "R", "fox", "https://moxie.foxnews.com/google-publisher/politics.xml"},
	{"FOX", "R", "fox", "https://moxie.foxnews.com/google-publisher/us.xml"},
	{"FOX", "R", "fox", "https://moxie.foxnews.com/google-publisher/world.xml"},
	{"EXAMINER", "R", "examiner", "https://www.washingtonexaminer.com/feed/"},
}

// widelyCarried drops the events only one outlet ran once there are enough
// that two or more did. A story nobody else picked up is rarely the one that
// matters, and a 9B model handed fifty will reach for some of them.
func widelyCarried(cs []cluster) []cluster {
	var shared []cluster
	for _, c := range cs {
		if len(c.outlets()) > 1 {
			shared = append(shared, c)
		}
	}
	// Twice the most points a brief can hold, so there's still a choice left
	// once the minor ones are gone.
	if len(shared) < 24 {
		return cs
	}
	return shared
}

// A small model told to skip trials will still write one up, and asked to label
// the point it wrote calls a trial politics. Labelling the list on its own first
// is an easier job, so the skipping happens in Go before the summary.
var newsCategories = []string{"us_politics", "world", "economy", "markets", "technology", "disaster",
	"security", "culture", "crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

var minor = []string{"crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

// mainstream is how many outlets have to carry a minor story before it counts
// as mainstream culture anyway, the way the Lindsay Clancy trial was.
const mainstream = 7

// newsMax is a headline's maxLength, well past twelve words, since a line that
// hits it is cut mid word and dropped.
const newsMax = 160

// newsPoints is the most a brief holds, and so the most events the writer sees.
// It matches the feeds so the panels in the band line up row for row.
const newsPoints = storiesShown

func newsSchema(events int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"points": map[string]any{
				"type": "array", "minItems": min(3, events), "maxItems": events,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":    map[string]any{"type": "string", "maxLength": newsMax},
						"sources": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": map[string]any{"type": "integer"}},
					},
					"required":             []string{"text", "sources"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"points"},
		"additionalProperties": false,
	}
}

type modelPoints struct {
	Points []struct {
		Text    string `json:"text"`
		Sources []int  `json:"sources"`
	} `json:"points"`
}

func (b *Briefer) compileNews(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	now := time.Now()
	stories := gather(ctx, b.guard, newsFeeds, at.Add(-slot.reach), now)
	all := clusterStories(stories, newsEvents)
	events := widelyCarried(all)
	if len(events) < briefFloor {
		return Brief{}, fmt.Errorf("only %d events", len(events))
	}
	ratings, err := b.rate(ctx, events, now)
	if err != nil {
		return Brief{}, fmt.Errorf("rate: %w", err)
	}
	rated := map[string]eventRating{}
	for i, c := range events {
		if i < len(ratings) {
			rated[c.lead().url] = ratings[i]
		}
	}

	// The read goes first so the headlines can leave out what it already says.
	read, picked, err := b.read(ctx, slot, at, all, rated, heard(stories), now)
	if err != nil {
		slog.Warn("brief read failed", slog.String("component", "brief"), slog.Any("err", err))
	}
	events = mostImpact(events, ratings, newsPoints+len(picked))
	events = slices.DeleteFunc(events, func(c cluster) bool {
		return slices.ContainsFunc(picked, func(p cluster) bool { return p.lead().url == c.lead().url })
	})
	events = events[:min(len(events), newsPoints)]
	if len(events) < 3 {
		return Brief{}, fmt.Errorf("only %d major events", len(events))
	}

	task := map[string]string{
		"morning": "Summarise what happened yesterday and overnight.",
		"preopen": "Summarise what happened yesterday, overnight and this morning.",
		"midday":  "Summarise the news so far today.",
		"close":   "Summarise how the day went in the news.",
	}[slot.kind]

	system := "You write a short executive news summary for one reader in the United States.\n" + neutralRules + `
- Write one point for each event, in the order given. If two events are the same story, write one point for them and cite both.
- Each point is one neutral headline about exactly one event, at most 12 words, in past tense. No second sentence, no detail beyond what makes the event clear.
- Each point cites the numbers of the events it draws on in "sources".`

	user := fmt.Sprintf("It is %s Eastern. %s\n\nEvents:\n%s", at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), task, clusterList(events, now))

	var out modelPoints
	if err := b.model.Structured(ctx, system, user, newsSchema(len(events)), 1300, &out); err != nil {
		return Brief{}, err
	}
	// The events are already ranked, so a point goes where its first event is
	// whatever order the model wrote them in.
	first := func(ns []int) int {
		m := len(events)
		for _, n := range ns {
			if n >= 1 && n <= len(events) {
				m = min(m, n)
			}
		}
		return m
	}
	sort.SliceStable(out.Points, func(i, j int) bool { return first(out.Points[i].Sources) < first(out.Points[j].Sources) })

	brief := newBrief(slot, at, events)
	for _, p := range out.Points {
		pt := cite(events, p.Sources)
		pt.Text = whole(p.Text, newsMax)
		// A point the model could not tie to a story is one it may have made up.
		if pt.Text == "" || len(pt.Links) == 0 {
			continue
		}
		pt.Gist = b.eventTLDR(ctx, events, p.Sources, pt.Links)
		brief.Points = append(brief.Points, pt)
	}
	if len(brief.Points) < 2 {
		return Brief{}, fmt.Errorf("only %d cited points", len(brief.Points))
	}
	brief.Read = read
	return brief, nil
}

// readMax is a read headline's maxLength. The model writes about twelve words
// when asked for nine, and a tighter cap drops the top story.
const readMax = 100

// readPicks is the most carried events that change something, by the same
// ratings the headlines use, so a prize or a profile that five outlets ran
// doesn't make the read unless nearly everyone did.
func readPicks(all []cluster, rated map[string]eventRating, n int) []cluster {
	var out []cluster
	for _, c := range all {
		if len(out) == n {
			break
		}
		r, ok := rated[c.lead().url]
		if len(c.outlets()) >= mainstream || ok && r.Impact >= 3 {
			out = append(out, c)
		}
	}
	return out
}

// heard is how many outlets answered this run, the count a story's spread is
// measured against when a feed is down.
func heard(stories []story) int {
	var names []string
	for _, s := range stories {
		if !slices.Contains(names, s.source) {
			names = append(names, s.source)
		}
	}
	return len(names)
}

// pulse is how big the day is, from the share of outlets on its most carried
// story. Seven in ten is every newsroom leading with the same thing, and four
// of eleven is about what an ordinary day's biggest story gets.
func pulse(top, outlets int) (level, label string) {
	switch {
	case outlets > 0 && top*10 >= outlets*7:
		return "major", "MAJOR STORY"
	case top*10 <= outlets*4:
		return "quiet", "QUIET DAY"
	}
	return "steady", "NORMAL DAY"
}

// read returns the events it covered, which the headlines then leave out. A
// read that fails is only logged, since the headlines stand without it.
func (b *Briefer) read(ctx context.Context, slot briefSlot, at time.Time, all []cluster, rated map[string]eventRating, outlets int, now time.Time) (*Read, []cluster, error) {
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("no events")
	}
	top := len(all[0].outlets())
	level, label := pulse(top, outlets)

	// Go says how big the day is so the model can't call every day busy, and
	// the model only sees the events it writes up, or it reaches for whatever
	// reads best.
	opener := map[string]string{"major": "One story is everywhere.", "quiet": "A quiet day."}[level]
	events := readPicks(all, rated, map[string]int{"major": 2, "steady": 3, "quiet": 2}[level])
	if len(events) == 0 {
		return nil, nil, fmt.Errorf("nothing carried widely that matters")
	}

	props := map[string]any{}
	var required []string
	for i := range events {
		limit := readMax
		if i == 0 && level == "major" {
			limit = 2 * readMax
		}
		key := fmt.Sprintf("event%d", i+1)
		props[key] = map[string]any{"type": "string", "maxLength": limit}
		required = append(required, key)
	}
	schema := map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}

	first := "one neutral headline about event 1"
	if level == "major" {
		first = "two neutral headlines about event 1, what happened and what comes next"
	}
	system := "You write the opening lines of a news summary for one reader in the United States, one per event, about the stories most newsrooms are carrying.\n" + neutralRules + `
- "event1" is ` + first + `. Every other field is one neutral headline about the event with that number.
- A headline is at most 9 words, in past tense, with no second clause and no detail like wind speeds, ages, places or who criticised whom.
- Rewrite each in your own plain words. Never hand back the event's own title or quote its words.
- Use only what the event lines say. Never add a name, title or fact from memory, since yours may be out of date.
- Never say how many or which outlets carried it.`

	when := map[string]string{"morning": "yesterday and overnight", "preopen": "yesterday, overnight and this morning",
		"midday": "so far today", "close": "today"}[slot.kind]
	user := fmt.Sprintf("It is %s Eastern. The most widely carried news %s.\n\nEvents:\n%s",
		at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), when, clusterList(events, now))

	// The prompt rule against handing back a title is often ignored, so a copy
	// gets one retry naming it and is dropped if it comes back.
	var out map[string]string
	var copied []int
	for try := 1; try <= 2; try++ {
		ask := user
		if len(copied) > 0 {
			var nums []string
			for _, n := range copied {
				nums = append(nums, fmt.Sprintf("event%d", n))
			}
			ask += "\n\nLast time " + strings.Join(nums, " and ") + " repeated the event's own title. Write those in different words."
		}
		out = nil
		if err := b.model.Structured(ctx, system, ask, schema, 300, &out); err != nil {
			return nil, nil, err
		}
		copied = nil
		for i, c := range events {
			if copiesTitle(out[fmt.Sprintf("event%d", i+1)], c) {
				copied = append(copied, i+1)
			}
		}
		if len(copied) == 0 {
			break
		}
		slog.Info("brief read copied a title", slog.String("component", "brief"), slog.Any("events", copied), slog.Int("try", try))
	}
	var parts []string
	if opener != "" {
		parts = append(parts, opener)
	}
	var cited []int
	for i := range events {
		limit := readMax
		if i == 0 && level == "major" {
			limit = 2 * readMax
		}
		t := whole(out[fmt.Sprintf("event%d", i+1)], limit)
		if t == "" || slices.Contains(copied, i+1) {
			continue
		}
		parts = append(parts, t)
		cited = append(cited, i+1)
	}
	if len(cited) == 0 {
		return nil, nil, fmt.Errorf("no read")
	}
	// One link per event, the least framed telling, since cite would spend
	// every link on the first event's outlets.
	var links []Link
	for _, n := range cited {
		l := events[n-1].lead()
		links = append(links, Link{Source: l.source, URL: l.url, Title: l.title})
	}
	text := strings.Join(parts, " ")
	r := &Read{Level: level, Label: label, Spread: fmt.Sprintf("TOP STORY ON %d OF %d OUTLETS", top, outlets),
		Text: text, Links: links}
	// Only a read that opens on the most carried story carries its rating, since
	// that's the story the level was counted from.
	if cited[0] == 1 && events[0].lead().url == all[0].lead().url {
		r.Impact = rated[all[0].lead().url].Impact
	}
	slog.Info("brief read", slog.String("component", "brief"), slog.String("slot", slot.kind),
		slog.String("level", level), slog.Int("top", top), slog.Int("outlets", outlets),
		slog.String("lead", all[0].lead().title), slog.String("text", text))
	var covered []cluster
	for _, n := range cited {
		covered = append(covered, events[n-1])
	}
	return r, covered, nil
}

// copiesTitle is a headline that is one of the event's own titles, or six or
// more of its words in a row, once case and punctuation are ignored.
func copiesTitle(line string, c cluster) bool {
	l := plainWords(line)
	if l == "" {
		return false
	}
	for _, s := range c.stories {
		t := plainWords(s.title)
		short := l
		if len(t) < len(short) {
			short = t
		}
		if l == t || len(strings.Fields(short)) >= 6 && (strings.Contains(t, l) || strings.Contains(l, t)) {
			return true
		}
	}
	return false
}

func plainWords(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// eventRating is the model's read of one event. Each carries its number back,
// since a small model labelling a long list in order drifts a place partway
// down.
type eventRating struct {
	N      int    `json:"n"`
	Label  string `json:"label"`
	Impact int    `json:"impact"`
}

// rateBatch keeps each call short enough that the numbering holds.
const rateBatch = 20

func (b *Briefer) rate(ctx context.Context, events []cluster, now time.Time) ([]eventRating, error) {
	system := `You rate news events, one rating per event, each carrying the event's number as "n".

"label" is the topic:
- us_politics: Congress, the White House, federal agencies and policy, elections, parties.
- crime_or_court: a criminal case, arrest, trial, sentencing or execution of particular people, even when officials are involved.
- security: war, terrorism, the military and intelligence, when it is about a country and not one suspect.
- incident: a shooting, attack, crash or fire with a handful of victims, in any country, that changes nothing beyond the place it happened.
- disaster: storms, floods, quakes and anything that shuts down production, power or travel for many people.
- human_interest: one person's or family's story.
- local: a story about one city or town.
- economy or markets: any story about prices, shares, yields, earnings or forecasts, whatever set it off, so futures falling after an attack is markets and not an incident.
The rest mean what they say.

"impact" is how much the event could change an ordinary American's money, prices, job, safety or daily life, or move the US stock market:
5: moves the whole market or changes prices, taxes or rates for most Americans, like a Fed decision, a war that moves oil, or tariffs on a major partner.
4: likely to move oil, rates or a whole industry, or a major change in US law or policy.
3: a notable US political or world event with an indirect effect.
2: worth knowing, with little effect on an American's money or life.
1: no effect on them at all.`

	ratings := make([]eventRating, len(events))
	for start := 0; start < len(events); start += rateBatch {
		batch := events[start:min(start+rateBatch, len(events))]
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ratings": map[string]any{
					"type": "array", "minItems": len(batch), "maxItems": len(batch),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"n":      map[string]any{"type": "integer", "minimum": 1, "maximum": len(batch)},
							"label":  map[string]any{"type": "string", "enum": newsCategories},
							"impact": map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
						},
						"required":             []string{"n", "label", "impact"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"ratings"},
			"additionalProperties": false,
		}
		var out struct {
			Ratings []eventRating `json:"ratings"`
		}
		if err := b.model.Structured(ctx, system, "Events:\n"+clusterList(batch, now), schema, 30*len(batch)+100, &out); err != nil {
			return nil, err
		}
		for _, r := range out.Ratings {
			if r.N >= 1 && r.N <= len(batch) {
				ratings[start+r.N-1] = r
			}
		}
	}
	return ratings, nil
}

// mostImpact drops anything rated 1 and keeps the top rated up to limit, unrated
// counting as a 3. Minor ones need mainstream or a 4 since market stories get
// filed as incidents, and one outlet's 3s only fill in after everything else.
func mostImpact(events []cluster, ratings []eventRating, limit int) []cluster {
	type ranked struct {
		c      cluster
		impact int
		filler bool
	}
	var keep []ranked
	for i, c := range events {
		r := eventRating{Impact: 3}
		rated := i < len(ratings) && ratings[i].N != 0
		if rated {
			r = ratings[i]
		}
		single := rated && len(c.outlets()) == 1
		drop := (slices.Contains(minor, r.Label) && r.Impact <= 3 && len(c.outlets()) < mainstream) || r.Impact <= 1 ||
			single && r.Impact < 3
		msg := "brief event kept"
		if drop {
			msg = "brief event dropped"
		}
		slog.Info(msg, slog.String("component", "brief"), slog.String("category", r.Label),
			slog.Int("impact", r.Impact), slog.Int("outlets", len(c.outlets())), slog.String("title", c.lead().title))
		if !drop {
			keep = append(keep, ranked{c, r.Impact, single && r.Impact < 4})
		}
	}
	sort.SliceStable(keep, func(i, j int) bool {
		if keep[i].filler != keep[j].filler {
			return !keep[i].filler
		}
		return keep[i].impact > keep[j].impact
	})
	var out []cluster
	for _, k := range keep[:min(len(keep), limit)] {
		out = append(out, k.c)
	}
	return out
}
