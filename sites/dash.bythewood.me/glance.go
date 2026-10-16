package main

import (
	"context"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The glance desk runs last at each slot and reads the rest of the page into
// one short read at the top. Go sets the levels and the model only phrases
// them, since asked whether things are serious it says yes.

const (
	levelQuiet = "quiet"
	levelWatch = "watch"
	levelAct   = "act"
)

var levelRank = map[string]int{levelQuiet: 0, levelWatch: 1, levelAct: 2}

func higher(a, b string) string {
	if levelRank[b] > levelRank[a] {
		return b
	}
	return a
}

// glanceSlot is one line of the read before the model writes it: what it's
// about, how much it matters, the facts the model may use, and a line Go can
// show by itself if the model's comes back wrong.
type glanceSlot struct {
	label, anchor string
	level         string
	why           []string
	facts         []string
	fallback      string
}

func (g glanceSlot) text() string {
	return strings.Join(g.facts, "\n")
}

// glanceSlots reads the state into its slots. Thresholds are on the side of
// quiet, since a read that says watch every day stops being read.
func glanceSlots(st State, releases []release, now time.Time) []glanceSlot {
	now = now.In(easternTime())
	return []glanceSlot{glanceMarkets(st, releases, now), glanceNews(st), glanceTech(st), glanceWeather(st), glancePlay(st, now)}
}

func glanceMarkets(st State, releases []release, now time.Time) glanceSlot {
	g := glanceSlot{label: "MARKETS", anchor: "markets", level: levelQuiet}
	g.facts = append(g.facts, "Session: "+st.Market.Session)
	var movers []string
	for _, c := range st.Market.Cards {
		if c.Unavailable {
			continue
		}
		g.facts = append(g.facts, fmt.Sprintf("%s %s, %s on the day", c.Label, c.Price, c.Percent))
		switch c.Key {
		case "sp500":
			movers = append(movers, "S&P 500 "+c.Percent)
		case "nasdaq":
			movers = append(movers, "Nasdaq "+c.Percent)
		case "vix":
			movers = append(movers, "VIX "+c.Price)
		}
	}
	for _, l := range marketLevels(st.Market.Cards) {
		g.level = higher(g.level, l.level)
		g.why = append(g.why, l.why)
	}
	if st.Signal.Level == "stress" {
		g.level = higher(g.level, levelWatch)
		g.why = append(g.why, strings.ToLower(st.Signal.Headline))
	}
	if st.Signal.Level == "stress" {
		g.facts = append(g.facts, "Long run conditions: "+strings.ToLower(st.Signal.Headline))
	}
	for _, p := range st.Briefs.Markets.Points {
		lean := p.Lean
		if p.Move != "" {
			lean += " " + p.Move
		} else if lean != "" {
			lean = "leans " + lean
		}
		g.facts = append(g.facts, fmt.Sprintf("%s (%s): %s", p.Label, lean, p.Text))
	}
	// A Fed decision or a big release on the next session is worth knowing
	// about the day before, whatever the tape is doing.
	day := now
	if weekend(now) || now.Hour() >= 16 {
		day = nextWeekday(now)
	}
	for _, n := range sessionNotes(day, releases, 0, now) {
		if strings.Contains(n, "comes out") || strings.Contains(n, "came out") || strings.Contains(n, "rate decision") {
			g.level = higher(g.level, levelWatch)
			first, _, _ := strings.Cut(n, ".")
			g.why = append(g.why, day.Format("Monday")+": "+first)
			g.facts = append(g.facts, "Scheduled "+day.Format("Monday")+": "+first)
		}
	}
	g.fallback = strings.Join(movers, ", ") + "."
	return g
}

// marketBar is how far a card has to go for the glance to call it watch and
// then act, in percent on the day, or for the VIX in its level. A bar with no
// act only ever gets to watch.
type marketBar struct {
	name       string
	watch, act float64
	level      bool
}

var marketBars = map[string]marketBar{
	"sp500": {name: "the S&P 500", watch: 1, act: 2},
	// The Nasdaq runs about a third hotter than the S&P, so a tech selloff can
	// be a big day while the S&P is only down a percent.
	"nasdaq":  {name: "the Nasdaq", watch: 1.5, act: 2.5},
	"vix":     {name: "the VIX", watch: 22, act: 30, level: true},
	"bitcoin": {name: "bitcoin", watch: 7},
}

// cardLevel is a card past its watch bar, and value is the figure the bar read.
type cardLevel struct {
	card  Card
	level string
	value float64
	why   string
}

// marketLevels reads the cards against marketBars. The glance asks at its slot
// and the market notices on every poll, so a push and the chip agree on what
// counts.
func marketLevels(cards []Card) []cardLevel {
	var out []cardLevel
	for _, c := range cards {
		bar, ok := marketBars[c.Key]
		if !ok || c.Unavailable {
			continue
		}
		v := cardFigure(c.Percent)
		if bar.level {
			v = cardFigure(c.Price)
		}
		var level string
		switch {
		case bar.act > 0 && math.Abs(v) >= bar.act:
			level = levelAct
		case math.Abs(v) >= bar.watch:
			level = levelWatch
		default:
			continue
		}
		why := fmt.Sprintf("%s moved %+.2f%%", bar.name, v)
		if bar.level {
			why = fmt.Sprintf("%s is %.1f", bar.name, v)
		}
		out = append(out, cardLevel{card: c, level: level, value: v, why: why})
	}
	return out
}

// cardFigure reads a card's "+1.23%" or "7,859.75" back into a number.
func cardFigure(s string) float64 {
	v, _ := strconv.ParseFloat(strings.NewReplacer("+", "", "%", "", ",", "").Replace(s), 64)
	return v
}

func glanceNews(st State) glanceSlot {
	g := glanceSlot{label: "NEWS", anchor: "today", level: levelQuiet}
	b := st.Briefs.News
	// A story every outlet runs is worth naming and not worth a watch, since
	// how widely a thing is carried says nothing about whether it touches him.
	if r := b.Read; r != nil {
		g.facts = append(g.facts, fmt.Sprintf("Most carried, %s: %s", strings.ToLower(r.Spread), r.Text))
	}
	for i, p := range b.Points[:min(len(b.Points), 6)] {
		g.facts = append(g.facts, fmt.Sprintf("Headline %d by impact: %s", i+1, p.Text))
	}
	if len(b.Points) > 0 {
		g.fallback = b.Points[0].Text
	}
	return g
}

func glanceTech(st State) glanceSlot {
	g := glanceSlot{label: "TECH", anchor: "hn", level: levelQuiet}
	for _, f := range []struct {
		name    string
		pulse   Pulse
		stories []Story
	}{{"Hacker News", st.HNPulse, st.HN}, {"Lobsters", st.LobstersPulse, st.Lobsters}} {
		// Points and pace are left out of the facts, since handed them the
		// model writes how fast a thread grew instead of what it's about.
		line := f.name + " is " + strings.ToLower(f.pulse.Label)
		if f.pulse.Level == "hot" {
			g.level = levelWatch
			g.why = append(g.why, "a thread is blowing up on "+f.name)
			if f.pulse.Thread != "" {
				line += ", the thread is " + f.pulse.Thread
				if g.fallback == "" {
					g.fallback = f.pulse.Thread + " is blowing up on " + f.name + "."
				}
			}
		}
		g.facts = append(g.facts, line)
		if f.pulse.Read != "" {
			g.facts = append(g.facts, f.name+" read: "+f.pulse.Read)
		}
		for i, s := range f.stories[:min(len(f.stories), 3)] {
			g.facts = append(g.facts, fmt.Sprintf("%s No. %d: %s", f.name, i+1, s.Title))
		}
	}
	if g.fallback == "" && len(st.HN) > 0 {
		g.fallback = "Top of Hacker News is " + st.HN[0].Title + "."
	}
	return g
}

// Below an NWS advisory, but a day of this is one to plan around.
const (
	heavyRain       = 1.0
	steadyRain      = 0.5
	steadyRainHours = 10
	strongGusts     = 30
)

func glanceWeather(st State) glanceSlot {
	g := glanceSlot{label: "WEATHER", anchor: "weather", level: levelQuiet}
	for _, a := range st.Alerts {
		lvl := levelWatch
		if a.Severity == "extreme" || a.Severity == "severe" && strings.HasSuffix(a.Event, " WARNING") {
			lvl = levelAct
		}
		g.level = higher(g.level, lvl)
		g.why = append(g.why, "a "+a.Event+" is out")
		g.facts = append(g.facts, fmt.Sprintf("NWS alert: %s (%s) until %s. %s", a.Event, a.Severity, a.Until, a.Headline))
	}
	w := st.Weather
	if !w.Unavailable && w.Condition != "" {
		g.facts = append(g.facts, fmt.Sprintf("Now %s°F and %s, high %s, low %s, rain chance %s, wind %s", w.Temperature, strings.ToLower(w.Condition), w.High, w.Low, w.Rain, w.Wind))
		g.fallback = fmt.Sprintf("%s°F and %s, high %s.", w.Temperature, strings.ToLower(w.Condition), w.High)
	}
	for i, d := range w.Days[:min(len(w.Days), 2)] {
		when := []string{"today", "tomorrow"}[i]
		if d.Rain >= heavyRain || d.Rain >= steadyRain && d.RainHours >= steadyRainHours {
			g.level = higher(g.level, levelWatch)
			g.why = append(g.why, fmt.Sprintf("%.1f in of rain %s", d.Rain, when))
		}
		if d.Gusts >= strongGusts {
			g.level = higher(g.level, levelWatch)
			g.why = append(g.why, fmt.Sprintf("gusts to %.0f mph %s", d.Gusts, when))
		}
		g.facts = append(g.facts, fmt.Sprintf("Rain %s: %.1f in over %.0f hours, gusts to %.0f mph", when, d.Rain, d.RainHours, d.Gusts))
	}
	for _, d := range st.Outlook.Days[:min(len(st.Outlook.Days), 3)] {
		var fs []string
		for _, f := range d.Factors {
			fs = append(fs, strings.ToLower(f.Label+" "+f.Value+" "+f.Detail))
		}
		g.facts = append(g.facts, fmt.Sprintf("%s %s: %s outside, high %s, %s", d.Day, d.Date, strings.ToLower(d.Verdict), d.High, strings.Join(fs, ", ")))
	}
	return g
}

// newRelease is how recently out a game has to be to count as new at the top.
const newRelease = 30 * 24 * time.Hour

func glancePlay(st State, now time.Time) glanceSlot {
	g := glanceSlot{label: "PLAY", anchor: "steam", level: levelQuiet}
	for i, s := range st.Steam[:min(len(st.Steam), 4)] {
		line := fmt.Sprintf("Steam top seller %d: %s, %d%% positive, %s playing", i+1, s.Name, s.Rating, s.Players)
		if out, err := time.ParseInLocation("2006-01-02", s.Released, easternTime()); err == nil && i < 3 && now.Sub(out) < newRelease {
			line += ", released " + out.Format("January 2")
			g.level = levelWatch
			g.why = append(g.why, s.Name+" is new and in the top three")
		}
		g.facts = append(g.facts, line)
	}
	if len(st.Steam) > 0 && st.SteamUsual != "" && st.Steam[0].Name != st.SteamUsual {
		g.level = levelWatch
		g.why = append(g.why, st.Steam[0].Name+" took No. 1 from "+st.SteamUsual)
		g.facts = append(g.facts, st.SteamUsual+" usually tops Steam and has been pushed off it")
	}
	for i, t := range st.Streaming[:min(len(st.Streaming), 4)] {
		g.facts = append(g.facts, fmt.Sprintf("Streaming popular %d: %s on %s, scored %d", i+1, t.Name, t.Provider, t.Score))
	}
	if o := st.OnAir; o.Live {
		g.facts = append(g.facts, fmt.Sprintf("%s is live now on %s playing %s", o.Name, o.Platform, o.Game))
	}
	if len(st.Steam) > 0 {
		g.fallback = st.Steam[0].Name + " tops Steam."
	}
	return g
}

// systemsDown names the sites the health strip has as down, which go in the
// read whatever else is happening, since it's the one thing here he'd fix.
func systemsDown(st State) []string {
	var out []string
	for _, r := range st.Systems.Rows {
		if r.State == "down" {
			out = append(out, r.Label)
		}
	}
	return out
}

var glanceLabels = map[string]string{levelQuiet: "QUIET", levelWatch: "WATCH", levelAct: "ACT"}

const (
	glanceSummaryMax = 240
	glanceLineMax    = 160
)

func (b *Briefer) compileGlance(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	st := b.store.Snapshot()
	slots := glanceSlots(st, b.store.releasesOrFetch(ctx, b.guard), at)
	down := systemsDown(st)

	level := levelQuiet
	var why []string
	for _, s := range slots {
		level = higher(level, s.level)
		why = append(why, s.why...)
	}
	if len(down) > 0 {
		level = higher(level, levelWatch)
		why = append(why, "down on the health strip: "+strings.Join(down, ", "))
	}

	// A slot with nothing to go on is left out instead of handed to the model,
	// which writes "No weather." for it.
	slots = slices.DeleteFunc(slots, func(s glanceSlot) bool { return len(s.facts) == 0 })

	var facts strings.Builder
	for i, s := range slots {
		fmt.Fprintf(&facts, "slot%d is %s, level %s", i+1, s.label, s.level)
		if len(s.why) > 0 {
			fmt.Fprintf(&facts, " because %s", strings.Join(s.why, "; "))
		}
		fmt.Fprintf(&facts, ":\n%s\n\n", s.text())
	}
	if len(down) > 0 {
		fmt.Fprintf(&facts, "His own sites that are down: %s\n", strings.Join(down, ", "))
	}

	system := `You write the read at the top of a dashboard, the first thing its owner sees. He lives in North Carolina's Yadkin Valley, invests in index funds, works in software, and plays PC games.
- The level of each slot and of the whole read is decided already and given to you, and a QUIET, WATCH or ACT chip beside your summary shows it. Never make anything sound bigger than its level. On a quiet slot say what's there in a calm, flat way.
- Each slot is one sentence of at most 18 words, about only that slot, using only its facts. No advice.
- The TECH slot says what the biggest thread is about and what its commenters make of it. Never its points, rank, or how fast it grew.
- "summary" is one or two sentences, at most 35 words. Open with the thing that set the level, then the next thing most worth knowing. On a quiet read name the one or two things most worth knowing today.
- The chip already says whether anything needs attention, so the summary never does. Never mention the reader, never write "nothing needs" or "needs attention".
- "words_to_bold" is the one or two names or figures in your summary that matter most, copied exactly as they appear in it, and they are shown in bold. No asterisks anywhere.
- Quote figures exactly as given. Never compute, round, or invent a figure, a name, or a date. No dashes or semicolons, use commas.`

	user := fmt.Sprintf("It is %s Eastern. The overall level is %s.\n\n%s", at.In(easternTime()).Format("15:04 on Monday, January 2"), level, facts.String())

	props := map[string]any{"summary": map[string]any{"type": "string", "maxLength": glanceSummaryMax}}
	props["words_to_bold"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]any{"type": "string", "maxLength": 40}}
	required := []string{"summary", "words_to_bold"}
	for i := range slots {
		key := fmt.Sprintf("slot%d", i+1)
		props[key] = map[string]any{"type": "string", "maxLength": glanceLineMax}
		required = append(required, key)
	}
	schema := map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}

	// Keys go out sorted and the model writes them in that order, so the bold
	// comes after the summary it picks from.
	var raw map[string]any
	if err := b.model.Structured(ctx, system, user, schema, 700, &raw); err != nil {
		return Brief{}, err
	}
	out := map[string]string{}
	var bold []string
	for k, v := range raw {
		switch v := v.(type) {
		case string:
			out[k] = strings.ReplaceAll(v, "**", "")
		case []any:
			for _, w := range v {
				if w, ok := w.(string); ok {
					bold = append(bold, w)
				}
			}
		}
	}

	brief := Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(), Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
	all := facts.String()
	for i, s := range slots {
		text := whole(out[fmt.Sprintf("slot%d", i+1)], glanceLineMax)
		if bad := unsupported(text, s.text()); text == "" || bad != "" {
			slog.Info("glance line replaced", slog.String("component", "brief"), slog.String("slot", s.label),
				slog.String("raw", out[fmt.Sprintf("slot%d", i+1)]), slog.String("unsupported", bad))
			text = s.fallback
		}
		if text == "" {
			continue
		}
		brief.Points = append(brief.Points, Point{Label: s.label, Text: text, Level: s.level, Anchor: s.anchor})
	}
	summary := whole(out["summary"], glanceSummaryMax)
	if talksToHim.MatchString(summary) {
		slog.Info("glance summary replaced", slog.String("component", "brief"), slog.String("raw", out["summary"]), slog.String("verdict", talksToHim.FindString(summary)))
		summary = ""
	}
	if bad := unsupported(summary, all); bad != "" {
		slog.Info("glance summary replaced", slog.String("component", "brief"), slog.String("raw", out["summary"]), slog.String("unsupported", bad))
		summary = ""
	}
	if summary == "" {
		summary = glanceFallback(level, why)
	}
	brief.Read = &Read{Level: level, Label: glanceLabels[level], Text: embolden(summary, bold, all)}
	return brief, nil
}

