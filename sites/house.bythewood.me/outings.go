package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Somewhere for a child to go on a Saturday, like a science centre, a cinema, a
// pool or a park with something in it, since being twenty minutes from the
// nearest anything costs you as well.
//
// OpenStreetMap has all of it tagged, and one query around the house at a radius
// worth driving covers the lot. `out center` rather than `out geom`, since what
// is wanted is where a building is and not its outline.
// An hour's drive, which on these roads is about forty miles. Wide enough to
// pick up Hickory and Statesville, where the science centre and cinemas are.
const outingsSearchFeet = 40 * 5280

// The kinds worth counting, in the order a child would rank them. Each is a tag
// match and a friendly name for the page, and each filter is another pass over
// the area, so these are kept to the ones that decide a day out.
//
// The water ones matter here, since the Catawba and the Yadkin are what people
// in these counties do in July.
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
	// The nearest of each kind that exists nearby, closest first.
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
	// Rounded coarsely, since what there is to do within an hour does not
	// change between one house and the next, or between one town and the next
	// either, so this is one query per rough area rather than one per address.
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

	// One bounding box rather than nine `around` scans at forty miles. Overpass
	// measures the distance to every candidate for an `around`, so nine of them at
	// that radius is enough work to ask a free service for that it answered 429 and
	// timed out. A box is cheap and the exact distance is worked out here anyway.
	//
	// The kinds are collapsed into four clauses by tag key for the same reason.
	south, west, north, east := boxAround(lat, lon, outingsSearchFeet)
	box := fmt.Sprintf("(%.4f,%.4f,%.4f,%.4f)", south, west, north, east)

	// nwr covers a point, a building outline and a relation in one pass, which
	// matters because a cinema is tagged all three ways depending on who mapped it.
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

	// The closest of each kind, so a town with six parks and no cinema does not
	// score as well as one with both.
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
		// The query asks for a box and the search is a circle, so a corner of the
		// box is half again as far as the radius and has to be dropped here.
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
	if _, err := o.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('outings',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// labelFor maps an element's tags back to the kind it matched. The union query
// does not say which arm returned a row, so it is worked out from the tags.
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

// Raw scores how much there is within reach and how close it is. Four different
// kinds inside a short drive is a childhood with something in it, and one park
// eighteen miles away is not.
func (r OutingsResult) Raw() float64 {
	if len(r.Nearest) == 0 {
		if r.Partial {
			return 0.5
		}
		return 0
	}

	// Each kind scores on how far away the nearest one is, and a kind with nothing
	// at all counts as zero rather than being left out, or a town with one close
	// park would score the same as one with a park, a cinema and a museum.
	var sum float64
	for _, o := range r.Nearest {
		// Close is better, and anything inside an hour still counts for something.
		sum += band(o.Miles, 5, 40)
	}
	return sum / float64(len(outingKinds))
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
