package skills

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Frankfurter publishes the ECB reference rates, keyless and without a quota.
// Rates are daily rather than live, which is right for "how much is 200 euros"
// and wrong for trading, and the answer says which it is.
const fxURL = "https://api.frankfurter.dev/v1/latest"

// Convert turns one unit into another.
//
// The units half never leaves the process, because a conversion factor is a
// constant and fetching a page to read one is absurd. The currency half is the
// only part that needs the network.
type Convert struct{}

func (Convert) Card() Card {
	return Card{
		Name: "convert",
		Does: "converts a quantity between units of temperature, length, weight, volume or speed, or between two currencies.",
		Fires: []string{
			"30 celsius to fahrenheit",
			"how many km in 5 miles",
			"100 usd in eur",
			"convert 180 pounds to kg",
			"how many cups is 500ml",
		},
		NotFor: []string{
			"why is the euro weak",
			"what currency does japan use",
			"how do i convert a pdf to word",
			"what is the exchange rate history",
			"how much does a car cost in europe",
		},
		Keywords: []string{" to fahrenheit", " to celsius", " in eur", " in usd",
			" to kg", " to pounds", " to km", " to miles", "convert "},
	}
}

// unit is one measure and its size in the family's base unit.
type unit struct {
	family string
	per    float64
	names  []string
}

var units = []unit{
	{"length", 0.001, []string{"mm", "millimetre", "millimeter", "millimetres", "millimeters"}},
	{"length", 0.01, []string{"cm", "centimetre", "centimeter", "centimetres", "centimeters"}},
	{"length", 1, []string{"m", "metre", "meter", "metres", "meters"}},
	{"length", 1000, []string{"km", "kilometre", "kilometer", "kilometres", "kilometers"}},
	{"length", 0.0254, []string{"in", "inch", "inches"}},
	{"length", 0.3048, []string{"ft", "foot", "feet"}},
	{"length", 0.9144, []string{"yd", "yard", "yards"}},
	{"length", 1609.344, []string{"mi", "mile", "miles"}},

	{"weight", 0.001, []string{"g", "gram", "grams", "gramme", "grammes"}},
	{"weight", 1, []string{"kg", "kilo", "kilos", "kilogram", "kilograms"}},
	{"weight", 0.0283495, []string{"oz", "ounce", "ounces"}},
	{"weight", 0.453592, []string{"lb", "lbs", "pound", "pounds"}},
	{"weight", 6.35029, []string{"st", "stone", "stones"}},
	{"weight", 1000, []string{"tonne", "tonnes", "metric ton"}},

	{"volume", 0.001, []string{"ml", "millilitre", "milliliter", "millilitres", "milliliters"}},
	{"volume", 1, []string{"l", "litre", "liter", "litres", "liters"}},
	{"volume", 0.236588, []string{"cup", "cups"}},
	{"volume", 0.0147868, []string{"tbsp", "tablespoon", "tablespoons"}},
	{"volume", 0.00492892, []string{"tsp", "teaspoon", "teaspoons"}},
	{"volume", 3.78541, []string{"gal", "gallon", "gallons"}},
	{"volume", 0.473176, []string{"pint", "pints"}},
	{"volume", 0.0295735, []string{"fl oz", "fluid ounce", "fluid ounces"}},

	{"speed", 1, []string{"kph", "km/h", "kmh", "kilometres per hour", "kilometers per hour"}},
	{"speed", 1.609344, []string{"mph", "mi/h", "miles per hour"}},
	{"speed", 3.6, []string{"m/s", "metres per second", "meters per second"}},
	{"speed", 1.852, []string{"knot", "knots", "kt"}},
}

// Temperature is not a ratio, so it cannot live in the table above.
var tempNames = map[string]string{
	"c": "C", "celsius": "C", "centigrade": "C", "°c": "C",
	"f": "F", "fahrenheit": "F", "°f": "F",
	"k": "K", "kelvin": "K",
}

var currencies = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "JPY": true, "CHF": true, "CAD": true,
	"AUD": true, "NZD": true, "SEK": true, "NOK": true, "DKK": true, "PLN": true,
	"CZK": true, "HUF": true, "RON": true, "BGN": true, "TRY": true, "ILS": true,
	"ZAR": true, "MXN": true, "BRL": true, "INR": true, "CNY": true, "HKD": true,
	"SGD": true, "KRW": true, "THB": true, "MYR": true, "PHP": true, "IDR": true,
	"ISK": true,
}

var currencyWords = map[string]string{
	"dollar": "USD", "dollars": "USD", "usd": "USD", "$": "USD",
	"euro": "EUR", "euros": "EUR", "eur": "EUR", "€": "EUR",
	"pound": "GBP", "pounds": "GBP", "gbp": "GBP", "£": "GBP", "sterling": "GBP",
	"yen": "JPY", "jpy": "JPY", "¥": "JPY",
	"franc": "CHF", "francs": "CHF", "chf": "CHF",
	"rupee": "INR", "rupees": "INR", "inr": "INR",
	"yuan": "CNY", "cny": "CNY", "rmb": "CNY",
	"won": "KRW", "krw": "KRW",
	"peso": "MXN", "pesos": "MXN", "mxn": "MXN",
	"real": "BRL", "reais": "BRL", "brl": "BRL",
	"rand": "ZAR", "zar": "ZAR",
	"cad": "CAD", "aud": "AUD", "nzd": "NZD", "sek": "SEK", "nok": "NOK",
}

