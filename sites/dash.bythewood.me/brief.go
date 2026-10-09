package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/world/rss.xml"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1001/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1014/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1004/rss.xml"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1006/rss.xml"},
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
}

var briefSlots = []briefSlot{
	{kind: "morning", hour: 7, reach: 24 * time.Hour},
	{kind: "midday", hour: 11, reach: 6 * time.Hour},
	{kind: "close", hour: 16, minute: 5, reach: 11 * time.Hour},
}

const (
	// A failed run tries again after this, until the next slot replaces it.
	briefRetry = 15 * time.Minute

	newsEvents   = 60
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

	// higher, lower or mixed on the lines that look forward, and up, down or
	// flat on the ones that already happened, which also carry the move.
	Lean string `json:"lean,omitempty"`
	Move string `json:"move,omitempty"`

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
	leans *leanBook

	// Nothing is tried before this, whether the card was busy or a run failed.
	next time.Time

	compile func(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error)
}

func NewBriefer(store *Store, g *Guard, m *Model, dataDir string) *Briefer {
	b := &Briefer{store: store, guard: g, path: filepath.Join(dataDir, "briefs.json"), leans: openLeanBook(dataDir)}
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
	if b.leans.add(saved.Markets) {
		b.leans.save()
	}
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
	b.store.mu.RLock()
	h := b.store.history
	b.store.mu.RUnlock()
	if b.leans.score(h, now) {
		b.leans.save()
	}

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
		logPoints(desk, slot.kind, brief)
		if desk == "markets" && b.leans.add(brief) {
			b.leans.save()
		}

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

// logPoints puts every line of a brief in logging, since the page and the file
// only keep the latest one and a brief can't be judged after it's replaced.
func logPoints(desk, slot string, b Brief) {
	for i, p := range b.Points {
		cited := make([]string, len(p.Links))
		for j, l := range p.Links {
			cited[j] = l.Source + ": " + l.Title
		}
		slog.Info("brief point", slog.String("component", "brief"), slog.String("desk", desk),
			slog.String("slot", slot), slog.Int("rank", i+1), slog.String("label", p.Label),
			slog.String("lean", p.Lean), slog.String("move", p.Move), slog.String("coverage", p.Coverage),
			slog.String("text", p.Text), slog.String("cited", strings.Join(cited, " | ")))
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
	// Twice the most points a brief can hold, so there's still a choice left
	// once the minor ones are gone.
	if len(shared) < 24 {
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

// newsPoints is the most a brief holds, and so the most events the writer sees.
const newsPoints = 12

func newsSchema(events int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"points": map[string]any{
				"type": "array", "minItems": min(3, events), "maxItems": events,
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
	ratings, err := b.rate(ctx, events, now)
	if err != nil {
		return Brief{}, fmt.Errorf("rate: %w", err)
	}
	events = mostImpact(events, ratings, newsPoints)
	if len(events) < 3 {
		return Brief{}, fmt.Errorf("only %d major events", len(events))
	}

	task := map[string]string{
		"morning": "Summarise what happened yesterday and overnight.",
		"midday":  "Summarise the news so far today.",
		"close":   "Summarise how the day went in the news.",
	}[slot.kind]

	system := "You write a short executive news summary for one reader in the United States.\n" + neutralRules + `
- Write one point for each event, in the order given. If two events are the same story, write one point for them and cite both.
- Each point is one neutral headline about exactly one event, at most 14 words, in past tense. No second sentence, no detail beyond what makes the event clear.
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

// eventRating is the model's read of one event. Each carries its number back, since
// a small model handed sixty events and asked for sixty labels in order drifts
// a place or two partway down and labels the futures story an incident.
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

// mostImpact drops the minor events, unless so many outlets carried one that it
// is mainstream anyway, and the ones rated as changing nothing, then keeps the
// highest rated up to limit. Ties stay in the order they came, most outlets
// first. An event the model skipped is kept and ranked as middling.
func mostImpact(events []cluster, ratings []eventRating, limit int) []cluster {
	type ranked struct {
		c      cluster
		impact int
	}
	var keep []ranked
	for i, c := range events {
		r := eventRating{Impact: 3}
		if i < len(ratings) && ratings[i].N != 0 {
			r = ratings[i]
		}
		drop := (slices.Contains(minor, r.Label) && len(c.outlets()) < mainstream) || r.Impact <= 1
		msg := "brief event kept"
		if drop {
			msg = "brief event dropped"
		}
		slog.Info(msg, slog.String("component", "brief"), slog.String("category", r.Label),
			slog.Int("impact", r.Impact), slog.Int("outlets", len(c.outlets())), slog.String("title", c.lead().title))
		if !drop {
			keep = append(keep, ranked{c, r.Impact})
		}
	}
	sort.SliceStable(keep, func(i, j int) bool { return keep[i].impact > keep[j].impact })
	var out []cluster
	for _, k := range keep[:min(len(keep), limit)] {
		out = append(out, k.c)
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
	// YESTERDAY needs the stories about yesterday's close whatever the slot,
	// and a weekend brief reads back to Friday morning.
	reach := max(slot.reach, 30*time.Hour)
	if weekend(at) {
		reach = max(reach, 72*time.Hour)
	}
	events := clusterStories(gather(ctx, b.guard, marketFeeds, at.Add(-reach), now), marketEvents)
	if len(events) < briefFloor {
		return Brief{}, fmt.Errorf("only %d events", len(events))
	}

	b.store.refreshSignal(ctx, b.guard)
	b.store.mu.RLock()
	h := b.store.history
	b.store.mu.RUnlock()
	st := b.store.Snapshot()

	lines := marketLines(h, st.Market.Session, at)
	asks := make([]string, len(lines))
	releases := b.store.releasesOrFetch(ctx, b.guard)
	var calendar strings.Builder
	for i, l := range lines {
		asks[i] = l.ask
		if !l.forward {
			continue
		}
		fmt.Fprintf(&calendar, "%s, %s:\n", l.label, l.day.Format("Monday January 2"))
		notes := sessionNotes(l.day, releases, vixLevel(st))
		if len(notes) == 0 {
			calendar.WriteString("- Nothing scheduled and no calendar pattern.\n")
		}
		for _, n := range notes {
			calendar.WriteString("- " + n + "\n")
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
- Each story says how long ago it ran. Explain a session only with stories from around that session, so a story about this morning's futures is never the reason for yesterday's close.
- No advice, nothing like "investors should".
- A line asking which way things lean is about the whole market, never single companies. Its reasons come from market-wide drivers: futures, Treasury yields and the Fed, the calendar and patterns listed for that day, oil, the VIX and the recent trend, and geopolitical news. A scheduled Fed decision or big release always gets named. The patterns are mild tilts, so they settle a close call and never outweigh the news. Mention a company only if it is one of the very largest in the S&P 500. It sets "lean" to higher, lower or mixed, for which way its reasons point on balance, and names each reason and which way it pushes, like "Futures are flat, but yields at multi-decade highs and a rising VIX weigh on stocks."
- Do not write the lean itself into the text, the page shows it beside the line. The text is only the reasons.
- The line for the next session names what the calendar has for that day, or says nothing major is scheduled, and never repeats the line before it.
- A line about a session that already happened gives the main reason for its move, and leaves out the direction and the percent, since the page shows them beside it.
- Every line that is not asking which way things lean sets "lean" to none and makes no prediction.`

	user := fmt.Sprintf("It is %s Eastern.\n\n%s\nNumbers right now:\n%s\nCalendar and patterns:\n%s\nStories, most widely covered first:\n%s",
		at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), want.String(),
		marketFacts(st, h, at), calendar.String(), clusterList(events, now))

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
		if i >= len(lines) {
			break
		}
		l := lines[i]
		pt := cite(events, p.Sources)
		pt.Text = tidy(p.Text)
		if pt.Text == "" {
			continue
		}
		pt.Label = l.label
		switch {
		case l.move != "":
			pt.Lean, pt.Move = l.dir, l.move
		case l.forward && p.Lean != "none":
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
func marketFacts(st State, h *history, at time.Time) string {
	var b strings.Builder
	if h != nil {
		today := at.In(easternTime()).Format("2006-01-02")
		var moves []string
		bars := dailyBars(h, easternTime())
		for i := max(1, len(bars)-5); i < len(bars); i++ {
			if bars[i].date < today {
				moves = append(moves, fmt.Sprintf("%s %+.2f%%", bars[i].day.Format("Mon Jan 2"), (bars[i].close-bars[i-1].close)/bars[i-1].close*100))
			}
		}
		if len(moves) > 0 {
			fmt.Fprintf(&b, "S&P 500 recent closes, oldest first: %s\n", strings.Join(moves, ", "))
		}
	}
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

// marketLine is one line of the markets brief before the model writes it. A
// session that already happened has its move worked out here from the daily
// closes, so the up or down beside it is a fact and never the model's guess.
type marketLine struct {
	label   string
	ask     string
	forward bool
	dir     string
	move    string

	// The session a forward line is about.
	day time.Time
}

// marketLines is always the last session, today and the next, so the strip has
// the same three answers at 7am as at 4pm. On a weekend it's Friday, the week
// and Monday.
func marketLines(h *history, session string, at time.Time) []marketLine {
	et := easternTime()
	now := at.In(et)
	today := now.Format("2006-01-02")
	next := nextWeekday(now)

	var bars []dayBar
	if h != nil {
		bars = dailyBars(h, et)
	}
	before := slices.IndexFunc(bars, func(b dayBar) bool { return b.date >= today })
	if before < 0 {
		before = len(bars)
	}
	past, current := bars[:before], bars[before:]

	if weekend(at) {
		var lines []marketLine
		if n := len(past); n >= 2 {
			lines = append(lines, closedLine(dayName(past[n-1].day, now), past[n-1].close, past[n-2].close,
				"the main reason given for the S&P 500's move on Friday"))
		}
		if n := len(past); n >= 6 {
			lines = append(lines, closedLine("LAST WEEK", past[n-1].close, past[n-6].close,
				"the main reason given for how stocks did over the past week"))
		}
		return append(lines, marketLine{label: dayName(next, now), forward: true, day: next,
			ask: "which way the market as a whole leans for Monday and the two or three biggest market-wide reasons"})
	}

	var lines []marketLine
	if n := len(past); n >= 2 {
		day := past[n-1].day
		lines = append(lines, closedLine(dayName(day, now), past[n-1].close, past[n-2].close,
			"the main reason given for the S&P 500's move on "+day.Format("Monday")))
	}
	if len(current) > 0 && len(past) > 0 && session != "pre" {
		l := closedLine("TODAY", current[0].close, past[len(past)-1].close,
			"the main reason given for how today's session closed")
		if session == "regular" {
			l.move += " SO FAR"
			l.ask = "the main reason given for how today's session is going so far"
		}
		lines = append(lines, l)
	} else {
		lines = append(lines, marketLine{label: "TODAY", forward: true, day: now,
			ask: "which way the market as a whole leans today and the two or three biggest market-wide reasons"})
	}
	return append(lines, marketLine{label: dayName(next, now), forward: true, day: next,
		ask: "which way the market as a whole leans for " + next.Format("Monday") + ", the next session, and the two or three biggest market-wide reasons"})
}

func closedLine(label string, close, prev float64, ask string) marketLine {
	pct := (close - prev) / prev * 100
	dir := "flat"
	switch {
	case pct >= 0.1:
		dir = "up"
	case pct <= -0.1:
		dir = "down"
	}
	return marketLine{label: label, ask: ask, dir: dir, move: fmt.Sprintf("%.2f%%", math.Abs(pct))}
}

type dayBar struct {
	date  string
	day   time.Time
	close float64
}

func dailyBars(h *history, et *time.Location) []dayBar {
	var out []dayBar
	for i, c := range h.closes {
		if i >= len(h.times) {
			break
		}
		d := time.Unix(h.times[i], 0).In(et)
		out = append(out, dayBar{d.Format("2006-01-02"), d, c})
	}
	return out
}

func nextWeekday(t time.Time) time.Time {
	t = t.AddDate(0, 0, 1)
	for weekend(t) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// dayName is YESTERDAY or TOMORROW when it is one, and the weekday when it is
// further, so Monday's brief says FRIDAY rather than calling it yesterday.
func dayName(day, now time.Time) string {
	switch day.Format("2006-01-02") {
	case now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "YESTERDAY"
	case now.AddDate(0, 0, 1).Format("2006-01-02"):
		return "TOMORROW"
	}
	return strings.ToUpper(day.Format("Monday"))
}

func vixLevel(st State) float64 {
	for _, c := range st.Market.Cards {
		if c.Key == "vix" && !c.Unavailable {
			v, _ := strconv.ParseFloat(strings.ReplaceAll(c.Price, ",", ""), 64)
			return v
		}
	}
	return 0
}

// nextSlot is when the next brief is due, weekends included.
func nextSlot(now time.Time) time.Time {
	now = now.In(easternTime())
	for day := 0; day < 2; day++ {
		d := now.AddDate(0, 0, day)
		for _, s := range briefSlots {
			at := time.Date(d.Year(), d.Month(), d.Day(), s.hour, s.minute, 0, 0, d.Location())
			if at.After(now) {
				return at
			}
		}
	}
	return now
}

// nextBrief is the footer's NEXT BRIEF segment.
func nextBrief(now time.Time) string {
	at := nextSlot(now)
	d := at.Sub(now).Round(time.Minute)
	in := fmt.Sprintf("%dM", int(d.Minutes()))
	if d >= time.Hour {
		in = fmt.Sprintf("%dH %dM", int(d.Hours()), int(d.Minutes())%60)
	}
	return at.Format("15:04") + " IN " + in
}
