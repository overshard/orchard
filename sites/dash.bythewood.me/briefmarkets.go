package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

var marketFeeds = []briefFeed{
	{"REUTERS", "C", "googlenews", googleNews("reuters.com/business")},
	{"WSJ", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/RSSMarketsMain"},
	{"MW", "C", "dowjones", "https://feeds.content.dowjones.io/public/rss/mw_topstories"},
	{"BBC", "C", "bbc", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	{"CNBC", "L", "cnbc", "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=20910258"},
	{"CNBC", "L", "cnbc", "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114"},
	{"NPR", "L", "npr", "https://feeds.npr.org/1017/rss.xml"},
}

// marketSchema gives every line its own field so a forward line can't come
// back as "none", which the 9B did on a quiet Monday and wrote up Netflix layoffs.
// The lean is "verdict" since keys go out sorted and the model writes them in
// that order, and it has to write its reasons before it picks a side.
func marketSchema(lines []marketLine) map[string]any {
	props := map[string]any{}
	var required []string
	for i, l := range lines {
		leans := []string{"none"}
		if l.forward {
			leans = []string{"higher", "lower", "mixed"}
		}
		key := fmt.Sprintf("line%d", i+1)
		props[key] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verdict": map[string]any{"type": "string", "enum": leans},
				"text":    map[string]any{"type": "string", "maxLength": 300},
				"sources": map[string]any{"type": "array", "maxItems": 3, "items": map[string]any{"type": "integer"}},
			},
			"required":             []string{"verdict", "text", "sources"},
			"additionalProperties": false,
		}
		required = append(required, key)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
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
	events := clusterBy(gather(ctx, b.guard, marketFeeds, at.Add(-reach), now), marketEvents, func(s story) time.Time { return tradingDay(s.at) })
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
		if l.week {
			calendar.WriteString(weekNotes(l.day, releases, vixLevel(st)))
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
		fmt.Fprintf(&want, "line%d: %s.\n", i+1, a)
	}

	system := "You write a one line markets summary for a dashboard, for one reader who invests in index funds.\n" + neutralRules + `
- Each line is one short sentence of at most 25 words. Name companies and events, never just "earnings" or "data".
- Quote figures exactly as given in the numbers. Never compute or invent a figure.
- A reason for a move has to come from a story, and is cited. If no story gives one, say what moved and leave the reason out.
- Each story says how long ago it ran and which session it was news for. Explain a session only with stories marked for it, so a story about today's trading is never the reason for yesterday's close.
- No advice, nothing like "investors should".
- A line asking which way things lean is about the whole market, never single companies. Its reasons come from market-wide drivers: futures, Treasury yields and the Fed, the calendar and patterns listed for that day, oil, the VIX and the recent trend, and geopolitical news. A scheduled Fed decision or big release always gets named. The patterns are mild tilts, so they settle a close call and never outweigh the news. Mention a company only if it is one of the very largest in the S&P 500. It sets "verdict" to higher, lower or mixed, for which way the reasons in its text point on balance, and names each reason and which way it pushes, like "Futures are flat, but yields at multi-decade highs and a rising VIX weigh on stocks."
- Do not write the lean itself into the text, the page shows it beside the line. The text is only the reasons.
- The line for the next session names what the calendar has for that day, or says nothing major is scheduled, and never repeats the line before it.
- The line for the week ahead names the biggest things on that week's calendar, and any weekend news that bears on Monday's open.
- A line about a session that already happened gives the main reason for its move, and leaves out the direction and the percent, since the page shows them beside it.
- A line asking which way things lean never leads with one company's news, and when no story speaks to that day it leans on the futures, yields, the VIX, the trend and the calendar in the numbers.
- Every line that is not asking which way things lean sets "verdict" to none and makes no prediction.`

	user := fmt.Sprintf("It is %s Eastern.\n\n%s\nNumbers right now:\n%s\nCalendar and patterns:\n%s\nStories, most widely covered first:\n%s",
		at.In(easternTime()).Format("15:04 on Monday, January 2 2006"), want.String(),
		marketFacts(st, h, at), calendar.String(), clusterList(events, now))

	// One retry with the problem named, since the 9B now and then copies one
	// line into the next or writes the week ahead about a single company.
	var out map[string]marketAnswer
	ask := user
	for try := 0; try < 2; try++ {
		out = nil
		if err := b.model.Structured(ctx, system, ask, marketSchema(lines), 900, &out); err != nil {
			return Brief{}, err
		}
		bad := marketProblems(lines, out, events, releases)
		if len(bad) == 0 {
			break
		}
		slog.Info("brief markets retried", slog.String("component", "brief"), slog.Int("try", try+1), slog.String("problems", strings.Join(slices.Collect(maps.Values(bad)), "; ")))
		if try == 1 {
			for key := range bad {
				delete(out, key)
			}
			break
		}
		ask = user + "\n\nYour last answer had problems, fix them:\n- " + strings.Join(slices.Collect(maps.Values(bad)), "\n- ")
	}

	brief := newBrief(slot, at, events)
	brief.Basis = spBasis(st.Market)
	if weekend(at) {
		brief.Title = "WEEKEND"
	}
	for i, l := range lines {
		if l.forward {
			if pt, ok := heldLine(st.Briefs.Markets, brief, l.label); ok {
				brief.Points = append(brief.Points, pt)
				continue
			}
		}
		p := out[fmt.Sprintf("line%d", i+1)]
		pt := cite(events, p.Sources)
		pt.Text = tidy(p.Text)
		if pt.Text == "" {
			continue
		}
		pt.Label = l.label
		switch {
		case l.move != "":
			pt.Lean, pt.Move = l.dir, l.move
		case l.forward:
			pt.Lean = p.Lean
		}
		brief.Points = append(brief.Points, pt)
	}
	if len(brief.Points) == 0 {
		return Brief{}, fmt.Errorf("no lines")
	}
	return brief, nil
}

// spBasis is the S&P card's price, which over a weekend is Friday's last
// futures print and doesn't move until they reopen Sunday evening.
func spBasis(m Market) string {
	for _, c := range m.Cards {
		if c.Key == "sp500" && !c.Unavailable {
			return c.Price
		}
	}
	return ""
}

// heldLine is the last brief's line for the same session or week when nothing
// has traded since it was written, since asked again with the same prices the
// 9B often flips its lean.
func heldLine(prev, b Brief, label string) (Point, bool) {
	if b.Basis == "" || prev.Basis != b.Basis || prev.Slot == 0 {
		return Point{}, false
	}
	day, from := leanDay(label, time.Unix(b.Slot, 0))
	if d, f := leanDay(label, time.Unix(prev.Slot, 0)); d != day || f != from {
		return Point{}, false
	}
	for _, p := range prev.Points {
		if p.Label == label && p.Lean != "" {
			return p, true
		}
	}
	return Point{}, false
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

func session(when string) string {
	switch when {
	case "PRE":
		return "before the open"
	case "POST":
		return "after the close"
	}
	return ""
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

	// The session a forward line is about, or the Monday of the week it is
	// about when week is set.
	day  time.Time
	week bool

	// The sessions a closed line covers, after the close before it through
	// its own, so a holiday's news still counts for the next session.
	after, through time.Time
}

const weekAhead = "WEEK AHEAD"

func weekStart(t time.Time) time.Time {
	return t.AddDate(0, 0, -(int(t.Weekday())+6)%7)
}

// lastBefore is the index of the last bar from before the day, or -1.
func lastBefore(bars []dayBar, day time.Time) int {
	date := day.Format("2006-01-02")
	i := slices.IndexFunc(bars, func(b dayBar) bool { return b.date >= date })
	if i < 0 {
		i = len(bars)
	}
	return i - 1
}

// marketLines is always the last session, today and the next, so the strip has
// the same three answers at 7am as at 4pm. On a weekend it's the week just gone
// and the week ahead.
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
		if n := len(past); n > 0 {
			if i := lastBefore(past, weekStart(past[n-1].day)); i >= 0 {
				lines = append(lines, closedLine("LAST WEEK", past[n-1].close, past[i].close,
					"the two or three main reasons given for how stocks did over the past week").between(past[i].day, past[n-1].day))
			}
		}
		return append(lines, marketLine{label: weekAhead, forward: true, week: true, day: next,
			ask: "which way the market as a whole leans over the coming week, Monday to Friday, and the two or three biggest market-wide reasons, including anything that happened over the weekend that bears on it"})
	}

	var lines []marketLine
	if n := len(past); n >= 2 {
		day := past[n-1].day
		lines = append(lines, closedLine(dayName(day, now), past[n-1].close, past[n-2].close,
			"the main reason given for the S&P 500's move on "+day.Format("Monday")).between(past[n-2].day, day))
	}
	if len(current) > 0 && len(past) > 0 && session != "pre" {
		l := closedLine("TODAY", current[0].close, past[len(past)-1].close,
			"the main reason given for how today's session closed")
		if session == "regular" {
			l.ask = "the main reason given for how today's session is going so far" + l.moved("is")
			l.move += " SO FAR"
		}
		lines = append(lines, l.between(past[len(past)-1].day, now))
	} else {
		lines = append(lines, marketLine{label: "TODAY", forward: true, day: now,
			ask: "which way the market as a whole leans today and the two or three biggest market-wide reasons"})
	}
	return append(lines, marketLine{label: dayName(next, now), forward: true, day: next,
		ask: "which way the market as a whole leans for " + next.Format("Monday") + ", the next session, and the two or three biggest market-wide reasons"})
}

