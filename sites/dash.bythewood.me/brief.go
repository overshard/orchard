package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Four times a day the local model writes a brief for each desk. Headlines are
// written to be clicked, so the news is told to restate what happened instead
// of how an outlet framed it.

// briefSlot is one run. The close is a few minutes after the bell so the 4pm
// print has landed on Yahoo before the strip is read.
type briefSlot struct {
	kind         string
	hour, minute int

	// How far back a story may be and still count, from the slot.
	reach time.Duration
}

var briefSlots = []briefSlot{
	{kind: "morning", hour: 7, reach: 24 * time.Hour},
	// The 8:30 releases like CPI and PPI are out by now, and it lands before
	// the 9:30 open.
	{kind: "preopen", hour: 9, reach: 24 * time.Hour},
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

	// The paragraph over TODAY's headlines. Markets never has one.
	Read *Read `json:"read,omitempty"`

	// What the commenters make of each thread on the feeds, by its discussion
	// URL, for the card a row opens.
	Threads []ThreadRead `json:"threads,omitempty"`

	// "off" when there is no model key, so the panel can say why it is empty.
	Status string `json:"status,omitempty"`

	// Why a slot that is due has not run yet, like the card being in use.
	Waiting string `json:"waiting,omitempty"`

	// The slot this answered, in unix seconds, which is how a restart knows
	// whether it is owed a run.
	Slot int64 `json:"slot"`

	// The S&P price a markets brief was written against.
	Basis string `json:"basis,omitempty"`
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

	// A TODAY point's TL;DR, from every outlet it links to.
	Gist string `json:"gist,omitempty"`

	// A glance line's level, and the panel it points at.
	Level  string `json:"level,omitempty"`
	Anchor string `json:"anchor,omitempty"`
}

// Read says whether anything big is going on, judged by how many newsrooms are
// on the same story and not by the model, which would call every day busy.
type Read struct {
	Level  string `json:"level"`
	Label  string `json:"label"`
	Spread string `json:"spread"`
	Text   string `json:"text"`
	Links  []Link `json:"links"`
}

type ThreadRead struct {
	URL  string `json:"url"`
	Read string `json:"read"`
	// A TL;DR of the article the thread is about.
	Gist string `json:"gist,omitempty"`
}

type Link struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

type Briefs struct {
	Markets Brief `json:"markets"`
	News    Brief `json:"news"`
	Feeds   Brief `json:"feeds"`
	Glance  Brief `json:"glance"`
}

