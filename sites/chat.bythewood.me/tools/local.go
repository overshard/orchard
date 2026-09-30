package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------- music

var MusicLookup = Tool{
	Name: "music_lookup",
	Description: "Check that a song or artist actually exists and get its album and year, from the " +
		"iTunes catalogue. Use it before listing songs you are not certain about, since an invented " +
		"track is the one mistake in a playlist a reader cannot see. " +
		"Only for a question that is already about music. It searches a music catalogue and cannot " +
		"say what an unfamiliar name refers to, so never reach for it to identify a person, a " +
		"channel or a company because the name sounds like a band.",
	Schema: obj(map[string]any{
		"artist": str("the artist"),
		"track":  str("the song title, optional"),
	}, "artist"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		term := strings.TrimSpace(argStr(a, "artist") + " " + argStr(a, "track"))
		if term == "" {
			return nil, fmt.Errorf("artist is required")
		}
		track := strings.TrimSpace(argStr(a, "track"))
		if out, ok := artistSongs(ctx, d, strings.TrimSpace(argStr(a, "artist")), track); ok {
			return map[string]any{"query": term, "matches": out}, nil
		}
		var res itunesResults
		u := "https://itunes.apple.com/search?media=music&entity=song&limit=5&term=" + url.QueryEscape(term)
		if err := getJSON(ctx, d, u, &res); err != nil {
			return nil, err
		}
		out := make([]songMatch, 0, len(res.Results))
		for _, r := range res.Results {
			out = append(out, r.match())
		}
		if len(out) == 0 {
			return map[string]any{"query": term, "matches": out,
				"note": "nothing in the catalogue matches, so treat this as not existing"}, nil
		}
		return map[string]any{"query": term, "matches": out}, nil
	},
}

type itunesResults struct {
	Results []itunesItem `json:"results"`
}

type itunesItem struct {
	WrapperType    string `json:"wrapperType"`
	ArtistID       int64  `json:"artistId"`
	ArtistName     string `json:"artistName"`
	TrackName      string `json:"trackName"`
	CollectionName string `json:"collectionName"`
	ReleaseDate    string `json:"releaseDate"`
}

type songMatch struct {
	Artist string `json:"artist"`
	Track  string `json:"track"`
	Album  string `json:"album"`
	Year   string `json:"year"`
}

func (r itunesItem) match() songMatch {
	y := r.ReleaseDate
	if len(y) >= 4 {
		y = y[:4]
	}
	return songMatch{r.ArtistName, r.TrackName, r.CollectionName, y}
}

