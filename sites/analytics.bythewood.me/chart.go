package main

import (
	"fmt"
	"html/template"
	"strings"
)

// ChartMark is a pushed alert on the visitors chart, at its bucket.
type ChartMark struct {
	I     int    `json:"i"`
	X     string `json:"-"`
	Label string `json:"label"`
	Tip   string `json:"tip"`
	// Right hangs the label off the left of its rule, near the chart's end.
	Right bool `json:"-"`
}

const chartW, chartH = 1000.0, 150.0

// visitorsChart draws the visitors line over the previous period's, muted, on
// one axis, with a dotted rule at each alert. Text sits outside the SVG since
// the drawing stretches to its box and would stretch the letters with it.
func visitorsChart(points []GraphPoint, alerts []Alert, b *buckets) (template.HTML, []ChartMark) {
	n := len(points)
	if n == 0 {
		return "", nil
	}
	top := int64(1)
	for _, p := range points {
		top = max(top, p.Count, p.Prev)
	}
	scale := float64(top) * 1.1
	x := func(i int) float64 {
		if n == 1 {
			return chartW / 2
		}
		return float64(i) / float64(n-1) * chartW
	}
	y := func(v int64) float64 { return chartH - float64(v)/scale*(chartH-4) }
	line := func(get func(GraphPoint) int64) string {
		var sb strings.Builder
		for i, p := range points {
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&sb, "%s%.1f,%.1f ", cmd, x(i), y(get(p)))
		}
		return strings.TrimSpace(sb.String())
	}
	cur := line(func(p GraphPoint) int64 { return p.Count })
	prev := line(func(p GraphPoint) int64 { return p.Prev })

	var marks []ChartMark
	taken := map[int]bool{}
	for _, a := range alerts {
		i := b.at(a.CreatedAt / 3600000)
		if i < 0 || taken[i] {
			continue
		}
		taken[i] = true
		label := "busy"
		if a.Kind == "link" {
			label = "linked"
			if _, from, ok := strings.Cut(a.Title, " linked from "); ok {
				label += " · " + from
			}
		}
		marks = append(marks, ChartMark{I: i, X: fmt.Sprintf("%.2f", x(i)/chartW*100), Label: label, Tip: a.Title + "\n" + a.Body, Right: x(i) > chartW*0.6})
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-hidden="true">`, chartW, chartH)
	for k := 1; k <= 3; k++ {
		gy := chartH - chartH*float64(k)/4
		fmt.Fprintf(&sb, `<line class="gl" x1="0" x2="%.0f" y1="%.1f" y2="%.1f"/>`, chartW, gy, gy)
	}
	for _, m := range marks {
		fmt.Fprintf(&sb, `<line class="mk" x1="%.1f" x2="%.1f" y1="0" y2="%.0f"/>`, x(m.I), x(m.I), chartH)
	}
	fmt.Fprintf(&sb, `<path class="pv" d="%s"/>`, prev)
	fmt.Fprintf(&sb, `<path class="ca" d="%s L%.1f,%.0f L%.1f,%.0f Z"/>`, cur, x(n-1), chartH, x(0), chartH)
	fmt.Fprintf(&sb, `<path class="cl" d="%s"/></svg>`, cur)
	return template.HTML(sb.String()), marks
}

// axisLabels picks up to four bucket labels spread along the chart.
func axisLabels(points []GraphPoint) []string {
	n := len(points)
	if n == 0 {
		return nil
	}
	if n <= 4 {
		out := make([]string, n)
		for i, p := range points {
			out[i] = p.Label
		}
		return out
	}
	return []string{points[0].Label, points[(n-1)/3].Label, points[2*(n-1)/3].Label, points[n-1].Label}
}
