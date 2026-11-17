package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Headlines are written to be clicked, so the news is told to restate what
// happened instead of how an outlet framed it.

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

// hourlySlots is what news and feeds run on, since a Haiku run costs about a
// cent. Each hour reads like the named slot it falls in, and before 7 it's the morning.
var hourlySlots = func() []briefSlot {
	out := make([]briefSlot, 0, 24)
	for h := range 24 {
		s := briefSlots[0]
		for _, named := range briefSlots {
			if named.hour <= h {
				s = named
			}
		}
		s.hour, s.minute = h, 0
		out = append(out, s)
	}
	return out
}()

// futuresSlot is Sunday's markets brief, once futures have opened on the week.
var futuresSlot = briefSlot{kind: "futures", hour: 18, minute: 15, reach: 24 * time.Hour}

// slotsOn is the slots a desk runs on day. Over a weekend markets only runs
// Saturday morning and once futures open Sunday, since nothing trades between
// them, and an empty desk gets every named slot.
func slotsOn(desk string, day time.Time) []briefSlot {
	if desk == "news" || desk == "feeds" {
		return hourlySlots
	}
	switch wd := day.Weekday(); {
	case desk == "markets" && wd == time.Saturday:
		return briefSlots[:1]
	case desk == "markets" && wd == time.Sunday:
		return []briefSlot{futuresSlot}
	case desk == "" && wd == time.Sunday:
		return append(slices.Clone(briefSlots), futuresSlot)
	}
	return briefSlots
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

	// The slot this answered, in unix seconds, which is how a restart knows
	// whether it is owed a run.
	Slot int64 `json:"slot"`

	// The S&P price a markets brief was written against.
	Basis string `json:"basis,omitempty"`

	// How big the news was each of the last thirty days, for TODAY.
	Intensity []DayShare `json:"intensity,omitempty"`

	// How the counted leans have done against always saying higher.
	Record string `json:"record,omitempty"`
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

	// NEW or UPDATED against the brief before it, and what TODAY said about the
	// story on the days before.
	Mark     string `json:"mark,omitempty"`
	Timeline []Step `json:"timeline,omitempty"`

	// A forward market line's calendar, written in Go since the model gets the
	// day wrong, and the move the VIX prices for its session or week.
	Calendar string `json:"calendar,omitempty"`
	Range    string `json:"range,omitempty"`
}

// Read says whether anything big is going on, judged by how many newsrooms are
// on the same story and not by the model, which would call every day busy.
type Read struct {
	Level  string `json:"level"`
	Label  string `json:"label"`
	Spread string `json:"spread"`
	Text   string `json:"text"`
	Links  []Link `json:"links"`

	// The 1 to 5 rating TODAY ranks by, for the most carried story when the read
	// opens on it.
	Impact int `json:"impact,omitempty"`
}

type ThreadRead struct {
	URL  string `json:"url"`
	Read string `json:"read"`
	// A TL;DR of the article the thread is about.
	Gist string `json:"gist,omitempty"`

	// How many comments the read was written from, and when, so a later slot
	// can carry it over while the thread hasn't moved on.
	Comments int    `json:"comments,omitempty"`
	At       string `json:"at,omitempty"`
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
}

func (s briefSlot) title() string {
	switch s.kind {
	case "morning":
		return "MORNING"
	case "preopen":
		return "PRE-OPEN"
	case "midday":
		return "MIDDAY"
	case "futures":
		return "FUTURES"
	}
	return "CLOSE"
}

// latestSlot is the most recent slot at or before now. Weekends run too,
// since a Sunday read of the week is a read of what Monday has coming.
func latestSlot(now time.Time) (briefSlot, time.Time) {
	return deskSlot("named", now)
}