// artistSongs reads the artist's own catalogue rather than searching every
// song, where an original is buried under its covers and tribute albums. The
// earliest release comes first, since that is usually the original.
func artistSongs(ctx context.Context, d *Deps, artist, track string) ([]songMatch, bool) {
	if artist == "" || track == "" {
		return nil, false
	}
	var found itunesResults
	u := "https://itunes.apple.com/search?media=music&entity=musicArtist&limit=1&term=" + url.QueryEscape(artist)
	if getJSON(ctx, d, u, &found) != nil || len(found.Results) == 0 || found.Results[0].ArtistID == 0 {
		return nil, false
	}
	var songs itunesResults
	u = fmt.Sprintf("https://itunes.apple.com/lookup?entity=song&limit=200&id=%d", found.Results[0].ArtistID)
	if getJSON(ctx, d, u, &songs) != nil {
		return nil, false
	}
	want := foldTitle(track)
	var out []songMatch
	for _, r := range songs.Results {
		if r.WrapperType == "track" && strings.Contains(foldTitle(r.TrackName), want) {
			out = append(out, r.match())
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out[:min(len(out), 6)], true
}

func foldTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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

// quoteChars lists the offending characters once each, in the order they turned
// up, since a model told which character to drop fixes it on the next call.
func quoteChars(s string) string {
	var seen []string
	for _, r := range s {
		q := strconv.QuoteRune(r)
		if !slices.Contains(seen, q) {
			seen = append(seen, q)
		}
	}
	if len(seen) == 0 {
		return "that"
	}
	return strings.Join(seen, " and ")
}

// ---------------------------------------------------------------- calc

// Letters and commas are allowed for the function names in exprFuncs, and
// `=` and `;` for naming a part before using it. evalExpr is what decides
// which names and functions exist. This only keeps the obvious rubbish out
// before the parser sees it.
const exprChars = `[0-9a-z_\.\+\-\*/\(\)\s%,^;=]`

var safeExpr = regexp.MustCompile(`^` + exprChars + `+$`)

// The same class unanchored, so a rejection can name what it choked on.
var exprChar = regexp.MustCompile(exprChars)

// Long enough for a formula written out in named steps and short enough that
// writing one cannot use up a whole round's token budget.
const maxExprChars = 1000

var Calc = Tool{
	Name: "calc",
	Description: "Evaluate arithmetic. Use it rather than doing sums in your head, especially for totals and " +
		"budgets. Every total you are about to write down goes through here first, including the total row of a " +
		"table and any figure you call an average, and the number you print is the number it returned. Changing " +
		"one line of a table means totalling it again. pow, sqrt, abs, round, floor, ceil, min and max are " +
		"available. You can name a part before using it, one statement per line or separated by semicolons, and " +
		"the last line is the answer: `r = 0.05/12` then `275000 * r / (1 - pow(1 + r, -360))` gives the monthly " +
		"payment on a 275000 loan at 5% over 30 years, which is 1476.26. r is the monthly rate and n the number " +
		"of payments, so do not divide the result by 12 again. To total a long list of numbers, add them in " +
		"groups of about twenty and total the groups rather than writing every number into one call.",
	Schema: obj(map[string]any{"expression": str("arithmetic, like (1299 + 210) * 0.93, or named steps like r = 0.05/12; 275000 * r / (1 - pow(1 + r, -360))")}, "expression"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		e := strings.TrimSpace(argStr(a, "expression"))
		if e == "" {
			return nil, fmt.Errorf("there is nothing to work out, pass the arithmetic as expression")
		}
		// A model totalling a bank statement will write every line into one
		// expression and run out of tokens partway, so the call arrives cut in
		// half. Saying so is more use than evaluating whatever survived, and
		// which advice helps depends on whether it was summing or deriving.
		if len(e) > maxExprChars {
			if strings.Count(e, "+") > 20 {
				return nil, fmt.Errorf("that expression is too long at %d characters, add the numbers in groups of about twenty and total the groups", len(e))
			}
			return nil, fmt.Errorf("that expression is too long at %d characters, work it out in fewer steps or across more than one call", len(e))
		}
		// Naming the character is the difference between a model fixing this on
		// the next call and writing the same thing again, which is what a flat
		// "only arithmetic is supported" got.
		if !safeExpr.MatchString(e) {
			return nil, fmt.Errorf("calc cannot read %s in that expression. Numbers, + - * / %% ( ) and the "+
				"functions pow, sqrt, abs, round, floor, ceil, min and max are allowed, and a part can be named "+
				"with = on its own line", quoteChars(exprChar.ReplaceAllString(e, "")))
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
	Name: "now",
	Description: "The current date and time. Use it whenever the answer depends on what day it is. " +
		"Pass until for how long it is to a time or a date, and give the remaining it returns rather than working it out.",
	Schema: obj(map[string]any{
		"timezone": str("IANA name, default America/New_York"),
		"until":    str("a time or date to count down to, like \"10am\", \"friday 5pm\", \"October 4\" or \"christmas\""),
	}),
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
		out := map[string]any{
			"iso": t.Format(time.RFC3339), "readable": t.Format("Monday, 2 January 2006 at 3:04 PM MST"),
			"weekday": t.Format("Monday"), "timezone": name,
		}
		if u := argStr(a, "until"); u != "" {
			at, ok := ParseUntil(u, t)
			if !ok {
				return nil, fmt.Errorf("could not read %q as a time or a date, try something like \"10am\" or \"October 4 7pm\"", u)
			}
			out["until"] = at.Format("Monday, 2 January 2006 at 3:04 PM MST")
			out["remaining"] = Remaining(t, at)
		}
		return out, nil
	},
}
