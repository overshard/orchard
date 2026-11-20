package main

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// Clicks, scrolls and first visits can still be pinned as cards from the
// custom event menu, they're just not on the strip by default.
var pinnableBuiltIns = []string{"click", "scroll", "session_start"}

var builtInCardNames = map[string]string{"click": "clicks", "scroll": "scrolls", "session_start": "new visitors"}

func card(name, value string, now, before float64, series []float64, help string) EventCard {
	return EventCard{
		Name:          name,
		Value:         value,
		PercentChange: pctChange(now, before),
		NoPrev:        before == 0 && now != 0,
		Series:        series,
		HelpText:      help,
	}
}

func rate(n, of int64) string {
	if of == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
}

// fillTiles reads both periods and builds the strip, the pinned cards, the
// goals panel and the chart's points.
func (s *site) fillTiles(ctx context.Context, db querier, d *Dashboard, p *Property, cur, prev scope, curB, prevB *buckets) {
	c := readPeriod(ctx, db, cur, curB)
	b := readPeriod(ctx, db, prev, prevB)

	var sum int64
	for i, label := range curB.labels {
		pt := GraphPoint{Label: label, Count: int64(c.visitors[i]), Prev: int64(b.visitors[i]), PrevLabel: prevB.labels[i]}
		if pt.Count > d.GraphPeak.Count || i == 0 {
			d.GraphPeak = pt
		}
		sum += pt.Count
		d.Graph = append(d.Graph, pt)
	}
	var prevSum int64
	for _, v := range b.visitors {
		prevSum += int64(v)
	}
	if n := int64(len(d.Graph)); n > 0 {
		d.GraphAvg = int64(math.Round(float64(sum) / float64(n)))
		d.PrevAvg = int64(math.Round(float64(prevSum) / float64(n)))
	}

	bounce := card("bounce", fmt.Sprintf("%.0f%%", c.Bounce()), c.Bounce(), b.Bounce(), divide(c.bounces, c.visits, 100),
		"Visits that saw one page and left.")
	bounce.Inverse = true
	d.Tiles = []EventCard{
		card("visitors", formatNum(c.Visitors), float64(c.Visitors), float64(b.Visitors), c.visitors,
			"People who viewed a page, counted once each however often they came back."),
		card("page views", formatNum(c.PageViews), float64(c.PageViews), float64(b.PageViews), c.pageViews,
			"Every page viewed in the window."),
		card("views per visit", fmt.Sprintf("%.1f", c.ViewsPerVisit()), c.ViewsPerVisit(), b.ViewsPerVisit(), divide(c.pageViews, c.visits, 1),
			"Page views over visits. A visit ends after half an hour with nothing."),
		bounce,
		card("time on page", fmt.Sprintf("%.0fs", c.TimeOnPage()), c.TimeOnPage(), b.TimeOnPage(), divide(c.timeMS, c.timeN, 0.001),
			"Average time on a page. Under a second or over half an hour is left out as a double fire or an idle tab."),
		card("engagement", fmt.Sprintf("%.0f%%", c.Engagement()), c.Engagement(), b.Engagement(), divide(c.engaged, c.visitors, 100),
			"Visitors with ten or more events, page views, clicks and scrolls together."),
	}
	d.Tiles[0].Event = "visitors"
	d.Tiles[1].Event = "page_view"

	conv, convSeries := readConversions(ctx, db, cur, curB, d.GoalNames)
	convPrev, _ := readConversions(ctx, db, prev, prevB, d.GoalNames)
	for _, g := range d.GoalNames {
		d.Goals = append(d.Goals, LabelCount{Label: g, Count: conv[g]})
	}
	sort.SliceStable(d.Goals, func(i, j int) bool { return d.Goals[i].Count > d.Goals[j].Count })
	if len(d.GoalNames) > 0 {
		g := d.GoalNames[0]
		t := card("goal · "+g, formatNum(conv[g]), float64(conv[g]), float64(convPrev[g]), convSeries[g],
			fmt.Sprintf("Visits that sent %s, %s of every visit.", g, rate(conv[g], c.Visits)))
		t.Note = rate(conv[g], c.Visits) + " of visits"
		t.Event = g
		d.Tiles = append(d.Tiles, t)
	} else {
		n, series := readEventCounts(ctx, db, cur, curB, []string{"session_start"})
		nPrev, _ := readEventCounts(ctx, db, prev, prevB, []string{"session_start"})
		t := card("new visitors", formatNum(n["session_start"]), float64(n["session_start"]), float64(nPrev["session_start"]), series["session_start"],
			"Visitors seen for the first time. Mark a custom event as a goal to put it here instead.")
		t.Event = "session_start"
		d.Tiles = append(d.Tiles, t)
	}

	names := customEventNames(ctx, s.db, p.ID)
	pinned := map[string]bool{}
	for _, cc := range p.CustomCards {
		if cc.Value {
			pinned[cc.Event] = true
		}
	}
	goal := map[string]bool{}
	for _, g := range d.GoalNames {
		goal[g] = true
	}
	var pinnedNames []string
	for _, n := range pinnableBuiltIns {
		d.CustomEvents = append(d.CustomEvents, CustomEventDescriptor{Event: n, Active: pinned[n]})
		if pinned[n] {
			pinnedNames = append(pinnedNames, n)
		}
	}
	for _, n := range names {
		d.CustomEvents = append(d.CustomEvents, CustomEventDescriptor{Event: n, Active: pinned[n], Goal: goal[n], Custom: true})
		if pinned[n] {
			pinnedNames = append(pinnedNames, n)
		}
	}
	counts, series := readEventCounts(ctx, db, cur, curB, pinnedNames)
	countsPrev, _ := readEventCounts(ctx, db, prev, prevB, pinnedNames)
	for _, n := range pinnedNames {
		name := n
		if bn, ok := builtInCardNames[n]; ok {
			name = bn
		}
		t := card(name, formatNum(counts[n]), float64(counts[n]), float64(countsPrev[n]), series[n], "")
		t.Event = n
		d.Pinned = append(d.Pinned, t)
	}
	d.EventCards = append(append([]EventCard{}, d.Tiles...), d.Pinned...)
	for i := range d.Goals {
		d.Goals[i].Rate = int64(math.Round(1000 * ratio(float64(d.Goals[i].Count), float64(c.Visits))))
	}
}

// topOf ranks a map, ties by name so the order is stable.
func topOf(m map[string]int64, n int) []LabelCount {
	out := make([]LabelCount, 0, len(m))
	for k, v := range m {
		out = append(out, LabelCount{Label: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