// deskSlot is the most recent slot a desk owes at or before now.
func deskSlot(desk string, now time.Time) (briefSlot, time.Time) {
	now = now.In(easternTime())
	for back := 0; back < 3; back++ {
		day := now.AddDate(0, 0, -back)
		slots := slotsOn(desk, day)
		for i := len(slots) - 1; i >= 0; i-- {
			s := slots[i]
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

// briefModel is the part of the gateway the briefs use, so the desks can be
// tested against scripted answers.
type briefModel interface {
	Structured(ctx context.Context, system, user string, schema map[string]any, maxTok int, out any) error
}

// Briefer owns the schedule and the file the latest briefs are kept in, so a
// deploy at 10am still shows the 9am one instead of nothing until 11.
type Briefer struct {
	store   *Store
	guard   *Guard
	model   briefModel
	path    string
	leans   *leanBook
	archive briefArchive

	// Nothing is tried before this after a run failed.
	next time.Time

	compile func(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error)

	// A run asked for from the page waits here for Run's loop, so it never
	// overlaps a scheduled one.
	force chan struct{}

	mu       sync.Mutex
	queued   bool
	running  bool
	started  time.Time
	finished time.Time
	forced   time.Time
	failed   []string
}

// A run from the page spends the same article and comment budgets as a slot,
// so it can't be asked for again until this long after the last one ended.
const rebuildCooldown = 10 * time.Minute

var (
	errBriefsOff  = errors.New("briefs are off")
	errBriefsBusy = errors.New("briefs are already running")
	errBriefsCool = errors.New("briefs ran a moment ago")
)

// briefDesks is every desk in the order they run.
var briefDesks = []string{"markets", "news", "feeds"}

// deskRun is one desk owed one slot.
type deskRun struct {
	desk string
	slot briefSlot
	at   time.Time
}

func NewBriefer(store *Store, g *Guard, m *Model, dataDir string) *Briefer {
	b := &Briefer{store: store, guard: g, path: filepath.Join(dataDir, "briefs.json"), leans: openLeanBook(dataDir),
		archive: openArchive(dataDir), force: make(chan struct{}, 1)}
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
	if m == nil {
		saved.Markets.Status, saved.News.Status = "off", "off"
	}
	saved.Markets.Record = b.leans.record().String()
	store.update(func(st *State) { st.Briefs = saved })
	if b.leans.add(saved.Markets) {
		b.leans.save()
	}
	for desk, br := range map[string]Brief{"markets": saved.Markets, "news": saved.News, "feeds": saved.Feeds} {
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
		case <-b.force:
			b.rebuild(ctx, time.Now())
		}
	}
}

// Force queues a run of every desk for Run's loop, refusing while one is going
// or inside the cooldown after the last one asked for.
func (b *Briefer) Force(now time.Time) error {
	if b.model == nil {
		return errBriefsOff
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.queued || b.running {
		return errBriefsBusy
	}
	if !b.forced.IsZero() && now.Before(b.forced.Add(rebuildCooldown)) {
		return errBriefsCool
	}
	b.queued = true
	select {
	case b.force <- struct{}{}:
	default:
	}
	return nil
}

// RebuildStatus is what the page's BRIEFS readout shows.
type RebuildStatus struct {
	Running  bool     `json:"running"`
	Elapsed  int      `json:"elapsed,omitempty"`
	Finished string   `json:"finished,omitempty"`
	Failed   []string `json:"failed,omitempty"`
	ReadyIn  int      `json:"ready_in"`
}

func (b *Briefer) Status(now time.Time) RebuildStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := RebuildStatus{Running: b.queued || b.running, Failed: b.failed}
	if b.running {
		st.Elapsed = int(now.Sub(b.started).Seconds())
	}
	if !b.finished.IsZero() {
		st.Finished = b.finished.In(easternTime()).Format("15:04")
	}
	if left := b.forced.Add(rebuildCooldown).Sub(now); !b.forced.IsZero() && left > 0 {
		st.ReadyIn = int(left.Round(time.Second).Seconds())
	}
	return st
}

// begin marks a run as going. A scheduled one gives way to a forced one that
// is already queued.
func (b *Briefer) begin(now time.Time, forced bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.queued && !forced {
		return false
	}
	b.queued, b.running, b.started = false, true, now
	return true
}

func (b *Briefer) end(forced bool, failed []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running, b.finished, b.failed = false, time.Now(), failed
	if forced {
		b.forced = b.finished
	}
}

// rebuild runs every desk because somebody asked.
func (b *Briefer) rebuild(ctx context.Context, now time.Time) {
	b.begin(now, true)
	var runs []deskRun
	for _, desk := range briefDesks {
		runs = append(runs, deskRun{desk, forcedSlot(now), now})
	}
	b.end(true, b.runDesks(ctx, runs, true))
}

// forcedSlot is the slot a run asked for at now is framed as. Before the first
// slot of the day the latest is yesterday's close, and the morning reads right.
func forcedSlot(now time.Time) briefSlot {
	slot, at := latestSlot(now)
	if at.Format("2006-01-02") != now.In(easternTime()).Format("2006-01-02") {
		return briefSlots[0]
	}
	return slot
}

// tick runs every owed desk back to back when a slot comes due.
func (b *Briefer) tick(ctx context.Context, now time.Time) {
	b.store.mu.RLock()
	h := b.store.history
	b.store.mu.RUnlock()
	if b.leans.score(h, now) {
		b.leans.save()
		record := b.leans.record().String()
		b.store.update(func(st *State) { st.Briefs.Markets.Record = record })
	}

	cur := b.store.Snapshot().Briefs
	slots := map[string]int64{"markets": cur.Markets.Slot, "news": cur.News.Slot, "feeds": cur.Feeds.Slot}
	var due []deskRun
	for _, desk := range briefDesks {
		if slot, at := deskSlot(desk, now); slots[desk] < at.Unix() {
			due = append(due, deskRun{desk, slot, at})
		}
	}
	if len(due) == 0 || now.Before(b.next) {
		return
	}
	// A run asked for from the page is next in the loop and covers every desk.
	if !b.begin(now, false) {
		return
	}
	failed := b.runDesks(ctx, due, false)
	if len(failed) > 0 {
		b.next = now.Add(briefRetry)
	}
	b.end(false, failed)
}

// runDesks writes each desk in turn and returns the ones that failed. They
// run one after another, which keeps the calls well under the gateway's
// per key rate.
func (b *Briefer) runDesks(ctx context.Context, runs []deskRun, forced bool) []string {
	var failed []string
	for _, run := range runs {
		desk, slot := run.desk, run.slot
		started := time.Now()
		brief, err := b.compile(ctx, desk, slot, run.at)
		if err != nil {
			failed = append(failed, desk)
			slog.Warn("brief failed", slog.String("component", "brief"), slog.String("desk", desk),
				slog.String("slot", slot.kind), slog.Bool("forced", forced), slog.Any("err", err))
			continue
		}
		slog.Info("brief written", slog.String("component", "brief"), slog.String("desk", desk),
			slog.String("slot", slot.kind), slog.Bool("forced", forced), slog.Int("stories", brief.Stories),
			slog.Duration("took", time.Since(started).Round(time.Second)))
		logPoints(desk, slot.kind, brief)
		b.archive.add(desk, brief)
		if desk == "markets" {
			if b.leans.add(brief) {
				b.leans.save()
			}
			brief.Record = b.leans.record().String()
		}

		b.store.update(func(st *State) { st.setBrief(desk, brief) })
	}
	b.save()
	return failed
}

func (st *State) setBrief(desk string, brief Brief) {
	switch desk {
	case "markets":
		st.Briefs.Markets = brief
	case "feeds":
		st.Briefs.Feeds = brief
		st.attachFeedReads()
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
	}
	return b.compileNews(ctx, slot, at)
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
- Name people by the office the stories give them. Add a party like (R-Texas) only when a story gives it, never from memory. No labels like far-right or far-left unless quoting someone by name.
- Attribute every claim, accusation or prediction to whoever made it. Never state one side's description of events as fact.
- On a contested political story, give each side's stated position in one clause each, of about equal length.
- If the stories disagree on a fact, say reports differ.
- Never pad. Do not write what a story did not say, and add a second sentence only when it carries another fact from the stories.
- Use only facts in the stories given. No outside knowledge, no guessing at motives, no opinion, no advice, no "what this means" unless a named source said it.
- Include a story whichever party or side it reflects well or badly on.
- Do not put story numbers or outlet names in the text. The page shows the sources beside each point.`

// sentence is a model's line tidied and ending on a full stop.
func sentence(raw string) string {
	s := tidy(raw)
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s[len(s)-1:], ".!?\"'\u201d") {
		s += "."
	}
	return s
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
	next := now
	for day := 0; day < 2; day++ {
		d := now.AddDate(0, 0, day)
		for _, s := range append(slotsOn("", d), hourlySlots...) {
			at := time.Date(d.Year(), d.Month(), d.Day(), s.hour, s.minute, 0, 0, d.Location())
			if at.After(now) && (next.Equal(now) || at.Before(next)) {
				next = at
			}
		}
		if !next.Equal(now) {
			return next
		}
	}
	return next
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