type marketAnswer struct {
	Lean    string `json:"verdict"`
	Text    string `json:"text"`
	Sources []int  `json:"sources"`
}

// marketProblems is what's wrong with each line, by key: one that repeats an
// earlier line, a past session explained by another session's story, and a
// week ahead that leaves out the week's scheduled releases.
func marketProblems(lines []marketLine, out map[string]marketAnswer, events []cluster, releases []release) map[string]string {
	bad := map[string]string{}
	for i, l := range lines {
		key := fmt.Sprintf("line%d", i+1)
		text := out[key].Text
		for j := range i {
			if other := out[fmt.Sprintf("line%d", j+1)].Text; text != "" && wordOverlap(text, other) > 0.6 {
				bad[key] = fmt.Sprintf("%s repeats %s, write it about %s instead", key, fmt.Sprintf("line%d", j+1), lines[i].label)
			}
		}
		if _, dup := bad[key]; dup {
			continue
		}
		if !l.forward {
			for _, n := range out[key].Sources {
				if n >= 1 && n <= len(events) && !l.covers(events[n-1].session) {
					bad[key] = fmt.Sprintf("%s cites [%d], which is news for %s's session, so explain %s%s, or if none of them gives a reason say what moved and cite nothing",
						key, n, events[n-1].session.Format("Monday"), l.label, l.only())
					break
				}
			}
			continue
		}
		if !l.week {
			continue
		}
		want := weekReleases(l.day, releases)
		if len(want) > 0 && !slices.ContainsFunc(want, func(w string) bool { return strings.Contains(strings.ToLower(text), strings.ToLower(w)) }) {
			bad[key] = fmt.Sprintf("%s has to name the week's scheduled %s", key, strings.Join(want, " and "))
		}
	}
	return bad
}

