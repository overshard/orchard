package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// An hour's drive on these roads, wide enough to pick up Hickory and Statesville
// where the science centre and cinemas are.
const outingsSearchFeet = 40 * 5280

// In the order a child would rank them. The water ones matter since the Catawba
// and the Yadkin are what people in these counties do in July.
var outingKinds = []struct {
	filter string
	label  string
}{
	{`["tourism"="museum"]`, "museum or science centre"},
	{`["amenity"="cinema"]`, "cinema"},
	{`["leisure"="water_park"]`, "water park"},
	{`["leisure"="slipway"]`, "river or lake access"},
	{`["sport"="canoe"]`, "tubing or paddling"},
	{`["leisure"="nature_reserve"]["name"]`, "nature reserve"},
	{`["tourism"="zoo"]`, "zoo or farm park"},
	{`["leisure"="sports_centre"]`, "sports centre"},
	{`["leisure"="park"]["name"]["leisure"!="garden"]`, "park"},
}

type Outing struct {
	Label   string  `json:"label"`
	Name    string  `json:"name"`
	Minutes float64 `json:"-"`
	Miles   float64 `json:"miles"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type OutingsResult struct {
	// The nearest of each kind, closest first.
	Nearest []Outing `json:"nearest"`
	Partial bool     `json:"partial"`
}

type Outings struct {
	db    *sql.DB
	roads *Roads // shares the Overpass mirrors and their guards
}

func NewOutings(db *sql.DB, roads *Roads) *Outings {
	return &Outings{db: db, roads: roads}
}

func (o *Outings) Lookup(ctx context.Context, lat, lon float64) (OutingsResult, error) {
	// Rounded coarsely, since what there is to do within an hour doesn't change
	// between one town and the next.
	key := fmt.Sprintf("%.1f,%.1f", lat, lon)

	var payload string
	err := o.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'outings' AND key = ?`, key).Scan(&payload)
	if err == nil {
		var cached OutingsResult
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return OutingsResult{}, err
	}

	// A box in a few clauses instead of an `around` per kind, since Overpass
	// measures every candidate for an `around` and at forty miles that drew 429s
	// and timeouts.
	south, west, north, east := boxAround(lat, lon, outingsSearchFeet)
	box := fmt.Sprintf("(%.4f,%.4f,%.4f,%.4f)", south, west, north, east)

	// nwr since a cinema is a point, an outline or a relation depending on who
	// mapped it.
	q := fmt.Sprintf("[out:json][timeout:%d];(", int(overpassAttempt.Seconds())) +
		`nwr["tourism"~"^(museum|zoo)$"]` + box + ";" +
		`nwr["amenity"="cinema"]` + box + ";" +
		`nwr["sport"="canoe"]` + box + ";" +
		`nwr["leisure"~"^(water_park|slipway|nature_reserve|sports_centre|park)$"]["name"]` + box + ";" +
		");out center tags;"

	body, err := o.roads.overpassQuery(ctx, q)
	if err != nil {
		return OutingsResult{Partial: true}, err
	}

	var res struct {
		Elements []struct {
			Tags   map[string]string `json:"tags"`
			Lat    float64           `json:"lat"`
			Lon    float64           `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return OutingsResult{Partial: true}, err
	}

	// The closest of each kind, so six parks and no cinema doesn't read as well as
	// both.
	best := map[string]Outing{}
	for _, el := range res.Elements {
		elat, elon := el.Lat, el.Lon
		if el.Center != nil {
			elat, elon = el.Center.Lat, el.Center.Lon
		}
		if elat == 0 && elon == 0 {
			continue
		}
		label := labelFor(el.Tags)
		if label == "" {
			continue
		}
		miles := haversineFeet(lat, lon, elat, elon) / 5280
		// The box's corners are further out than the radius.
		if miles > outingsSearchFeet/5280 {
			continue
		}
		if have, ok := best[label]; ok && have.Miles <= miles {
			continue
		}
		best[label] = Outing{
			Label: label,
			Name:  firstNonEmpty(el.Tags["name"], label),
			Miles: miles,
			Lat:   elat,
			Lon:   elon,
		}
	}

	out := OutingsResult{}
	for _, k := range outingKinds {
		if o, ok := best[k.label]; ok {
			out.Nearest = append(out.Nearest, o)
		}
	}
	sortOutings(out.Nearest)

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := keep(ctx, o.db,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('outings',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// labelFor exists since a union query doesn't say which arm returned a row.
func labelFor(tags map[string]string) string {
	switch {
	case tags["tourism"] == "museum":
		return "museum or science centre"
	case tags["amenity"] == "cinema":
		return "cinema"
	case tags["leisure"] == "water_park":
		return "water park"
	case tags["leisure"] == "slipway":
		return "river or lake access"
	case tags["sport"] == "canoe":
		return "tubing or paddling"
	case tags["leisure"] == "nature_reserve":
		return "nature reserve"
	case tags["tourism"] == "zoo":
		return "zoo or farm park"
	case tags["leisure"] == "sports_centre":
		return "sports centre"
	case tags["leisure"] == "park":
		return "park"
	}
	return ""
}

func sortOutings(o []Outing) {
	for i := 1; i < len(o); i++ {
		for j := i; j > 0 && o[j].Miles < o[j-1].Miles; j-- {
			o[j], o[j-1] = o[j-1], o[j]
		}
	}
}

func (r OutingsResult) Why() string {
	if len(r.Nearest) == 0 {
		if r.Partial {
			return "could not look this up just now"
		}
		return "nothing much within an hour"
	}
	first := r.Nearest[0]
	return fmt.Sprintf("%d kinds of outing within an hour, closest is %s about %s miles away",
		len(r.Nearest), first.Name, trimFloat(math.Round(first.Miles*10)/10))
}
