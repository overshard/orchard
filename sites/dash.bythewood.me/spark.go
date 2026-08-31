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
}

// buildSpark scales closes into the viewBox. previous is included in the range
// so the baseline is always drawn inside the box rather than off the top of a
// day that only went up.
func buildSpark(closes []float64, previous float64) Spark {
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

	stepX := sparkWidth / float64(len(closes)-1)

	var line strings.Builder
	for i, c := range closes {
		if i == 0 {
			line.WriteString("M")
		} else {
			line.WriteString("L")
		}
		line.WriteString(num(float64(i) * stepX))
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
	area := line.String() +
		fmt.Sprintf(" L%s,%s L0,%s Z", num(sparkWidth), num(sparkHeight), num(sparkHeight))

	s := Spark{Line: line.String(), Area: area, Points: len(closes)}
	if hasBase {
		s.Baseline = round2(scaleY(previous))
		s.HasBase = true
	}
	return s
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
