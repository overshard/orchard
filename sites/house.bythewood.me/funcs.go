package main

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"
)

// One set of formatters, shared between the Go side and the templates, so a
// number reads the same on a card, in a breakdown and in an alert.

// commaInt is the thousands separator. It takes a plain int because every caller
// here has one.
func commaInt(v int) string {
	s := strconv.Itoa(v)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// num is the template-facing separator, taking any because a template handing an
// int to a function declared int64 fails at render time with "wrong type for
// value", which compiles, passes every test and 500s the page.
func numAny(v any) string {
	switch t := v.(type) {
	case int:
		return commaInt(t)
	case int64:
		return commaInt(int(t))
	case float64:
		return commaInt(int(math.Round(t)))
	case nil:
		return "0"
	default:
		return fmt.Sprint(v)
	}
}

// money takes any for the same reason num does: a template handing an int to a
// function declared float64 fails at render time with "wrong type for value",
// which compiles, passes every test, and 500s the page.
func money(v any) string {
	switch t := v.(type) {
	case int:
		return "$" + commaInt(t)
	case int64:
		return "$" + commaInt(int(t))
	case float64:
		return "$" + commaInt(int(math.Round(t)))
	default:
		return "$" + fmt.Sprint(v)
	}
}

// feet reads in feet up to a quarter mile and in miles past it, because "1,847
// ft from the creek" is harder to picture than "0.35 mi".
func feet(v float64) string {
	switch {
	case v == notFound:
		return "none nearby"
	case v < 1320:
		return commaInt(int(math.Round(v/10)*10)) + " ft"
	default:
		return strconv.FormatFloat(v/5280, 'f', 2, 64) + " mi"
	}
}

func fmtMinutes(v float64) string {
	if v <= 0 {
		return "unknown"
	}
	if v < 1 {
		return "under a minute"
	}
	return strconv.Itoa(int(math.Round(v))) + " min"
}

// trimFloat drops a trailing zero, so 1.00 acres reads as 1 and 1.25 keeps both.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// ago is how long a house has been sitting, which is half of what a listing page
// never tells you plainly.
func ago(ts int64) string {
	if ts == 0 {
		return "unknown"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " days ago"
	default:
		return time.Unix(ts, 0).Format("Jan 2")
	}
}

// scoreBand is what the card colours its badge by. Three bands rather than a
// gradient, since the point of a glance is a decision and not a shade.
func scoreBand(score float64) string {
	switch {
	case score >= 70:
		return "good"
	case score >= 50:
		return "fair"
	default:
		return "poor"
	}
}

// detourBand is the stated judgement: under 8 minutes is great, under 15 is
// workable, over 20 is a real problem. The three numbers are config.
func detourBand(cfg Config, mins float64) string {
	switch {
	case mins <= 0:
		return "unknown"
	case mins <= cfg.Household.DropoffGreatMin:
		return "good"
	case mins <= cfg.Household.DropoffOkMin:
		return "fair"
	default:
		return "poor"
	}
}

// html/template sanitises inline style attributes and does not understand CSS
// custom properties, so `style="--fill: 86%"` is rewritten to ZgotmplZ and every
// bar renders empty. These return template.CSS and build a plain declaration.
func widthPct(v float64) template.CSS {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return template.CSS("width:" + strconv.FormatFloat(v, 'f', 1, 64) + "%")
}

func sizeRem(rem string) template.CSS {
	// Only ever called with a literal from a template, and clamped to something
	// that cannot carry a declaration of its own.
	for _, r := range rem {
		if !strings.ContainsRune("0123456789.rem", r) {
			return template.CSS("")
		}
	}
	// The font size goes out with the box size because the number inside is sized
	// as a percentage, and a percentage font size resolves against the inherited
	// font size rather than against the element's own width.
	return template.CSS("width:" + rem + ";height:" + rem + ";font-size:" + rem)
}

// dialData is the score ring. The size is passed through as a CSS length so one
// template draws the small one on a card and the big one on a report.
type dialData struct {
	Score   float64
	Size    string
	Out     bool
	Pending bool
}

// The circumference of the ring, 2 x pi x 45 rounded, which is the radius the
// template draws. The offset is what is left of it after the score is filled.
const dialCircumference = 283.0

func dashOffset(score float64) string {
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return strconv.FormatFloat(dialCircumference*(1-score/100), 'f', 1, 64)
}

// meterBand colours a factor's bar. Three bands rather than a gradient, since the
// point of a glance is a judgement and not a shade.
func meterBand(raw float64) string {
	switch {
	case raw >= 0.66:
		return "high"
	case raw >= 0.4:
		return "mid"
	default:
		return "low"
	}
}

// cardCtx is what a card template needs: the row and the config it is judged
// against. Go templates have no dict, so the pairing is a function.
type cardCtx struct {
	Card   Card
	Config Config
}

var templateFuncs = template.FuncMap{
	"cardCtx": func(cfg Config, c Card) cardCtx { return cardCtx{Card: c, Config: cfg} },
	"add1":    func(i int) int { return i + 1 },
	"dial": func(score float64, size string, out, pending bool) dialData {
		return dialData{Score: score, Size: size, Out: out, Pending: pending}
	},
	"widthPct":  widthPct,
	"sizeRem":   sizeRem,
	"dash":      dashOffset,
	"meterBand": meterBand,
	"detour":    detourBand,
	"mul":       func(a, b float64) float64 { return a * b },
	"add2":      func(a, b float64) float64 { return a + b },
	"num":       numAny,
	"money":     money,
	"feet":      feet,
	"minutes":   fmtMinutes,
	"acres":     trimFloat,
	"ago":       ago,
	"band":      scoreBand,
	"pct":       func(v float64) string { return strconv.Itoa(int(math.Round(v))) + "%" },
	"round":     func(v float64) int { return int(math.Round(v)) },
	"lower":     strings.ToLower,
	"join":      strings.Join,
}