// talksToHim catches a summary saying whether anything needs him, which the chip
// beside it already does, and which the 9B writes on a WATCH day as readily as
// a quiet one.
var talksToHim = regexp.MustCompile(`(?i)\bneeds? (him|you|his|your|attention)\b|\bnothing needs\b|\b(his|your) attention\b`)

func glanceFallback(level string, why []string) string {
	if level == levelQuiet || len(why) == 0 {
		return "Nothing out of the ordinary today."
	}
	return "Worth a look, " + strings.Join(why, ", ") + "."
}

var figures = regexp.MustCompile(`\d[\d,.]*\d|\d`)

// unsupported is the first figure in a line that its facts don't carry, since a
// number the model made up reads exactly like one it copied.
func unsupported(line, facts string) string {
	for _, f := range figures.FindAllString(line, -1) {
		if !strings.Contains(facts, f) {
			return f
		}
	}
	return ""
}

// embolden marks the first time each phrase appears in the summary, when the
// facts carry it too, so bold only ever lands on something real. Two at most.
func embolden(summary string, phrases []string, facts string) string {
	lower := strings.ToLower(facts)
	n := 0
	for _, p := range phrases {
		p = strings.TrimSpace(strings.Trim(p, "*"))
		i := strings.Index(summary, p)
		if n == 2 || len(p) < 3 || i < 0 || !strings.Contains(lower, strings.ToLower(p)) {
			continue
		}
		summary = summary[:i] + "**" + p + "**" + summary[i+len(p):]
		n++
	}
	return summary
}

// marked escapes a read and turns its **double asterisks** into bold, the one
// piece of markup a model's text is allowed on this page.
func marked(s string) template.HTML {
	parts := strings.Split(s, "**")
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 1 && i < len(parts)-1 {
			b.WriteString("<b>" + html.EscapeString(p) + "</b>")
			continue
		}
		if i%2 == 1 {
			b.WriteString("**")
		}
		b.WriteString(html.EscapeString(p))
	}
	return template.HTML(b.String())
}