// The shapes a conversion is written in: "30 c to f", "how many km in 5 miles",
// "convert 180 lb to kg". The number can sit on either side of the unit pair.
var (
	reAtoB    = regexp.MustCompile(`(-?[\d.,]+)\s*([a-z°$€£¥/ ]{1,22}?)\s+(?:to|in|into|as)\s+([a-z°$€£¥/ ]{1,22})`)
	reHowMany = regexp.MustCompile(`how many\s+([a-z°/ ]{1,22}?)\s+(?:are\s+)?(?:in|is)\s+(-?[\d.,]+)\s*([a-z°/ ]{1,22})`)
)

type conversion struct {
	amount   float64
	from, to string
}

func parseConversion(q string) (conversion, bool) {
	l := strings.ToLower(strings.TrimSpace(q))
	l = strings.TrimSuffix(l, "?")
	l = strings.ReplaceAll(l, "degrees ", "")

	if m := reHowMany.FindStringSubmatch(l); m != nil {
		n, err := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)
		if err == nil {
			return conversion{n, strings.TrimSpace(m[3]), strings.TrimSpace(m[1])}, true
		}
	}
	if m := reAtoB.FindStringSubmatch(l); m != nil {
		n, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err == nil {
			return conversion{n, strings.TrimSpace(m[2]), strings.TrimSpace(m[3])}, true
		}
	}
	return conversion{}, false
}

func findUnit(name string) (unit, bool) {
	name = strings.TrimSpace(name)
	for _, u := range units {
		for _, n := range u.names {
			if n == name {
				return u, true
			}
		}
	}
	return unit{}, false
}

func findCurrency(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if c, ok := currencyWords[name]; ok {
		return c, true
	}
	up := strings.ToUpper(name)
	if currencies[up] {
		return up, true
	}
	return "", false
}

func (Convert) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()
	c, ok := parseConversion(question)
	if !ok {
		return nil, nil
	}

	if text, ok := convertTemperature(c); ok {
		return &Result{Skill: "convert", Shape: "factual", Text: text,
			Elapsed: d.now().Sub(start).Round(time.Millisecond).String()}, nil
	}
	if text, ok := convertUnits(c); ok {
		return &Result{Skill: "convert", Shape: "factual", Text: text,
			Elapsed: d.now().Sub(start).Round(time.Millisecond).String()}, nil
	}

	from, okF := findCurrency(c.from)
	to, okT := findCurrency(c.to)
	if !okF || !okT || from == to {
		return nil, nil
	}
	var p struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	u := fmt.Sprintf("%s?base=%s&symbols=%s", fxURL, from, to)
	if err := getJSON(ctx, d, u, &p); err != nil {
		return nil, err
	}
	rate, ok := p.Rates[to]
	if !ok || rate == 0 {
		return nil, nil
	}
	text := fmt.Sprintf("**%s %s**\n\n- **%s %s** at **%.4f** %s per %s\n\n"+
		"European Central Bank reference rate for %s, which is set once a day rather than live.",
		formatNumber(round2(c.amount*rate)), to,
		formatNumber(round2(c.amount)), from, rate, to, from, p.Date)

	return &Result{
		Skill: "convert", Shape: "factual", Text: text,
		Sources: []Source{{URL: "https://frankfurter.dev/", Title: "Frankfurter, ECB rates", Site: "frankfurter.dev"}},
		Elapsed: d.now().Sub(start).Round(10 * time.Millisecond).String(),
	}, nil
}

func convertTemperature(c conversion) (string, bool) {
	from, okF := tempNames[strings.TrimSpace(c.from)]
	to, okT := tempNames[strings.TrimSpace(c.to)]
	if !okF || !okT || from == to {
		return "", false
	}
	var celsius float64
	switch from {
	case "C":
		celsius = c.amount
	case "F":
		celsius = (c.amount - 32) * 5 / 9
	case "K":
		celsius = c.amount - 273.15
	}
	var out float64
	switch to {
	case "C":
		out = celsius
	case "F":
		out = celsius*9/5 + 32
	case "K":
		out = celsius + 273.15
	}
	return fmt.Sprintf("**%.1f°%s**\n\n- **%.1f°%s** is **%.1f°%s**",
		out, to, c.amount, from, out, to), true
}

func convertUnits(c conversion) (string, bool) {
	from, okF := findUnit(c.from)
	to, okT := findUnit(c.to)
	if !okF || !okT || from.family != to.family {
		return "", false
	}
	out := c.amount * from.per / to.per
	return fmt.Sprintf("**%s %s**\n\n- **%s %s** is **%s %s**",
		trimNum(out), c.to, trimNum(c.amount), c.from, trimNum(out), c.to), true
}

// trimNum keeps enough precision to be useful without printing nine decimals
// for a number a person is going to round anyway.
func trimNum(v float64) string {
	switch a := abs(v); {
	case a >= 100:
		return formatNumber(round2(v))
	case a >= 1:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
	default:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
	}
}