func (s briefSlot) title() string {
	switch s.kind {
	case "morning":
		return "MORNING"
	case "preopen":
		return "PRE-OPEN"
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

// Briefer owns the schedule and the file the latest briefs are kept in, so a
// deploy at 10am still shows the 9am one instead of nothing until 11.
type Briefer struct {
	store   *Store
	guard   *Guard
	model   briefModel
	path    string
	leans   *leanBook
	archive briefArchive

	// Nothing is tried before this, whether the card was busy or a run failed.
	next time.Time

	compile func(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error)
}

func NewBriefer(store *Store, g *Guard, m *Model, dataDir string) *Briefer {
	b := &Briefer{store: store, guard: g, path: filepath.Join(dataDir, "briefs.json"), leans: openLeanBook(dataDir),
		archive: openArchive(dataDir)}
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
	saved.Markets.Waiting, saved.News.Waiting, saved.Feeds.Waiting, saved.Glance.Waiting = "", "", "", ""
	if m == nil {
		saved.Markets.Status, saved.News.Status, saved.Glance.Status = "off", "off", "off"
	}
	store.update(func(st *State) { st.Briefs = saved })
	if b.leans.add(saved.Markets) {
		b.leans.save()
	}
	for desk, br := range map[string]Brief{"markets": saved.Markets, "news": saved.News, "feeds": saved.Feeds, "glance": saved.Glance} {
		if len(br.Points) > 0 && br.Slot > 0 && !b.archive.has(desk, br.Slot) {
			b.archive.add(desk, br)
		}
	}
	return b
}

func (b *Briefer) Run(ctx context.Context) {
	if b.model == nil {
		slog.Warn("no LLM_KEY, briefs are off", slog.String("component", "brief"))
		return
	}
	b.store.awaitFirstRound(ctx, 2*time.Minute)
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

// tick runs every desk back to back on one model load when a slot is owed,
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
	if cur.Feeds.Slot < at.Unix() {
		due = append(due, "feeds")
	}
	// Last, since it reads what the other desks just wrote.
	if cur.Glance.Slot < at.Unix() {
		due = append(due, "glance")
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
		b.archive.add(desk, brief)
		if desk == "markets" && b.leans.add(brief) {
			b.leans.save()
		}

		b.store.update(func(st *State) { st.setBrief(desk, brief) })
	}
	if failed {
		b.next = now.Add(briefRetry)
	}
	b.waiting("")
	b.save()
}

func (st *State) setBrief(desk string, brief Brief) {
	switch desk {
	case "markets":
		st.Briefs.Markets = brief
	case "feeds":
		st.Briefs.Feeds = brief
		st.attachFeedReads()
	case "glance":
		st.Briefs.Glance = brief
	default:
		st.Briefs.News = brief
		st.setNotices("news", briefNotices(brief))
	}
}

func (b *Briefer) compileDesk(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error) {
	switch desk {
	case "markets":
		return b.compileMarkets(ctx, slot, at)
	case "feeds":
		return b.compileFeeds(ctx, slot, at)
	case "glance":
		return b.compileGlance(ctx, slot, at)
	}
	return b.compileNews(ctx, slot, at)
}

// waiting says on every brief panel why the last brief is still up.
func (b *Briefer) waiting(why string) {
	b.store.update(func(st *State) {
		st.Briefs.Markets.Waiting, st.Briefs.News.Waiting, st.Briefs.Feeds.Waiting, st.Briefs.Glance.Waiting = why, why, why, why
	})
}

func (b *Briefer) save() {
	raw, err := json.Marshal(b.store.Snapshot().Briefs)
	if err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o644); err == nil {
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

// The rules the prompts share. They're about register and not about any side,
// since the reader asked for what happened and not for a take on it.
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

// whole keeps what a maxLength cut left whole, the first finished sentence,
// else up to the last comma past halfway, else nothing. The cut lands just
// short of the limit, so a long line is judged by how it ends.
func whole(raw string, limit int) string {
	s := tidy(raw)
	// A cut that lands right after a comma is a whole clause already.
	if t := strings.TrimSuffix(s, ","); t != s {
		s = strings.TrimSuffix(strings.TrimSuffix(t, " and"), " while") + "."
	}
	if s == "" || s == "." {
		return ""
	}
	if len([]rune(s)) >= limit*85/100 && !endsSentence(s) {
		i := strings.Index(s, ". ")
		for i > 0 && !endsSentence(s[:i+1]) {
			j := strings.Index(s[i+2:], ". ")
			if j < 0 {
				i = -1
				break
			}
			i += 2 + j
		}
		switch c := strings.LastIndex(s, ", "); {
		case i > 0:
			s = s[:i+1]
		case c > len(s)/2:
			s = strings.TrimSuffix(strings.TrimSuffix(s[:c], " and"), " while")
		default:
			return ""
		}
	}
	if !strings.ContainsAny(s[len(s)-1:], ".!?\"'\u201d") {
		s += "."
	}
	return s
}

// endsSentence is a full stop after an ordinary word, since one after U.S. or
// a number doesn't end anything and half a word has none.
func endsSentence(s string) bool {
	if !strings.HasSuffix(s, ".") {
		return false
	}
	last := strings.TrimSuffix(s[strings.LastIndex(s, " ")+1:], ".")
	if len(last) < 3 {
		return false
	}
	for _, r := range last {
		if !unicode.IsLower(r) {
			return false
		}
	}
	return true
}

var citeMarks = regexp.MustCompile(`\s*[\[(]\d+(?:\s*,\s*\d+)*[\])]`)

// tidy strips the bracketed numbers a model writes into the text even when
// told not to, and the dashes the page's type does not use.
func tidy(s string) string {
	s = citeMarks.ReplaceAllString(s, "")
	s = strings.NewReplacer(" — ", ", ", "—", ", ", " – ", ", ").Replace(s)
	return strings.TrimSpace(s)
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
