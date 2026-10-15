package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var stateNames = map[string]string{
	"nc": "North Carolina", "sc": "South Carolina", "va": "Virginia", "tn": "Tennessee",
	"ga": "Georgia", "ny": "New York", "ca": "California", "tx": "Texas", "fl": "Florida",
	"wv": "West Virginia", "ky": "Kentucky", "oh": "Ohio", "pa": "Pennsylvania", "md": "Maryland",
}

var trailingState = regexp.MustCompile(`^(.*?)[,\s]+([A-Za-z]{2})$`)

// geocode tries the obvious rewrites before giving up, since Open-Meteo wants a
// clean place name and "Yadkin Valley NC" misses where "Yadkin Valley, North
// Carolina" hits.
func geocode(ctx context.Context, d *Deps, place string) (g geo, err error) {
	cands := []string{place}
	if m := trailingState.FindStringSubmatch(place); m != nil {
		if full, ok := stateNames[strings.ToLower(m[2])]; ok {
			cands = append(cands, m[1]+", "+full, strings.TrimSpace(m[1]))
		}
	}
	if i := strings.Index(place, ","); i > 0 {
		cands = append(cands, strings.TrimSpace(place[:i]))
	}
	seen := map[string]bool{}
	for _, c := range cands {
		c = strings.TrimSpace(c)
		if c == "" || seen[strings.ToLower(c)] {
			continue
		}
		seen[strings.ToLower(c)] = true
		var out struct {
			Results []struct {
				Latitude  float64  `json:"latitude"`
				Longitude float64  `json:"longitude"`
				Name      string   `json:"name"`
				Admin1    string   `json:"admin1"`
				Country   string   `json:"country_code"`
				Postcodes []string `json:"postcodes"`
			} `json:"results"`
		}
		u := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=en&name=" + url.QueryEscape(c)
		if e := getJSON(ctx, d, u, &out); e != nil {
			err = e
			continue
		}
		if len(out.Results) > 0 {
			r := out.Results[0]
			g = geo{Lat: r.Latitude, Lon: r.Longitude,
				Name:    strings.TrimSuffix(r.Name+", "+r.Admin1, ", "),
				Country: strings.ToUpper(r.Country)}
			if len(r.Postcodes) > 0 {
				g.Zip = r.Postcodes[0]
			}
			return g, nil
		}
	}
	// Open-Meteo only knows towns, so a gorge, a park or a trailhead would answer
	// for the nearest town down in the valley.
	if g, ok := nominatim(ctx, d, place); ok {
		return g, nil
	}
	return geo{}, fmt.Errorf("could not find a place called %q", place)
}

func nominatim(ctx context.Context, d *Deps, place string) (geo, bool) {
	var out []struct {
		Lat     string `json:"lat"`
		Lon     string `json:"lon"`
		Name    string `json:"name"`
		Display string `json:"display_name"`
	}
	u := "https://nominatim.openstreetmap.org/search?format=jsonv2&limit=1&countrycodes=us&q=" + url.QueryEscape(place)
	// Their policy asks for an agent that names the application, not a browser's.
	if err := getJSONHeaders(ctx, d, u, map[string]string{"User-Agent": "chat.bythewood.me"}, &out); err != nil || len(out) == 0 {
		return geo{}, false
	}
	lat, err1 := strconv.ParseFloat(out[0].Lat, 64)
	lon, err2 := strconv.ParseFloat(out[0].Lon, 64)
	if err1 != nil || err2 != nil {
		return geo{}, false
	}
	name := out[0].Name
	if parts := strings.Split(out[0].Display, ", "); len(parts) >= 2 {
		name = strings.Join(parts[:2], ", ")
	}
	return geo{Lat: lat, Lon: lon, Name: name, Country: "US"}, true
}

// The postcode is carried because pollen.com is keyed on a US zip and this is the
// only call that ever knows one, and the country says whether asking for pollen
// is worth a request at all.
type geo struct {
	Lat, Lon float64
	Name     string
	Zip      string
	Country  string
}

