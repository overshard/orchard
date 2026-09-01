package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sparkline geometry. The SVG is drawn with preserveAspectRatio="none" so these
// are a coordinate space rather than pixels, and the card decides the real size
// in CSS.
const (
	sparkWidth  = 100.0
	sparkHeight = 32.0

	// Half a stroke of room at the top and bottom, so a day that closes at its
	// own high does not get its line clipped in half by the viewBox edge.
	sparkPad = 1.5
)

// Spark is one sparkline as the template writes it out: a path for the line, a
// closed path for the fill under it, and the y of the previous close so the
// card can rule a line across at the level the day is measured from. This is
// how Yahoo draws it, and the reference line is what makes the shape mean
// anything on its own.
type Spark struct {
	Line     string  `json:"line"`
	Area     string  `json:"area"`
	Baseline float64 `json:"baseline"`
	HasBase  bool    `json:"has_base"`
	Points   int     `json:"points"`

	// How far across the box the data reaches, and whether that is short of the
	// end. The axis is the whole session rather than the bars that have printed
	// so far, so ten minutes after the open the line covers a fortieth of the
	// card instead of all of it. Yahoo and every other intraday sparkline draw
	// it this way, and stretching five bars across a full card is a lie about
	// how much of the day has happened.
	Span    float64 `json:"span"`
	Partial bool    `json:"partial"`
}

// buildSpark scales closes into the viewBox. previous is included in the range
// so the baseline is always drawn inside the box rather than off the top of a
// day that only went up.
//
// times are the unix seconds of each close and axis is the session they are
// drawn against. Either being absent falls back to spacing the points evenly
// across the full width, which is right for anything that never closes.
func buildSpark(closes []float64, times []int64, previous float64, axis tradingAxis) Spark {
	if len(closes) < 2 {
		return Spark{}
	}

	lo, hi := closes[0], closes[0]
	for _, c := range closes {
		lo, hi = math.Min(lo, c), math.Max(hi, c)
	}
	hasBase := previous > 0
	if hasBase {
		lo, hi = math.Min(lo, previous), math.Max(hi, previous)
	}

	// A flat day has no range to scale against, so give it one and let the line
	// sit in the middle instead of dividing by zero.
	if hi-lo < 1e-9 {
		hi = lo + 1
	}

	scaleY := func(v float64) float64 {
		frac := (v - lo) / (hi - lo)
		// SVG y grows downward, so the high price is the small number.
		return sparkPad + (1-frac)*(sparkHeight-2*sparkPad)
	}

	xs := scaleX(closes, times, axis)

	var line strings.Builder
	for i, c := range closes {
		if i == 0 {
			line.WriteString("M")
		} else {
			line.WriteString("L")
		}
		line.WriteString(num(xs[i]))
		line.WriteString(",")
		line.WriteString(num(scaleY(c)))
		if i < len(closes)-1 {
			line.WriteString(" ")
		}
	}

	// The fill runs from the line down to the floor of the box, which is the
	// shape Yahoo uses. Filling to the baseline instead would need two clipped
	// halves to colour the above and below parts differently, and at 32 units
	// tall that reads as noise.
	first, last := xs[0], xs[len(xs)-1]
	area := line.String() +
		fmt.Sprintf(" L%s,%s L%s,%s Z", num(last), num(sparkHeight), num(first), num(sparkHeight))

	s := Spark{
		Line:   line.String(),
		Area:   area,
		Points: len(closes),
		Span:   round2(last),
		// A hair short of the end still counts as finished, since the last bar
		// of a session prints at its start and never at its close.
		Partial: last < sparkWidth-1,
	}
	if hasBase {
		s.Baseline = round2(scaleY(previous))
		s.HasBase = true
	}
	return s
}

// scaleX places each close along the session rather than along the list. A
// point outside the axis is clamped rather than dropped, since an extended
// hours bar is still a price and pushing it off the box would lose it.
func scaleX(closes []float64, times []int64, axis tradingAxis) []float64 {
	xs := make([]float64, len(closes))

	span := axis.end - axis.start
	if !axis.ok || span <= 0 || len(times) != len(closes) {
		step := sparkWidth / float64(len(closes)-1)
		for i := range closes {
			xs[i] = float64(i) * step
		}
		return xs
	}

	for i, t := range times {
		frac := float64(t-axis.start) / float64(span)
		xs[i] = math.Min(sparkWidth, math.Max(0, frac*sparkWidth))
	}
	return xs
}

// num keeps the path short. Two decimals in a 100 by 32 box is well under a
// rendered pixel and takes a third of the bytes that %g would.
func num(v float64) string {
	return strconv.FormatFloat(round2(v), 'f', -1, 64)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// formatNumber writes a price the way the rest of the page reads it: grouped
// thousands, fixed decimals.
func formatNumber(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)

	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// signed always carries the sign, because a change of zero read as "0.00" and a
// change of +0.00 mean different things on a card whose whole job is direction.
func signed(v float64, decimals int) string {
	s := formatNumber(math.Abs(v), decimals)
	if v < 0 {
		return "-" + s
	}
	return "+" + s
}