// weekReleases is the short names of what's scheduled Monday to Friday.
func weekReleases(monday time.Time, releases []release) []string {
	var out []string
	for d := monday; !weekend(d); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for _, r := range releases {
			if r.at.In(easternTime()).Format("2006-01-02") == date && r.short != "" && !slices.Contains(out, r.short) {
				out = append(out, r.short)
			}
		}
		if _, ok := fomcDecisions[date]; ok && !slices.Contains(out, "Fed") {
			out = append(out, "Fed")
		}
	}
	return out
}

func wordOverlap(a, b string) float64 {
	wa, wb := strings.Fields(strings.ToLower(a)), strings.Fields(strings.ToLower(b))
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	shared := 0
	for _, w := range wa {
		if slices.Contains(wb, w) {
			shared++
		}
	}
	return float64(shared) / float64(max(len(wa), len(wb)))
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
	l := marketLine{label: label, dir: dir, move: fmt.Sprintf("%.2f%%", math.Abs(pct))}
	if ask != "" {
		l.ask = ask + l.moved("was")
	}
	return l
}

// between sets the sessions a closed line covers and tells the model which
// stories it may explain them with.
func (l marketLine) between(after, through time.Time) marketLine {
	l.after, l.through = after, through
	l.ask += l.only()
	return l
}

// covers is whether a story that was news for day can explain this line.
func (l marketLine) covers(day time.Time) bool {
	d := day.Format("2006-01-02")
	return d > l.after.Format("2006-01-02") && d <= l.through.Format("2006-01-02")
}

// only names the sessions a closed line's stories have to be marked for.
func (l marketLine) only() string {
	var names []string
	for d := l.after.AddDate(0, 0, 1); l.covers(d); d = d.AddDate(0, 0, 1) {
		if !weekend(d) {
			names = append(names, d.Format("Monday"))
		}
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) > 1 {
		names = []string{strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]}
	}
	return ", using only stories marked " + names[0] + "'s session"
}

// tradingDay is the session a story is news for. Futures reopen at 6pm New
// York, so the evening's stories are about the next session, and the
// weekend's about Monday's.
func tradingDay(at time.Time) time.Time {
	t := at.In(easternTime())
	if t.Hour() >= 18 {
		t = t.AddDate(0, 0, 1)
	}
	for weekend(t) {
		t = t.AddDate(0, 0, 1)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// moved tells the model which way a session went, or it gives reasons that
// "weighed on" stocks beside an up arrow.
func (l marketLine) moved(verb string) string {
	switch l.dir {
	case "up":
		return ", when the S&P 500 " + verb + " up " + l.move
	case "down":
		return ", when the S&P 500 " + verb + " down " + l.move
	}
	return ", when the S&P 500 " + verb + " about flat"
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