type weatherArgs struct {
	Location string
	Days     int
}

func (a *weatherArgs) params() []param {
	return []param{
		strArg(&a.Location, "location", "a town, city or region, like \"Boone, NC\"").required(),
		intArg(&a.Days, "days", "how many days ahead, 1 to 14, default 7").otherwise(7),
	}
}

var Weather = Define(Tool{
	Name:        "weather",
	Description: "Daily forecast for a place, up to 14 days out, in Fahrenheit and mph.",
}, func(ctx context.Context, d *Deps, a weatherArgs) (any, error) {
	g, err := geocode(ctx, d, a.Location)
	if err != nil {
		return nil, err
	}
	lat, lon, name := g.Lat, g.Lon, g.Name
	d.Widgets.Add(Widget{Kind: "weather", Place: name, Lat: lat, Lon: lon,
		Zip: g.Zip, Country: g.Country})
	days := a.Days
	if days < 1 || days > 14 {
		days = 7
	}
	// Asked for one day about tomorrow it got today and called it tomorrow.
	days = max(days, 3)
	var w struct {
		Offset int `json:"utc_offset_seconds"`
		Daily  struct {
			Time    []string  `json:"time"`
			Max     []float64 `json:"temperature_2m_max"`
			Min     []float64 `json:"temperature_2m_min"`
			Precip  []float64 `json:"precipitation_probability_max"`
			Wind    []float64 `json:"wind_speed_10m_max"`
			Sunrise []string  `json:"sunrise"`
			Sunset  []string  `json:"sunset"`
		} `json:"daily"`
	}
	u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f"+
		"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,wind_speed_10m_max,sunrise,sunset"+
		"&temperature_unit=fahrenheit&wind_speed_unit=mph&timezone=auto&forecast_days=%d", lat, lon, days)
	if err := getJSON(ctx, d, u, &w); err != nil {
		return nil, err
	}
	type day struct {
		Date      string  `json:"date"`
		Weekday   string  `json:"weekday"`
		When      string  `json:"when,omitempty"`
		HighF     float64 `json:"high_f"`
		LowF      float64 `json:"low_f"`
		PrecipPct float64 `json:"precip_chance_pct"`
		WindMPH   float64 `json:"wind_mph"`
		Sunset    string  `json:"sunset,omitempty"`
	}
	out := make([]day, 0, len(w.Daily.Time))
	today := d.Now().UTC().Add(time.Duration(w.Offset) * time.Second).Format("2006-01-02")
	for i := range w.Daily.Time {
		wd := ""
		if t, e := time.Parse("2006-01-02", w.Daily.Time[i]); e == nil {
			wd = t.Format("Monday")
		}
		dd := day{Date: w.Daily.Time[i], Weekday: wd, When: relativeTo(today, w.Daily.Time[i]), HighF: w.Daily.Max[i],
			LowF: w.Daily.Min[i], PrecipPct: w.Daily.Precip[i], WindMPH: w.Daily.Wind[i]}
		if i < len(w.Daily.Sunset) {
			if t, e := time.Parse("2006-01-02T15:04", w.Daily.Sunset[i]); e == nil {
				dd.Sunset = t.Format("3:04 PM")
			}
		}
		out = append(out, dd)
	}
	return map[string]any{"place": name, "units": "fahrenheit, mph", "days": out}, nil
})

// relativeTo names a date against today, both as the place's own YYYY-MM-DD, so
// the model reads which row is tomorrow instead of working it out.
func relativeTo(today, date string) string {
	t, err1 := time.Parse("2006-01-02", today)
	d, err2 := time.Parse("2006-01-02", date)
	if err1 != nil || err2 != nil {
		return ""
	}
	switch int(d.Sub(t).Hours() / 24) {
	case -1:
		return "yesterday"
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return ""
}
