package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Three times a day the local model reads what the feeds below have published
// and writes two things, a line or two on the markets under the strip and an
// executive summary of the news in place of a list of headlines. Headlines are
// written to be clicked, so the summary is told to restate what happened rather
// than repeat how an outlet framed it.

// lean is AllSides' rating of each outlet, L, C or R. It is never shown to the
// model as a reason to keep or drop anything, only counted per story so a story
// one side alone is covering can be marked as that rather than left out.
type briefFeed struct{ name, lean, endpoint, url string }

// AP and Reuters have no public feeds of their own any more, so they come
// through Google News searches restricted to their domains.
func googleNews(site string) string {
	return "https://news.google.com/rss/search?q=when:1d+site:" + site + "&hl=en-US&gl=US&ceid=US:en"
}

var newsFeeds = []briefFeed{
	{"AP", "C", "googlenews", googleNews("apnews.com")},
	{"REUTERS", "C", "googlenews", googleNews("reuters.com")},
	{"WSJ", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/RSSUSnews"},
	{"HILL", "C", "thehill", "https://thehill.com/news/feed/"},
	{"NEWSNATION", "C", "newsnation", "https://www.newsnationnow.com/feed/"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1001/rss.xml"},
	{"PBS", "L", "pbs", "https://www.pbs.org/newshour/feeds/rss/headlines"},
	{"CBS", "L", "cbs", "https://www.cbsnews.com/latest/rss/main"},
	{"FOX", "R", "fox", "https://moxie.foxnews.com/google-publisher/latest.xml"},
	{"EXAMINER", "R", "examiner", "https://www.washingtonexaminer.com/feed/"},
}

var marketFeeds = []briefFeed{
	{"REUTERS", "C", "googlenews", googleNews("reuters.com/business")},
	{"WSJ", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/RSSMarketsMain"},
	{"MW", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/mw_topstories"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	{"CNBC", "L", "cnbc", "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=20910258"},
	{"CNBC", "L", "cnbc", "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1017/rss.xml"},
}

// briefSlot is one of the three runs. The close runs a few minutes after the
// bell so the 4pm print has landed on Yahoo before the strip is read.
type briefSlot struct {
	kind         string
	hour, minute int

	// How far back a story may be and still count, from the slot.
	reach time.Duration

	// What the markets lines are about, one label each, in order.
	marketLines []string
}

var briefSlots = []briefSlot{
	{kind: "morning", hour: 7, reach: 24 * time.Hour, marketLines: []string{"YESTERDAY", "TODAY"}},
	{kind: "midday", hour: 11, reach: 6 * time.Hour, marketLines: []string{"SO FAR"}},
	{kind: "close", hour: 16, minute: 5, reach: 11 * time.Hour, marketLines: []string{"SESSION", "READ", "AHEAD"}},
}

const (
	// A failed run tries again after this, until the next slot replaces it.
	briefRetry = 15 * time.Minute

	newsEvents   = 50
	marketEvents = 40

	// Fewer than this and the feeds are down, and a summary of five stories is
	// a summary of whatever happened to load.
	briefFloor = 8
)

// Brief is one summary as the page draws it.
type Brief struct {
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Compiled string  `json:"compiled"`
	Stories  int     `json:"stories"`
	Outlets  string  `json:"outlets"`
	Points   []Point `json:"points"`

	// "off" when there is no model key, so the panel can say why it is empty.
	Status string `json:"status,omitempty"`

	// Why a slot that is due has not run yet, like the card being in use.
	Waiting string `json:"waiting,omitempty"`

	// The slot this answered, in unix seconds, which is how a restart knows
	// whether it is owed a run.
	Slot int64 `json:"slot"`
}

type Point struct {
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
	Links []Link `json:"links"`

	// higher, lower or mixed on the lines that look forward.
	Lean string `json:"lean,omitempty"`

	// Outlets that carried it per AllSides lean, like "L1 C3 R1".
	Coverage string `json:"coverage,omitempty"`
	Note     string `json:"note,omitempty"`
}

type Link struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

type Briefs struct {
	Markets Brief `json:"markets"`
	News    Brief `json:"news"`
}

func (s briefSlot) title() string {
	switch s.kind {
	case "morning":
		return "MORNING"
	case "midday":
		return "MIDDAY"
	}
	return "CLOSE"
}

// latestSlot is the most recent slot at or before now. Weekends run too,
// since a Sunday read of the week is a read of what Monday has coming.
func latestSlot(now time.Time) (briefSlot, time.Time) {
	now = now.In(easternTime())
	for back := 0; back < 2; back++ {
		day := now.AddDate(0, 0, -back)
		for i := len(briefSlots) - 1; i >= 0; i-- {
			s := briefSlots[i]
			at := time.Date(day.Year(), day.Month(), day.Day(), s.hour, s.minute, 0, 0, day.Location())
			if !at.After(now) {
				return s, at
			}
		}
	}
	return briefSlots[0], now
}

func weekend(t time.Time) bool {
	d := t.In(easternTime()).Weekday()
	return d == time.Saturday || d == time.Sunday
}

// briefModel is the part of the gateway the briefs use, so the schedule can
// be tested without a card.
type briefModel interface {
	Structured(ctx context.Context, system, user string, schema map[string]any, maxTok int, out any) error
	GPU(ctx context.Context) (GPUState, error)
	Unload(ctx context.Context) error
}

// A card the desktop is using is asked again in an hour, so a weekend of games
// costs one probe an hour and never a model load.
const briefDefer = time.Hour

// Briefer owns the schedule and the file the last two briefs are kept in, so a
// deploy at 9am still shows the morning one rather than nothing until 11.
type Briefer struct {
	store *Store
	guard *Guard
	model briefModel
	path  string

	// Nothing is tried before this, whether the card was busy or a run failed.
	next time.Time

	compile func(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error)
}

func NewBriefer(store *Store, g *Guard, m *Model, dataDir string) *Briefer {
	b := &Briefer{store: store, guard: g, path: filepath.Join(dataDir, "briefs.json")}
	b.compile = b.compileDesk
	if m != nil {
		b.model = m
	}

	var saved Briefs
	if raw, err := os.ReadFile(b.path); err == nil {
		if err := json.Unmarshal(raw, &saved); err != nil {
			slog.Warn("briefs file unreadable", slog.String("component", "brief"), slog.Any("err", err))
		}
	}
	saved.Markets.Waiting, saved.News.Waiting = "", ""
	if m == nil {
		saved.Markets.Status, saved.News.Status = "off", "off"
	}
	store.update(func(st *State) { st.Briefs = saved })
	return b
}

func (b *Briefer) Run(ctx context.Context) {
	if b.model == nil {
		slog.Warn("no LLM_KEY, briefs are off", slog.String("component", "brief"))
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		b.tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick runs both desks back to back on one model load when a slot is owed,
// after asking whether the desktop is using the card.
func (b *Briefer) tick(ctx context.Context, now time.Time) {
	slot, at := latestSlot(now)
	cur := b.store.Snapshot().Briefs
	var due []string
	if cur.Markets.Slot < at.Unix() {
		due = append(due, "markets")
	}
	if cur.News.Slot < at.Unix() {
		due = append(due, "news")
	}
	if len(due) == 0 || now.Before(b.next) {
		return
	}

	gpu, err := b.model.GPU(ctx)
	if err != nil {
		b.next = now.Add(briefRetry)
		slog.Warn("brief could not ask about the card", slog.String("component", "brief"), slog.Any("err", err))
		return
	}
	if gpu.Busy {
		b.next = now.Add(briefDefer)
		slog.Info("brief deferred", slog.String("component", "brief"), slog.String("slot", slot.kind),
			slog.String("reason", gpu.Reason), slog.Time("next", b.next))
		b.waiting(fmt.Sprintf("CARD IN USE, NEXT TRY %s", b.next.In(easternTime()).Format("15:04")))
		return
	}
	// Only weights this run put on the card come off it. A model already there
	// is somebody's conversation, and its own idle timer is theirs.
	if !gpu.Loaded {
		defer func() {
			if err := b.model.Unload(context.WithoutCancel(ctx)); err != nil {
				slog.Warn("brief could not unload", slog.String("component", "brief"), slog.Any("err", err))
			}
		}()
	}

	failed := false
	for _, desk := range due {
		started := time.Now()
		brief, err := b.compile(ctx, desk, slot, at)
		if err != nil {
			failed = true
			slog.Warn("brief failed", slog.String("component", "brief"), slog.String("desk", desk),
				slog.String("slot", slot.kind), slog.Any("err", err))
			continue
		}
		slog.Info("brief written", slog.String("component", "brief"), slog.String("desk", desk),
			slog.String("slot", slot.kind), slog.Int("stories", brief.Stories),
			slog.Duration("took", time.Since(started).Round(time.Second)))

		b.store.update(func(st *State) {
			if desk == "markets" {
				st.Briefs.Markets = brief
			} else {
				st.Briefs.News = brief
				st.setNotices("news", briefNotices(brief))
			}
		})
	}
	if failed {
		b.next = now.Add(briefRetry)
	}
	b.waiting("")
	b.save()
}

func (b *Briefer) compileDesk(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error) {
	if desk == "markets" {
		return b.compileMarkets(ctx, slot, at)
	}
	return b.compileNews(ctx, slot, at)
}

// waiting says on both panels why the last brief is still up.
func (b *Briefer) waiting(why string) {
	b.store.update(func(st *State) { st.Briefs.Markets.Waiting, st.Briefs.News.Waiting = why, why })
}

func (b *Briefer) save() {
	raw, err := json.Marshal(b.store.Snapshot().Briefs)
	if err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		err = os.Rename(tmp, b.path)
	}
	if err != nil {
		slog.Warn("briefs not saved", slog.String("component", "brief"), slog.Any("err", err))
	}
}

func briefNotices(b Brief) []Notice {
	if len(b.Points) == 0 {
		return nil
	}
	return []Notice{{
		ID: fmt.Sprintf("brief:%d", b.Slot), Kind: "news",
		Title: "TODAY, " + b.Title, Body: b.Points[0].Text, URL: baseURL + "/",
	}}
}

// story is one item off a feed.
type story struct {
	title, desc, url, source, lean string
	at                             time.Time
	words                          []string
}

// cluster is one event as every outlet that carried it told it.
type cluster struct {
	stories []story
	members []int
	newest  time.Time
}

// distinctive drops the words a big share of the pile has in its titles. On
// a day the administration is in every headline, trump, admin and feder are
// in half of them, and two unrelated stories share four words without trying.
func distinctive(all []story) [][]string {
	df := map[string]int{}
	for _, s := range all {
		for _, w := range s.words {
			df[w]++
		}
	}
	common := max(4, len(all)/15)
	out := make([][]string, len(all))
	for i, s := range all {
		for _, w := range s.words {
			if df[w] <= common {
				out[i] = append(out[i], w)
			}
		}
	}
	return out
}

// Three rare words in common is one event. The common ones are already gone,
// so three of what is left is a lot.
func sameEvent(a, b []string) bool {
	shared := 0
	for _, w := range a {
		if slices.Contains(b, w) {
			shared++
		}
	}
	return shared >= 3
}

// A Google News search hands back a hundred items, so each feed is held to
// its newest few or AP and Reuters would be most of the pile.
const perFeed = 25

// gather reads every feed and keeps what landed between since and now.
func gather(ctx context.Context, g *Guard, feeds []briefFeed, since, now time.Time) []story {
	seen := map[string]bool{}
	var all []story
	for _, feed := range feeds {
		items, err := fetchRSS(ctx, g, feed.endpoint, feed.url)
		if err != nil {
			slog.Warn("brief feed failed", slog.String("component", "brief"), slog.String("feed", feed.url), slog.Any("err", err))
			continue
		}
		var mine []story
		for _, it := range items.Channel.Items {
			title := strings.TrimSpace(html.UnescapeString(it.Title))
			desc := blurb(it.Description)
			if feed.endpoint == "googlenews" {
				// Its titles end in " - AP News" and its description is the
				// title again wrapped in a link.
				if i := strings.LastIndex(title, " - "); i > 0 {
					title = title[:i]
				}
				desc = ""
			}
			if title == "" || it.Link == "" || promotional(title) || sidebar(title) || clip(it.Link) {
				continue
			}
			key := it.GUID
			if key == "" {
				key = it.Link
			}
			if seen[key] || seen[titleKey(title)] {
				continue
			}
			seen[key], seen[titleKey(title)] = true, true

			at, err := parseRSSTime(it.PubDate)
			if err != nil || at.Before(since) || at.After(now.Add(10*time.Minute)) {
				continue
			}
			if strings.HasPrefix(desc, title) {
				desc = ""
			}
			mine = append(mine, story{title: title, desc: desc, url: it.Link, source: feed.name,
				lean: feed.lean, at: at, words: significant(title)})
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].at.After(mine[j].at) })
		all = append(all, mine[:min(len(mine), perFeed)]...)
	}
	return all
}

// clusterStories groups the same event across outlets and ranks the groups by
// how many outlets carried them, which is the closest thing to "this
// mattered" that does not depend on any one newsroom's judgement.
func clusterStories(all []story, limit int) []cluster {
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	keys := distinctive(all)
	var out []cluster
	for n, s := range all {
		i := slices.IndexFunc(out, func(c cluster) bool {
			return slices.ContainsFunc(c.members, func(m int) bool { return sameEvent(keys[m], keys[n]) })
		})
		if i < 0 {
			out = append(out, cluster{newest: s.at})
			i = len(out) - 1
		}
		out[i].stories = append(out[i].stories, s)
		out[i].members = append(out[i].members, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := len(out[i].outlets()), len(out[j].outlets())
		if a != b {
			return a > b
		}
		return out[i].newest.After(out[j].newest)
	})
	return out[:min(len(out), limit)]
}

func (c cluster) outlets() []string {
	var names []string
	for _, s := range c.stories {
		if !slices.Contains(names, s.source) {
			names = append(names, s.source)
		}
	}
	return names
}

// lead is the telling the model reads, a center outlet's where there is one,
// since the point is to hand it the least framed headline available.
func (c cluster) lead() story {
	best := c.stories[0]
	score := func(s story) int {
		n := 0
		if s.lean == "C" {
			n += 2
		}
		if s.desc != "" {
			n++
		}
		return n
	}
	for _, s := range c.stories[1:] {
		if score(s) > score(best) {
			best = s
		}
	}
	return best
}

var tags = regexp.MustCompile(`<[^>]*>`)

// blurb is the feed's own description cut to a sentence or two. CBS and PBS
// wrap theirs in markup.
func blurb(desc string) string {
	s := html.UnescapeString(tags.ReplaceAllString(desc, " "))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 240 {
		s = string(r[:240])
		if i := strings.LastIndex(s, " "); i > 160 {
			s = s[:i]
		}
		s += "..."
	}
	return s
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
	if len(shared) < 12 {
		return cs
	}
	return shared
}

func outletNames(cs []cluster) string {
	var names []string
	for _, c := range cs {
		for _, n := range c.outlets() {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	return strings.Join(names, " / ")
}

func storyCount(cs []cluster) int {
	n := 0
	for _, c := range cs {
		n += len(c.stories)
	}
	return n
}

// clusterList numbers the events from 1 for the model to cite by. The lean of
// each outlet stays out of it, since a model told which side a story came from
// starts writing about the sides.
func clusterList(cs []cluster, now time.Time) string {
	var b strings.Builder
	for i, c := range cs {
		l := c.lead()
		names := c.outlets()
		fmt.Fprintf(&b, "[%d] %d outlet", i+1, len(names))
		if len(names) > 1 {
			b.WriteString("s")
		}
		fmt.Fprintf(&b, " (%s), %s ago: %s", strings.Join(names, ", "), ago(now.Sub(c.newest)), l.title)
		if l.desc != "" {
			b.WriteString(" | " + l.desc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ago(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 1))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// The rules both prompts share. They are about register and not about any
// side, since the reader asked for what happened and not for a take on it.
const neutralRules = `Rules:
- Write in AP wire style. Past tense, who did what and when, no adjectives that judge.
- Restate what happened in your own plain words. Never copy a headline's framing.
- Neutral verbs only: said, announced, voted, proposed, ruled, filed. Never slammed, blasted, admitted, claimed, touted, doubled down, or words like chaos, radical, extreme, shocking, historic, unless quoting someone by name.
- Name people by office and party, like "Sen. Jane Doe (R-Texas)". No labels like far-right or far-left unless quoting someone by name.
- Attribute every claim, accusation or prediction to whoever made it. Never state one side's description of events as fact.
- On a contested political story, give each side's stated position in one clause each, of about equal length.
- If the stories disagree on a fact, say reports differ.
- Never pad. Do not write what a story did not say, and add a second sentence only when it carries another fact from the stories.
- Use only facts in the stories given. No outside knowledge, no guessing at motives, no opinion, no advice, no "what this means" unless a named source said it.
- Include a story whichever party or side it reflects well or badly on.
- Do not put story numbers or outlet names in the text. The page shows the sources beside each point.`

// A small model told to skip trials will still write one up, and asked to label
// the point it wrote calls a trial politics. Labelling the list on its own first
// is an easier job, so the skipping happens in Go before the summary.
var newsCategories = []string{"us_politics", "world", "economy", "markets", "technology", "disaster",
	"security", "culture", "crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

var minor = []string{"crime_or_court", "incident", "human_interest", "sport", "celebrity", "local"}

// mainstream is how many outlets have to carry a minor story before it counts
// as mainstream culture anyway, the way the Lindsay Clancy trial was.
const mainstream = 7

var newsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"points": map[string]any{
			"type": "array", "minItems": 3, "maxItems": 8,
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":    map[string]any{"type": "string", "maxLength": 120},
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

type modelPoints struct {
	Points []struct {
		Text    string `json:"text"`
		Sources []int  `json:"sources"`
	} `json:"points"`
}

func (b *Briefer) compileNews(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	now := time.Now()
	events := widelyCarried(clusterStories(gather(ctx, b.guard, newsFeeds, at.Add(-slot.reach), now), newsEvents))
	if len(events) < briefFloor {
		return Brief{}, fmt.Errorf("only %d events", len(events))
	}
	labels, err := b.classify(ctx, events, now)
	if err != nil {
		return Brief{}, fmt.Errorf("classify: %w", err)
	}
	events = keepMajor(events, labels)
	if len(events) < 3 {
		return Brief{}, fmt.Errorf("only %d major events", len(events))
	}

	task := map[string]string{
		"morning": "Summarise what happened yesterday and overnight.",
		"midday":  "Summarise the news so far today.",
		"close":   "Summarise how the day went in the news.",
	}[slot.kind]

	system := "You write a short executive news summary for one reader in the United States.\n" + neutralRules + `
- The events are listed most widely covered first. Keep roughly that order, and prefer events many outlets carried over ones only one did.
- Write 3 to 8 points. Each point is one neutral headline about exactly one event, at most 14 words, in past tense. No second sentence, no detail beyond what makes the event clear.
- Include an event only if it affects US politics, world politics, the markets or the economy, or mainstream culture in a big way. Major technology news and disasters affecting many people count.
- Skip individual crime and court cases, trials, human interest, local stories, celebrity, sport and lifestyle. The exception is a story 7 or more outlets carried, which has become mainstream culture in its own right.
- Fewer points is better than filler. On a quiet day write three.
- Each point cites the numbers of every event it draws on in "sources".`

	user := fmt.Sprintf("It is %s Eastern. %s\n\nEvents:\n%s", at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), task, clusterList(events, now))

	var out modelPoints
	if err := b.model.Structured(ctx, system, user, newsSchema, 900, &out); err != nil {
		return Brief{}, err
	}

	brief := newBrief(slot, at, events)
	for _, p := range out.Points {
		pt := cite(events, p.Sources)
		pt.Text = tidy(p.Text)
		// A point the model could not tie to a story is one it may have made up.
		if pt.Text == "" || len(pt.Links) == 0 {
			continue
		}
		brief.Points = append(brief.Points, pt)
	}
	if len(brief.Points) < 2 {
		return Brief{}, fmt.Errorf("only %d cited points", len(brief.Points))
	}
	return brief, nil
}

func (b *Briefer) classify(ctx context.Context, events []cluster, now time.Time) ([]string, error) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"labels": map[string]any{
				"type": "array", "minItems": len(events), "maxItems": len(events),
				"items": map[string]any{"type": "string", "enum": newsCategories},
			},
		},
		"required":             []string{"labels"},
		"additionalProperties": false,
	}
	system := `You label news events by topic, one label per event, in the order given.
- us_politics: Congress, the White House, federal agencies and policy, elections, parties.
- crime_or_court: a criminal case, arrest, trial, sentencing or execution of particular people, even when officials are involved.
- security: war, terrorism, the military and intelligence, when it is about a country and not one suspect.
- incident: a shooting, attack, crash, fire or accident with a handful of victims, in any country, that changes nothing beyond the place it happened.
- human_interest: one person's or family's story.
- local: a story about one city or town.
The rest mean what they say.`
	var out struct {
		Labels []string `json:"labels"`
	}
	if err := b.model.Structured(ctx, system, "Events:\n"+clusterList(events, now), schema, 20*len(events)+100, &out); err != nil {
		return nil, err
	}
	return out.Labels, nil
}

// keepMajor drops the minor events, unless so many outlets carried one that it
// is mainstream anyway. A label list the wrong length keeps everything, since
// it cannot be lined up with the events.
func keepMajor(events []cluster, labels []string) []cluster {
	if len(labels) != len(events) {
		return events
	}
	var out []cluster
	for i, c := range events {
		if slices.Contains(minor, labels[i]) && len(c.outlets()) < mainstream {
			slog.Info("brief event dropped as minor", slog.String("component", "brief"),
				slog.String("category", labels[i]), slog.String("title", c.lead().title))
			continue
		}
		out = append(out, c)
	}
	return out
}

func marketSchema(lines int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"points": map[string]any{
				"type": "array", "minItems": lines, "maxItems": lines,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"lean":    map[string]any{"type": "string", "enum": []string{"higher", "lower", "mixed", "none"}},
						"text":    map[string]any{"type": "string", "maxLength": 300},
						"sources": map[string]any{"type": "array", "maxItems": 3, "items": map[string]any{"type": "integer"}},
					},
					"required":             []string{"lean", "text", "sources"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"points"},
		"additionalProperties": false,
	}
}

func (b *Briefer) compileMarkets(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	// The strip idles at five minutes with nobody watching, and the close
	// wants the 4pm print rather than one from before the bell.
	b.store.refreshMarket(ctx, b.guard)
	b.store.refreshBoard(ctx, b.guard)

	now := time.Now()
	// A Saturday or Sunday has little market news of its own, so a weekend
	// brief reads back to Friday morning.
	reach := slot.reach
	if weekend(at) {
		reach = max(reach, 72*time.Hour)
	}
	events := clusterStories(gather(ctx, b.guard, marketFeeds, at.Add(-reach), now), marketEvents)
	if len(events) < briefFloor {
		return Brief{}, fmt.Errorf("only %d events", len(events))
	}

	b.store.mu.RLock()
	h := b.store.history
	b.store.mu.RUnlock()
	st := b.store.Snapshot()

	var asks []string
	labels := slot.marketLines
	switch {
	case weekend(at):
		asks = []string{
			"how last week ended for stocks and the main reason given",
			"which way the market as a whole leans for Monday and the two or three biggest market-wide reasons",
		}
		labels = []string{"LAST WEEK", "MONDAY"}
	case slot.kind == "morning":
		asks = []string{
			"how the previous session went and the main reason given for it",
			"which way the market as a whole leans today and the two or three biggest market-wide reasons",
		}
	case slot.kind == "midday":
		asks = []string{"how today's session is going so far and the main reason given for it"}
	default:
		asks = []string{
			"how today's session closed and the main reason given for it",
			"the broader read: what led and lagged, rates, and whether today fits the recent trend",
			"which way the market as a whole leans for tomorrow and the two or three biggest market-wide reasons",
		}
	}
	var want strings.Builder
	for i, a := range asks {
		fmt.Fprintf(&want, "Line %d: %s.\n", i+1, a)
	}

	system := "You write a one line markets summary for a dashboard, for one reader who invests in index funds.\n" + neutralRules + `
- Each line is one short sentence of at most 25 words. Name companies and events, never just "earnings" or "data".
- Quote figures exactly as given in the numbers. Never compute or invent a figure.
- A reason for a move has to come from a story, and is cited. If no story gives one, say what moved and leave the reason out.
- No advice, nothing like "investors should".
- The line asking which way things lean is about the whole market, never single companies. Its reasons come from market-wide drivers: futures, Treasury yields and the Fed, scheduled economic data, oil, the VIX and the recent trend, and geopolitical news. Mention a company only if it is one of the very largest in the S&P 500. It sets "lean" to higher, lower or mixed, for which way its reasons point on balance, and names each reason and which way it pushes, like "Futures are flat, but yields at multi-decade highs and a rising VIX weigh on stocks."
- Do not write the lean itself into the text, the page shows it beside the line. The text is only the reasons.
- Every other line sets "lean" to none and makes no prediction.`

	user := fmt.Sprintf("It is %s Eastern.\n\n%s\nNumbers right now:\n%s\nStories, most widely covered first:\n%s",
		at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), want.String(),
		marketFacts(st, h, slot), clusterList(events, now))

	var out struct {
		Points []struct {
			Lean    string `json:"lean"`
			Text    string `json:"text"`
			Sources []int  `json:"sources"`
		} `json:"points"`
	}
	if err := b.model.Structured(ctx, system, user, marketSchema(len(asks)), 900, &out); err != nil {
		return Brief{}, err
	}

	brief := newBrief(slot, at, events)
	if weekend(at) {
		brief.Title = "WEEKEND"
	}
	for i, p := range out.Points {
		pt := cite(events, p.Sources)
		pt.Text = tidy(p.Text)
		if pt.Text == "" {
			continue
		}
		pt.Label = labels[min(i, len(labels)-1)]
		if forward(pt.Label) && p.Lean != "none" {
			pt.Lean = p.Lean
		}
		brief.Points = append(brief.Points, pt)
	}
	if len(brief.Points) == 0 {
		return Brief{}, fmt.Errorf("no lines")
	}
	return brief, nil
}

// marketFacts is everything on the page the model may quote, written out the
// way the cards show it so a figure in the summary matches the one above it.
func marketFacts(st State, h *history, slot briefSlot) string {
	var b strings.Builder
	m := st.Market
	fmt.Fprintf(&b, "Session: %s %s\n", m.Session, m.Phase)
	for _, c := range m.Cards {
		if c.Unavailable {
			continue
		}
		fmt.Fprintf(&b, "%s (%s): %s, %s, %s", c.Label, c.Symbol, c.Price, c.Change, c.Percent)
		if c.Note != "" {
			fmt.Fprintf(&b, ", %s", strings.ToLower(c.Note))
		}
		b.WriteString("\n")
	}
	if (slot.kind == "morning" || weekend(time.Now())) && h != nil && len(h.closes) >= 2 {
		last, prev := h.closes[len(h.closes)-1], h.closes[len(h.closes)-2]
		fmt.Fprintf(&b, "S&P 500 previous session close: %.2f, %+.2f%%\n", last, (last-prev)/prev*100)
	}
	if m.Drawdown != "" {
		fmt.Fprintf(&b, "S&P 500 from its 52 week high: %s\n", m.Drawdown)
	}
	if st.Signal.Headline != "" {
		fmt.Fprintf(&b, "Conditions: %s\n", strings.ToLower(st.Signal.Headline))
	}
	for _, c := range st.Signal.Conditions {
		if c.Known {
			fmt.Fprintf(&b, "S&P 500 %s (%s): %s, %s\n", strings.ToLower(c.Label), strings.ToLower(c.Note), c.Value, strings.ToLower(c.State))
		}
	}
	for _, r := range st.Rates.Rows {
		if !r.Unavailable {
			fmt.Fprintf(&b, "%s Treasury yield: %s, %s\n", r.Label, r.Yield, r.Change)
		}
	}
	if st.Rates.Curve != "" {
		fmt.Fprintf(&b, "10Y minus 3M spread: %s\n", st.Rates.Curve)
	}
	var sectors []string
	for _, s := range st.Sectors {
		if !s.Unavailable && !s.Benchmark {
			sectors = append(sectors, s.Label+" "+s.Percent)
		}
	}
	if len(sectors) > 0 {
		fmt.Fprintf(&b, "Sectors best to worst: %s\n", strings.Join(sectors, ", "))
	}
	for _, e := range st.Earnings.Reported {
		fmt.Fprintf(&b, "%s (%s) reported %s %s: EPS %s against %s forecast, %s, stock %s\n",
			e.Name, e.Symbol, strings.ToLower(e.Day), session(e.When), e.Actual, e.Forecast, strings.ToLower(e.Verdict), e.Move)
	}
	for _, e := range st.Earnings.Upcoming {
		fmt.Fprintf(&b, "%s (%s) reports %s %s, EPS estimate %s\n", e.Name, e.Symbol, strings.ToLower(e.Day), session(e.When), e.Est)
	}
	return b.String()
}

func newBrief(slot briefSlot, at time.Time, events []cluster) Brief {
	return Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(), Stories: storyCount(events),
		Outlets: outletNames(events), Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
}

func forward(label string) bool {
	return label == "TODAY" || label == "AHEAD" || label == "MONDAY"
}

func session(when string) string {
	switch when {
	case "PRE":
		return "before the open"
	case "POST":
		return "after the close"
	}
	return ""
}

// cite turns the model's event numbers into links, one per outlet with the
// center ones first, and counts who carried the point on each side. A number
// that is not an event is dropped.
func cite(events []cluster, nums []int) Point {
	var pt Point
	var picked []story
	for _, n := range nums {
		if n < 1 || n > len(events) {
			continue
		}
		for _, s := range events[n-1].stories {
			if !slices.ContainsFunc(picked, func(p story) bool { return p.source == s.source }) {
				picked = append(picked, s)
			}
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].lean == "C" && picked[j].lean != "C" })

	sides := map[string]int{}
	for _, s := range picked {
		sides[s.lean]++
	}
	for _, s := range picked[:min(len(picked), 4)] {
		pt.Links = append(pt.Links, Link{Source: s.source, URL: s.url, Title: s.title})
	}
	if len(picked) > 0 {
		pt.Coverage = fmt.Sprintf("L%d C%d R%d", sides["L"], sides["C"], sides["R"])
	}
	// Ground News' blindspot: a story only one side's outlets are running is
	// marked as that and kept, since dropping it would be its own kind of spin.
	if len(picked) >= 2 && sides["C"] == 0 {
		if sides["R"] == 0 {
			pt.Note = "LEFT-LEANING OUTLETS ONLY"
		} else if sides["L"] == 0 {
			pt.Note = "RIGHT-LEANING OUTLETS ONLY"
		}
	}
	return pt
}

var citeMarks = regexp.MustCompile(`\s*\[\d+(?:\s*,\s*\d+)*\]`)

// tidy strips the bracketed numbers a model writes into the text even when
// told not to, and the dashes the page's type does not use.
func tidy(s string) string {
	s = citeMarks.ReplaceAllString(s, "")
	s = strings.NewReplacer(" — ", ", ", "—", ", ", " – ", ", ").Replace(s)
	return strings.TrimSpace(s)
}
