package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
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
// that two or more did, since a story nobody else picked up is rarely the one
// that matters.
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

// Every event is labelled before anything is written, so what TODAY leaves out
// is decided in Go from the labels and never left to the writer's judgement.
var newsCategories = []string{"us_politics", "world", "economy", "markets", "technology", "disaster",
	"security", "culture", "crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

var minor = []string{"crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

// mainstream is how many outlets have to carry a minor story before it counts
// as mainstream culture anyway, the way the Lindsay Clancy trial was.
const mainstream = 7

// newsPoints is the most a brief holds, and so the most events the writer sees.
// It matches the feeds so the panels in the band line up row for row.
const newsPoints = storiesShown

func newsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"points": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":    map[string]any{"type": "string"},
						"sources": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
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

type modelPoint struct {
	Text    string `json:"text"`
	Sources []int  `json:"sources"`
}

type modelPoints struct {
	Points []modelPoint `json:"points"`
}

// picks is the events a point cites, skipping numbers that aren't events.
func picks(events []cluster, nums []int) []cluster {
	var out []cluster
	for _, n := range nums {
		if n >= 1 && n <= len(events) {
			out = append(out, events[n-1])
		}
	}
	return out
}

// related is the cited events a point is actually about, so its links follow
// what it says and not a number the model wrote by mistake.
func related(text string, events []cluster, nums []int) []int {
	var out []int
	for _, n := range nums {
		if n >= 1 && n <= len(events) && relates(text, events[n-1]) && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// pointProblems is what's wrong with each point, by its index: one about none
// of the events it cites, or one naming something those events don't have.
func pointProblems(events []cluster, out modelPoints) map[int]string {
	bad := map[int]string{}
	for i, p := range out.Points {
		nums := related(p.Text, events, p.Sources)
		if len(nums) == 0 {
			bad[i] = fmt.Sprintf("%q isn't about the events it cites, cite the event it's about", p.Text)
			continue
		}
		if miss := unsupported(p.Text, eventText(picks(events, nums)...)); len(miss) > 0 {
			bad[i] = fmt.Sprintf("%q has %s, which its events don't say, use only what they say", p.Text, strings.Join(miss, ", "))
		}
	}
	return bad
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
- Each event is its lead story, with how the other outlets told it indented under it.
- Write one point for each event, in the order given. If two events are the same story, write one point for them and cite both.
- Each point is one neutral headline about exactly one event, at most 12 words, in past tense. No second sentence, no detail beyond what makes the event clear.
- Each point cites the numbers of the events it draws on in "sources", at most four.`

	user := fmt.Sprintf("It is %s Eastern. %s\n\nEvents:\n%s", at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), task, eventDetail(events, now))

	// One retry with the problems named, then a point that still has one is
	// dropped.
	var out modelPoints
	var bad map[int]string
	ask := user
	for try := 0; try < 2; try++ {
		out = modelPoints{}
		if err := b.model.Structured(ctx, system, ask, newsSchema(), 2000, &out); err != nil {
			return Brief{}, err
		}
		if bad = pointProblems(events, out); len(bad) == 0 {
			break
		}
		problems := slices.Collect(maps.Values(bad))
		slog.Info("brief news retried", slog.String("component", "brief"), slog.Int("try", try+1), slog.String("problems", strings.Join(problems, "; ")))
		ask = user + "\n\nYour last answer had problems, fix them:\n- " + strings.Join(problems, "\n- ")
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
	var written []modelPoint
	for i, p := range out.Points {
		if _, ok := bad[i]; !ok && i < len(events) {
			p.Sources = related(p.Text, events, p.Sources)
			p.Sources = p.Sources[:min(len(p.Sources), 4)]
			written = append(written, p)
		}
	}
	sort.SliceStable(written, func(i, j int) bool { return first(written[i].Sources) < first(written[j].Sources) })

	prev := b.store.Snapshot().Briefs.News
	past := b.archive.since("news", now.AddDate(0, 0, -timelineDays))
	brief := newBrief(slot, at, events)
	for _, p := range written {
		pt := cite(events, p.Sources)
		source := eventText(picks(events, p.Sources)...)
		pt.Text = stripParties(sentence(p.Text), source)
		// A point the model could not tie to a story is one it may have made up.
		if pt.Text == "" || len(pt.Links) == 0 {
			continue
		}
		if reports, fetched := b.eventReports(ctx, events, p.Sources, pt.Links); fetched {
			pt.Gist = stripParties(b.eventTLDR(ctx, reports), reports)
		}
		pt.Mark = mark(prev, pt, picks(events, p.Sources))
		pt.Timeline = timeline(past, pt, at)
		brief.Points = append(brief.Points, pt)
	}
	if len(brief.Points) < 2 {
		return Brief{}, fmt.Errorf("only %d cited points", len(brief.Points))
	}
	brief.Intensity = intensity(append(past, Brief{Slot: at.Unix(), Read: read}), at)
	brief.Read = read
	return brief, nil
}

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

// eventDetail is clusterList with every outlet's telling under each event,
// which is the text the checks hold a line to.
func eventDetail(cs []cluster, now time.Time) string {
	var b strings.Builder
	for i, c := range cs {
		b.WriteString(strings.Replace(clusterList([]cluster{c}, now), "[1]", fmt.Sprintf("[%d]", i+1), 1))
		lead := c.lead().url
		for _, s := range c.stories {
			if s.url == lead {
				continue
			}
			fmt.Fprintf(&b, "  %s: %s", s.source, s.title)
			if s.desc != "" {
				b.WriteString(" | " + s.desc)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
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

	// A field per event keeps each headline tied to the event it's about.
	props := map[string]any{}
	var required []string
	for i := range events {
		key := fmt.Sprintf("event%d", i+1)
		props[key] = map[string]any{"type": "string"}
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
- Never say how many or which outlets carried it.
- Each event is its lead story, with how the other outlets told it indented under it.`

	when := map[string]string{"morning": "yesterday and overnight", "preopen": "yesterday, overnight and this morning",
		"midday": "so far today", "close": "today"}[slot.kind]
	user := fmt.Sprintf("It is %s Eastern. The most widely carried news %s.\n\nEvents:\n%s",
		at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), when, eventDetail(events, now))

	// A copied title, a field about another event, or a name or number the
	// event doesn't have gets one retry naming it, and is dropped if it comes back.
	var out map[string]string
	var bad map[int]string
	for try := 1; try <= 2; try++ {
		ask := user
		if len(bad) > 0 {
			ask += "\n\nYour last answer had problems, fix them:\n- " + strings.Join(slices.Collect(maps.Values(bad)), "\n- ")
		}
		out = nil
		if err := b.model.Structured(ctx, system, ask, schema, 600, &out); err != nil {
			return nil, nil, err
		}
		bad = readProblems(events, out)
		if len(bad) == 0 {
			break
		}
		slog.Info("brief read retried", slog.String("component", "brief"), slog.Int("try", try), slog.String("problems", strings.Join(slices.Collect(maps.Values(bad)), "; ")))
	}
	var parts []string
	if opener != "" {
		parts = append(parts, opener)
	}
	var cited []int
	for i := range events {
		t := stripParties(sentence(out[fmt.Sprintf("event%d", i+1)]), eventText(events[i]))
		if _, ok := bad[i+1]; ok || t == "" {
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
		slog.String("read_level", level), slog.Int("top", top), slog.Int("outlets", outlets),
		slog.String("lead", all[0].lead().title), slog.String("text", text))
	var covered []cluster
	for _, n := range cited {
		covered = append(covered, events[n-1])
	}
	return r, covered, nil
}

// readProblems is what's wrong with each read field, by its event number.
func readProblems(events []cluster, out map[string]string) map[int]string {
	bad := map[int]string{}
	for i, c := range events {
		key := fmt.Sprintf("event%d", i+1)
		t := out[key]
		switch {
		case t == "":
		case copiesTitle(t, c):
			bad[i+1] = key + " repeated the event's own title, write it in different words"
		case !relates(t, c):
			bad[i+1] = key + " isn't about event " + fmt.Sprint(i+1) + ", write each field about the event with its number"
		default:
			if miss := unsupported(t, eventText(c)); len(miss) > 0 {
				bad[i+1] = fmt.Sprintf("%s has %s, which event %d doesn't say, use only what it says", key, strings.Join(miss, ", "), i+1)
			}
		}
	}
	return bad
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

// eventRating is the model's read of one event, carrying the event's number
// back so a skipped or reordered rating can't land on the wrong event.
type eventRating struct {
	N      int    `json:"n"`
	Label  string `json:"label"`
	Impact int    `json:"impact"`
}

func (b *Briefer) rate(ctx context.Context, events []cluster, now time.Time) ([]eventRating, error) {
	system := `You rate news events, one rating for every event listed, each carrying the event's number as "n".

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

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ratings": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"n":      map[string]any{"type": "integer"},
						"label":  map[string]any{"type": "string", "enum": newsCategories},
						"impact": map[string]any{"type": "integer", "enum": []int{1, 2, 3, 4, 5}},
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
	if err := b.model.Structured(ctx, system, "Events:\n"+clusterList(events, now), schema, 40*len(events)+200, &out); err != nil {
		return nil, err
	}
	ratings := make([]eventRating, len(events))
	for _, r := range out.Ratings {
		if r.N >= 1 && r.N <= len(events) {
			ratings[r.N-1] = r
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
