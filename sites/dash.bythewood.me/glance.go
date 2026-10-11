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

// The glance desk runs last at each slot and reads everything else on the page
// into one short read at the top, so a day where nothing happened can be seen to
// be one without scrolling. Each slot's level is worked out here from fixed
// thresholds and the model only gets to phrase it, since a small model asked
// whether things are serious will find a way to say yes.

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

// glanceSlots reads the state into the five slots. Thresholds are on the side
// of quiet, since a read that says watch every day stops being read.
func glanceSlots(st State, releases []release, now time.Time) []glanceSlot {
	now = now.In(easternTime())
	return []glanceSlot{glanceMarkets(st, releases, now), glanceNews(st), glanceTech(st), glanceWeather(st), glancePlay(st)}
}

func glanceMarkets(st State, releases []release, now time.Time) glanceSlot {
	g := glanceSlot{label: "MARKETS", anchor: "markets", level: levelQuiet}
	g.facts = append(g.facts, "Session: "+st.Market.Session)
	var spx, vix float64
	var movers []string
	for _, c := range st.Market.Cards {
		if c.Unavailable {
			continue
		}
		g.facts = append(g.facts, fmt.Sprintf("%s %s, %s on the day", c.Label, c.Price, c.Percent))
		pct, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(c.Percent, "+"), "%"), 64)
		switch c.Key {
		case "sp500":
			spx = pct
			movers = append(movers, "S&P 500 "+c.Percent)
		case "vix":
			vix, _ = strconv.ParseFloat(strings.ReplaceAll(c.Price, ",", ""), 64)
			movers = append(movers, "VIX "+c.Price)
		case "bitcoin":
			if math.Abs(pct) >= 7 {
				g.level = higher(g.level, levelWatch)
				g.why = append(g.why, "bitcoin moved "+c.Percent)
			}
		}
	}
	switch {
	case math.Abs(spx) >= 2:
		g.level = levelAct
		g.why = append(g.why, fmt.Sprintf("the S&P 500 moved %+.2f%%", spx))
	case math.Abs(spx) >= 1:
		g.level = higher(g.level, levelWatch)
		g.why = append(g.why, fmt.Sprintf("the S&P 500 moved %+.2f%%", spx))
	}
	switch {
	case vix >= 30:
		g.level = levelAct
		g.why = append(g.why, fmt.Sprintf("the VIX is %.1f", vix))
	case vix >= 22:
		g.level = higher(g.level, levelWatch)
		g.why = append(g.why, fmt.Sprintf("the VIX is %.1f", vix))
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
	for _, n := range sessionNotes(day, releases, 0) {
		if strings.Contains(n, "comes out") || strings.Contains(n, "rate decision") {
			g.level = higher(g.level, levelWatch)
			first, _, _ := strings.Cut(n, ".")
			g.why = append(g.why, day.Format("Monday")+": "+first)
			g.facts = append(g.facts, "Scheduled "+day.Format("Monday")+": "+first)
		}
	}
	g.fallback = strings.Join(movers, ", ") + "."
	return g
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
		if f.pulse.Level == "hot" {
			g.level = levelWatch
			g.why = append(g.why, "a thread is blowing up on "+f.name)
		}
		line := f.name + " is " + strings.ToLower(f.pulse.Label)
		if f.pulse.Text != "" {
			line += ", " + f.pulse.Text
		}
		g.facts = append(g.facts, line)
		if f.pulse.Read != "" {
			g.facts = append(g.facts, f.name+" read: "+f.pulse.Read)
		}
		for i, s := range f.stories[:min(len(f.stories), 3)] {
			g.facts = append(g.facts, fmt.Sprintf("%s No. %d: %s (%d points)", f.name, i+1, s.Title, s.Points))
		}
	}
	if len(st.HN) > 0 {
		g.fallback = "Top of Hacker News is " + st.HN[0].Title + "."
	}
	return g
}

func glanceWeather(st State) glanceSlot {
	g := glanceSlot{label: "WEATHER", anchor: "weather", level: levelQuiet}
	for _, a := range st.Alerts {
		lvl := levelWatch
		if a.Severity == "Extreme" || a.Severity == "Severe" && strings.Contains(a.Event, "Warning") {
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
	for _, d := range st.Outlook.Days[:min(len(st.Outlook.Days), 3)] {
		var fs []string
		for _, f := range d.Factors {
			fs = append(fs, strings.ToLower(f.Label+" "+f.Value+" "+f.Detail))
		}
		g.facts = append(g.facts, fmt.Sprintf("%s %s: %s outside, high %s, %s", d.Day, d.Date, strings.ToLower(d.Verdict), d.High, strings.Join(fs, ", ")))
	}
	return g
}

func glancePlay(st State) glanceSlot {
	g := glanceSlot{label: "PLAY", anchor: "steam", level: levelQuiet}
	for i, s := range st.Steam[:min(len(st.Steam), 4)] {
		g.facts = append(g.facts, fmt.Sprintf("Steam top seller %d: %s, %d%% positive, %s playing", i+1, s.Name, s.Rating, s.Players))
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

	// A slot with nothing to go on is left out rather than handed to the model,
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

	system := `You write the read at the top of one person's dashboard, so he can tell at a glance whether anything needs his attention. He lives in North Carolina's Yadkin Valley, invests in index funds, works in software, and plays PC games. Most days nothing needs him, and saying so plainly is the right answer.
- The level of each slot and of the whole read is decided already and given to you. Never make anything sound bigger than its level. On a quiet slot say what's there in a calm, flat way.
- Each slot is one sentence of at most 18 words, about only that slot, using only its facts. No advice.
- "summary" is one or two sentences, at most 35 words, of what matters across all of it, the things that set the level first. If the overall level is quiet, say nothing needs attention and name the one or two things most worth knowing.
- "summary" opens by saying whether anything needs him. Then the one or two things most worth knowing today.
- "words_to_bold" is the one or two names or figures in your summary that matter most, copied exactly as they appear in it, and they are shown in bold. No asterisks anywhere.
- Quote figures exactly as given. Never compute, round, or invent a figure, a name, or a date. No dashes, use commas.`

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

func glanceFallback(level string, why []string) string {
	if level == levelQuiet || len(why) == 0 {
		return "Nothing needs attention right now."
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
