package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// boardEvery is the rates and sector poll. Five minutes is finer than either
// number deserves and it is a fifth of what riding the market poll cost.
const boardEvery = 5 * time.Minute

// Treasury yields and the sector board. Both ride the request the market strip
// already makes, so the whole of this file costs no extra call.

// Rate is one point on the curve. Yahoo publishes these as index symbols whose
// price is the yield.
type Rate struct {
	Key    string
	Symbol string
	Label  string
}

var rates = []Rate{
	{Key: "m3", Symbol: "^IRX", Label: "3M"},
	{Key: "y5", Symbol: "^FVX", Label: "5Y"},
	{Key: "y10", Symbol: "^TNX", Label: "10Y"},
	{Key: "y30", Symbol: "^TYX", Label: "30Y"},
}

type RateRow struct {
	Label       string `json:"label"`
	Yield       string `json:"yield"`
	Change      string `json:"change"`
	Direction   string `json:"direction"`
	Unavailable bool   `json:"unavailable"`

	// How long this row's bar is. Four numbers in a column say what each
	// maturity pays and nothing about the shape they make together, and the
	// shape is the only reason anyone looks at a curve. Reading the bars down
	// the panel gives the ladder without spending a block on a chart.
	Fill float64 `json:"fill"`
}

type Rates struct {
	Rows []RateRow `json:"rows"`

	// The shape in a word, which is the read the spread is there to give.
	Shape string `json:"shape"`

	// Curve is the ten year less the three month. It is the spread with the
	// better record as a recession signal than the two year version everyone
	// quotes, and it is the one these four symbols can actually produce, since
	// Yahoo has no clean two year index.
	Curve      string `json:"curve"`
	CurveState string `json:"curve_state"`
}

// normaliseYield handles Yahoo's legacy scaling. These indexes were quoted at
// ten times the yield for years and are now quoted as the yield itself, and a
// treasury paying 42% would be a story bigger than this dashboard, so anything
// above twenty is the old form.
func normaliseYield(v float64) float64 {
	if v > 20 {
		return v / 10
	}
	return v
}

func buildRates(quotes map[string]Quote) Rates {
	var r Rates
	yields := map[string]float64{}

	for _, rate := range rates {
		row := RateRow{Label: rate.Label}

		q, ok := quotes[rate.Symbol]
		if !ok || q.Price == 0 {
			row.Unavailable = true
			r.Rows = append(r.Rows, row)
			continue
		}

		y := normaliseYield(q.Price)
		prev := normaliseYield(q.Previous)
		yields[rate.Key] = y

		// Moves are in basis points, which is how anyone reading a curve thinks
		// about them. A hundredth of a percent shown as "0.03%" is noise.
		bps := (y - prev) * 100
		row.Yield = fmt.Sprintf("%.2f%%", y)
		row.Change = signed(bps, 0) + "bp"
		row.Direction = direction(bps)
		r.Rows = append(r.Rows, row)
	}

	if ten, ok := yields["y10"]; ok {
		if three, ok := yields["m3"]; ok {
			spread := (ten - three) * 100
			r.Curve = signed(spread, 0) + "bp"
			switch {
			case spread < 0:
				r.CurveState = "inverted"
			// Under a quarter point across seven and a half years of maturity
			// is not a slope anyone would trade on.
			case spread < 25:
				r.CurveState = "flat"
			default:
				r.CurveState = "normal"
			}
			r.Shape = strings.ToUpper(r.CurveState)
		}
	}

	scaleRates(&r, yields)
	return r
}

// scaleRates sets each row's bar length. The scale runs from zero to the top of
// the range rounded up, so a bar is proportional to the yield it draws and the
// four together read as the ladder. A relative scale between the lowest and the
// highest would turn a quarter point of spread into a cliff.
func scaleRates(r *Rates, yields map[string]float64) {
	var hi float64
	for _, y := range yields {
		hi = math.Max(hi, y)
	}
	if hi <= 0 {
		return
	}
	// Up to the next half point, so the longest bar stops short of the end and
	// has somewhere to grow.
	top := math.Ceil(hi*2) / 2

	for i := range r.Rows {
		y, ok := yields[rates[i].Key]
		if !ok {
			continue
		}
		r.Rows[i].Fill = math.Round(y/top*1000) / 10
	}
}

// Sector is one of the eleven SPDR funds the S&P is cut into. Together they are
// the answer to what is actually moving on a day the index moved.
type Sector struct {
	Symbol string
	Label  string
}

var sectors = []Sector{
	{"XLK", "TECH"},
	{"XLC", "COMM"},
	{"XLY", "DISC"},
	{"XLF", "FIN"},
	{"XLV", "HEALTH"},
	{"XLI", "INDUS"},
	{"XLP", "STAPLE"},
	{"XLE", "ENERGY"},
	{"XLU", "UTIL"},
	{"XLRE", "REIT"},
	{"XLB", "MATRL"},
}

type SectorCell struct {
	Label       string  `json:"label"`
	Benchmark   bool    `json:"benchmark"`
	Percent     string  `json:"percent"`
	Direction   string  `json:"direction"`
	Heat        int     `json:"heat"`
	Raw         float64 `json:"raw"`
	Unavailable bool    `json:"unavailable"`
}

// benchmark sits in the board with the eleven sectors, both so the grid is a
// complete three by four and because the only useful thing to know about a
// sector's day is whether it beat the index.
var benchmark = Sector{"SPY", "S&P 500"}

// buildSectors returns the cells ordered by the day's move, so the board reads
// best to worst rather than in a fixed order nobody remembers.
func buildSectors(quotes map[string]Quote) []SectorCell {
	cells := make([]SectorCell, 0, len(sectors)+1)

	for _, s := range append(sectors, benchmark) {
		cell := SectorCell{Label: s.Label, Benchmark: s.Symbol == benchmark.Symbol}

		q, ok := quotes[s.Symbol]
		if !ok || q.Price == 0 {
			cell.Unavailable = true
			cells = append(cells, cell)
			continue
		}

		pct := q.percent()
		cell.Raw = pct
		cell.Percent = signed(pct, 2) + "%"
		cell.Direction = direction(pct)
		cell.Heat = heatStep(pct)
		cells = append(cells, cell)
	}

	// Descending, with anything unavailable pushed to the end rather than
	// sorting as a zero in the middle of the board.
	for i := 1; i < len(cells); i++ {
		for j := i; j > 0; j-- {
			a, b := cells[j-1], cells[j]
			if a.Unavailable && !b.Unavailable || (!a.Unavailable && !b.Unavailable && b.Raw > a.Raw) {
				cells[j-1], cells[j] = b, a
				continue
			}
			break
		}
	}
	return cells
}

// heatStep buckets a move into four shades either side of flat. The clamp is at
// three percent because a sector moving more than that is rare enough that
// finer gradations above it would only ever show one colour.
func heatStep(pct float64) int {
	mag := pct
	if mag < 0 {
		mag = -mag
	}
	switch {
	case mag >= 2.0:
		return 4
	case mag >= 1.0:
		return 3
	case mag >= 0.4:
		return 2
	case mag >= 0.1:
		return 1
	default:
		return 0
	}
}

// rateAndSectorSymbols is everything this file needs, appended to the one
// request the market poll already makes.
func rateAndSectorSymbols() []string {
	out := make([]string, 0, len(rates)+len(sectors)+1)
	for _, r := range rates {
		out = append(out, r.Symbol)
	}
	for _, s := range append(sectors, benchmark) {
		out = append(out, s.Symbol)
	}
	return out
}
