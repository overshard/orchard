package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------- music

var MusicLookup = Tool{
	Name: "music_lookup",
	Description: "Check that a song or artist actually exists and get its album and year, from the " +
		"iTunes catalogue. Use it before listing songs you are not certain about, since an invented " +
		"track is the one mistake in a playlist a reader cannot see.",
	Schema: obj(map[string]any{
		"artist": str("the artist"),
		"track":  str("the song title, optional"),
	}, "artist"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		term := strings.TrimSpace(argStr(a, "artist") + " " + argStr(a, "track"))
		if term == "" {
			return nil, fmt.Errorf("artist is required")
		}
		var res struct {
			Results []struct {
				ArtistName     string `json:"artistName"`
				TrackName      string `json:"trackName"`
				CollectionName string `json:"collectionName"`
				ReleaseDate    string `json:"releaseDate"`
			} `json:"results"`
		}
		u := "https://itunes.apple.com/search?media=music&limit=5&term=" + url.QueryEscape(term)
		if err := getJSON(ctx, d, u, &res); err != nil {
			return nil, err
		}
		type match struct {
			Artist string `json:"artist"`
			Track  string `json:"track"`
			Album  string `json:"album"`
			Year   string `json:"year"`
		}
		out := make([]match, 0, len(res.Results))
		for _, r := range res.Results {
			y := r.ReleaseDate
			if len(y) >= 4 {
				y = y[:4]
			}
			out = append(out, match{r.ArtistName, r.TrackName, r.CollectionName, y})
		}
		if len(out) == 0 {
			return map[string]any{"query": term, "matches": out,
				"note": "nothing in the catalogue matches, so treat this as not existing"}, nil
		}
		return map[string]any{"query": term, "matches": out}, nil
	},
}

// ---------------------------------------------------------------- convert

// units are grouped so a length never converts into a weight. Volume to weight
// is the one crossing allowed and it is only true for water, which is why it
// says so in the answer rather than quietly being wrong about flour.
var (
	weight = map[string]float64{"mg": 0.001, "g": 1, "kg": 1000, "oz": 28.3495, "lb": 453.592, "st": 6350.29}
	volume = map[string]float64{"ml": 1, "l": 1000, "tsp": 4.92892, "tbsp": 14.7868,
		"cup": 236.588, "floz": 29.5735, "pint": 473.176, "quart": 946.353, "gallon": 3785.41}
	length = map[string]float64{"mm": 0.001, "cm": 0.01, "m": 1, "km": 1000,
		"in": 0.0254, "ft": 0.3048, "yd": 0.9144, "mi": 1609.34}
)

var unitAlias = map[string]string{
	"gram": "g", "grams": "g", "gramme": "g", "kilogram": "kg", "kilograms": "kg", "kilo": "kg",
	"ounce": "oz", "ounces": "oz", "pound": "lb", "pounds": "lb", "lbs": "lb",
	"millilitre": "ml", "milliliter": "ml", "litre": "l", "liter": "l", "liters": "l", "litres": "l",
	"teaspoon": "tsp", "teaspoons": "tsp", "tablespoon": "tbsp", "tablespoons": "tbsp",
	"cups": "cup", "fluid ounce": "floz", "fl oz": "floz", "fahrenheit": "f", "celsius": "c",
	"centigrade": "c", "inch": "in", "inches": "in", "foot": "ft", "feet": "ft",
	"mile": "mi", "miles": "mi", "metre": "m", "meter": "m", "kilometre": "km", "kilometer": "km",
}

func normUnit(u string) string {
	u = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(u, ".")))
	u = strings.TrimPrefix(u, "°")
	if v, ok := unitAlias[u]; ok {
		return v
	}
	return u
}

var Convert = Tool{
	Name:        "convert",
	Description: "Convert between units of weight, volume, length or temperature.",
	Schema: obj(map[string]any{
		"value":     num("the number to convert"),
		"from_unit": str("the unit it is in, like cup, lb, f, mi"),
		"to_unit":   str("the unit to convert to, like g, kg, c, km"),
	}, "value", "from_unit", "to_unit"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		v := argNum(a, "value", 0)
		from, to := normUnit(argStr(a, "from_unit")), normUnit(argStr(a, "to_unit"))
		if from == "" || to == "" {
			return nil, fmt.Errorf("from_unit and to_unit are required")
		}
		if from == "c" || from == "f" || to == "c" || to == "f" {
			switch {
			case from == "c" && to == "f":
				return map[string]any{"value": round1(v*9/5 + 32), "unit": "F"}, nil
			case from == "f" && to == "c":
				return map[string]any{"value": round1((v - 32) * 5 / 9), "unit": "C"}, nil
			case from == to:
				return map[string]any{"value": v, "unit": strings.ToUpper(from)}, nil
			}
			return nil, fmt.Errorf("temperature only converts to temperature")
		}
		for _, set := range []map[string]float64{weight, volume, length} {
			f, okF := set[from]
			t, okT := set[to]
			if okF && okT {
				return map[string]any{"value": round4(v * f / t), "unit": to}, nil
			}
		}
		// The one crossing worth allowing, said out loud.
		if f, ok := volume[from]; ok {
			if t, ok2 := weight[to]; ok2 {
				return map[string]any{"value": round4(v * f / t), "unit": to,
					"warning": "volume to weight is only right for water. Flour, sugar and butter all differ, so say so or look up the real density."}, nil
			}
		}
		return nil, fmt.Errorf("cannot convert %s to %s", from, to)
	},
}

func round4(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }

// ---------------------------------------------------------------- calc

var safeExpr = regexp.MustCompile(`^[0-9\.\+\-\*/\(\)\s%]+$`)

// Long enough for any real sum and short enough that writing one cannot use up
// a whole round's token budget.
const maxExprChars = 600

var Calc = Tool{
	Name: "calc",
	Description: "Evaluate an arithmetic expression. Use it rather than doing sums in your head, especially " +
		"for totals and budgets. Keep the expression short: to total a long list of numbers, add them in " +
		"groups of about twenty and then total the groups, rather than writing every number into one call.",
	Schema: obj(map[string]any{"expression": str("arithmetic only, like (1299 + 210) * 0.93")}, "expression"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		e := argStr(a, "expression")
		// A model totalling a bank statement will write every line into one
		// expression and run out of tokens partway, so the call arrives cut in
		// half. Saying so is more use than evaluating whatever survived.
		if len(e) > maxExprChars {
			return nil, fmt.Errorf("that expression is too long at %d characters, add the numbers in groups of about twenty and total the groups", len(e))
		}
		if !safeExpr.MatchString(e) {
			return nil, fmt.Errorf("only arithmetic is supported, no names or functions")
		}
		v, err := evalExpr(e)
		if err != nil {
			return nil, err
		}
		return map[string]any{"expression": e, "value": v}, nil
	},
}

// ---------------------------------------------------------------- now

var Now = Tool{
	Name:        "now",
	Description: "The current date and time. Use it whenever the answer depends on what day it is.",
	Schema:      obj(map[string]any{"timezone": str("IANA name, default America/New_York")}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		name := argStr(a, "timezone")
		if name == "" {
			name = "America/New_York"
		}
		loc, err := time.LoadLocation(name)
		if err != nil {
			return nil, fmt.Errorf("no timezone called %q", name)
		}
		t := d.Now().In(loc)
		return map[string]any{
			"iso": t.Format(time.RFC3339), "readable": t.Format("Monday, 2 January 2006 at 3:04 PM MST"),
			"weekday": t.Format("Monday"), "timezone": name,
		}, nil
	},
}
