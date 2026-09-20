package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// What the report is waiting on, source by source. A spinner says something is
// happening and this says what, who is being asked, and which answers were
// already on disk.
//
// It also says how much of this is cached, which is most of it, since none of
// these facts change between one Tuesday and the next.
type SourceState struct {
	Key    string // the lookups row this fact lands in
	Name   string // what it answers, in plain words
	Who    string // whose server is being asked
	Scope  string // how widely the cached answer is reused
	Done   bool
	Cached bool // it was already there before this address was checked
	Failed bool
	Note   string
}

// The precision each lookup's cache key is rounded to, which is what decides how
// widely an answer is reused. A flood zone is per parcel and what there is to do
// on a Saturday is per town, so they round differently and the page says so.
var sourcePlan = []struct {
	kind    string
	name    string
	who     string
	scope   string
	decimal int
}{
	{"parcel3", "The lot, and what it is assessed at", "NC OneMap, from the county assessor", "this parcel", 5},
	{"street", "Who owns the houses around it", "NC OneMap parcels", "this street", 4},
	{"floodv2", "Flood zone, creeks and rivers", "FEMA and the USGS", "this parcel", 5},
	{"roadv2", "The road out front, and the interstate", "OpenStreetMap and NCDOT", "this parcel", 5},
	{"terrain", "How flat the yard is", "USGS 3DEP elevation", "this parcel", 5},
	{"zones2", "Which schools it is zoned to", "the county schools GIS", "this parcel", 5},
	{"outings", "Places to go on a Saturday", "OpenStreetMap", "this area", 1},
	{"health", "Hospitals and nursing homes", "NC OneMap health", "this area", 3},
	{"acs", "Who lives around here", "US Census ACS", "this county", 0},
	{"crime", "Reported crime", "FBI Crime Data Explorer", "this county", 0},
	{"hpi", "What houses here have done since", "the FHFA house price index", "the whole search", 0},
}

// Progress reads the state of every source for one listing. Cheap enough to poll:
// it is one query over a small table plus the guard rows.
func Progress(ctx context.Context, db *sql.DB, lat, lon float64, since int64) ([]SourceState, error) {
	out := make([]SourceState, 0, len(sourcePlan))

	for _, p := range sourcePlan {
		s := SourceState{Key: p.kind, Name: p.name, Who: p.who, Scope: p.scope}

		// The county lookup is keyed by name rather than by coordinate, so it is
		// checked by kind alone.
		var fetched int64
		var err error
		if p.decimal == 0 {
			err = db.QueryRowContext(ctx,
				`SELECT MAX(fetched_at) FROM lookups WHERE kind = ?`, p.kind).Scan(&fetched)
		} else {
			key := fmt.Sprintf("%.*f,%.*f", p.decimal, lat, p.decimal, lon)
			err = db.QueryRowContext(ctx,
				`SELECT fetched_at FROM lookups WHERE kind = ? AND key = ?`, p.kind, key).Scan(&fetched)
		}
		if err == nil && fetched > 0 {
			s.Done = true
			// Written before this address was submitted, so it cost nothing.
			s.Cached = fetched < since
		}
		out = append(out, s)
	}

	// A source whose upstream is resting is not slow, it is waiting, and one that
	// has been refused outright is not waiting at all. Both were drawn the same
	// way as a source that simply had not answered yet.
	guards, err := guardStatuses(db)
	if err != nil {
		return out, nil
	}
	for _, g := range guards {
		for i := range out {
			if out[i].Done || !guardServes(g.Endpoint, out[i].Key) {
				continue
			}
			switch {
			case g.Open:
				out[i].Failed = true
				out[i].Note = "they asked us to slow down, back at " + g.OpenUntil.Format("15:04")
			case g.Failures > 0:
				out[i].Failed = true
				out[i].Note = "did not answer, trying again"
			}
		}
	}
	return out, nil
}

// Which guard feeds which fact, roughly, since it only drives a sentence on a
// progress panel.
func guardServes(endpoint, kind string) bool {
	switch kind {
	case "parcel3", "street":
		return endpoint == "nc-parcels"
	case "floodv2":
		return endpoint == "fema-nfhl" || endpoint == "usgs-nhd"
	case "roadv2", "outings":
		return len(endpoint) > 8 && endpoint[:8] == "overpass" || endpoint == "ncdot-aadt"
	case "terrain":
		return endpoint == "usgs-3dep"
	case "zones2":
		return len(endpoint) > 4 && endpoint[:4] == "gis-"
	case "health":
		return endpoint == "nc-health"
	case "acs":
		return endpoint == "acs-census"
	case "crime":
		return endpoint == "fbi-cde"
	case "hpi":
		return endpoint == "fhfa-hpi"
	}
	return false
}

// Summary is the one line above the list.
func SummariseProgress(states []SourceState) (done, cached, total int, when time.Duration) {
	for _, s := range states {
		total++
		if s.Done {
			done++
		}
		if s.Cached {
			cached++
		}
	}
	// Roughly ten seconds per outstanding source, which is the paced round trip
	// plus the wait in front of it.
	return done, cached, total, time.Duration(total-done) * 10 * time.Second
}
