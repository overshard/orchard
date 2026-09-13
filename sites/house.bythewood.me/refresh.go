package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// One command refreshes everything: pull listings from every adapter, place the
// ones with no coordinate, work out the facts for anything new or changed,
// rescore, and write it all down. Routing and every external lookup only runs
// for a listing that has never had one, because a road layout and a flood zone do
// not change between Tuesdays.
type Refresher struct {
	db    *sql.DB
	cfg   Config
	geo   *Geocoder
	flood *Flood
	roads *Roads
	schls *Schools
	terra *Terrain
	outng *Outings
	parcl *Parcels
	strt  *Street
	area  AreaTable
	acs   *Census
	crime *Crime
	hpi   *HPI
	route *Router
	facs  *Facilities
	alert *Alerter

	adapters []Adapter

	// Which listings have an assessment running right now. The report asks, so a
	// run that is still going does not look like a page that has finished badly.
	mu       sync.Mutex
	inflight map[int64]bool
}

// Running reports whether a background assessment is still going for a listing.
func (r *Refresher) Running(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inflight[id]
}

// claim marks a listing as being worked on and reports whether the caller got it.
// Checking and then setting left a window where a check from the page and the
// mender both started on the same listing, which doubled every external request
// and had whichever finished first tell the page it was done.
func (r *Refresher) claim(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		r.inflight = map[int64]bool{}
	}
	if r.inflight[id] {
		return false
	}
	r.inflight[id] = true
	return true
}

func (r *Refresher) release(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, id)
}

type RunResult struct {
	Seen     int
	Added    int
	Repriced int
	Scored   int
	Excluded int
	Errors   []string
}

func NewRefresher(db *sql.DB, cfg Config, dataDir string) *Refresher {
	geo := NewGeocoder(db)
	roads := NewRoads(db, cfg.Filters.RoadClasses, cfg.Filters.CornerFeet)
	parcels := NewParcels(db)
	area, err := LoadAreaTable(dataDir + "/area.json")
	if err != nil {
		// Not fatal. The two factors that read it say they have no figures, which
		// is the honest answer and is why they are their own factors.
		slog.Error("area table not loaded", slog.Any("err", err))
		area = AreaTable{}
	}
	return &Refresher{
		db:    db,
		cfg:   cfg,
		geo:   geo,
		flood: NewFlood(db, cfg.Filters.FloodZones),
		roads: roads,
		outng: NewOutings(db, roads),
		schls: NewSchools(db),
		terra: NewTerrain(db),
		parcl: parcels,
		strt:  NewStreet(db, parcels),
		area:  area,
		acs:   NewCensus(db),
		crime: NewCrime(db),
		hpi:   NewHPI(db),
		route: NewRouter(db),
		facs:  NewFacilities(db, geo),
		alert: NewAlerter(),
		// One adapter, and it reads files. Every route to a listing feed is either
		// licensed, paid, or behind a sign in, and the address box makes all of
		// them optional: a house is checked by pasting where it is.
		adapters: []Adapter{
			NewCSVAdapter(dataDir + "/import"),
		},
	}
}

// Run is the whole pipeline. It does not stop on a per-listing failure: one
// address the geocoder cannot place must not cost the other ninety-nine their
// refresh.
func (r *Refresher) Run(ctx context.Context) (RunResult, error) {
	var out RunResult
	now := time.Now()

	res, err := r.db.ExecContext(ctx, `INSERT INTO runs (started_at, source) VALUES (?, ?)`,
		now.Unix(), "refresh")
	if err != nil {
		return out, err
	}
	runID, _ := res.LastInsertId()

	var incoming []Listing
	for _, a := range r.adapters {
		rows, err := a.Fetch(ctx)
		if err != nil {
			out.Errors = append(out.Errors, a.Name()+": "+err.Error())
			continue
		}
		slog.Info("adapter read", slog.String("adapter", a.Name()), slog.Int("listings", len(rows)))
		incoming = append(incoming, rows...)
	}
	out.Seen = len(incoming)

	for _, l := range incoming {
		if l.Lat == 0 || l.Lon == 0 {
			full := strings.TrimSpace(fmt.Sprintf("%s, %s, %s %s", l.Address, l.City, l.State, l.Zip))
			lat, lon, _, err := r.geo.Geocode(ctx, full)
			if err != nil {
				out.Errors = append(out.Errors, "geocode "+l.Address+": "+err.Error())
			} else {
				l.Lat, l.Lon = lat, lon
			}
		}

		id, isNew, moved, err := Upsert(ctx, r.db, l, now)
		if err != nil {
			out.Errors = append(out.Errors, "store "+l.Address+": "+err.Error())
			continue
		}
		if isNew {
			out.Added++
		}
		if moved {
			out.Repriced++
		}

		assessed, err := r.assess(ctx, id, l)
		if err != nil {
			out.Errors = append(out.Errors, "assess "+l.Address+": "+err.Error())
			continue
		}
		out.Scored++
		if len(assessed.Excluded) > 0 {
			out.Excluded++
		}

		// The alert goes out only for a new listing that survived the filters and
		// is worth getting up for. A new listing that is excluded is a row in the
		// drawer, not a notification.
		if isNew && len(assessed.Excluded) == 0 {
			bd := Score(r.cfg, *assessed)
			if bd.Score >= newListingAlertScore {
				r.alert.NewListing(ctx, l, bd.Score, assessed.Monthly, assessed.Morning.DetourMin)
			}
		}
		if moved {
			if from, to, ok := lastPriceMove(ctx, r.db, id); ok && to < from {
				r.alert.PriceDrop(ctx, l, from, to)
			}
		}
	}

	// Anything in the database that this run did not see has come off the market,
	// or come out of a narrower export. Marked rather than deleted, because what a
	// house sold for and how long it took is the comparison for the next one.
	if _, err := r.db.ExecContext(ctx,
		`UPDATE listings SET gone_at = ? WHERE last_seen < ? AND gone_at IS NULL`,
		now.Unix(), now.Unix()); err != nil {
		out.Errors = append(out.Errors, "marking gone: "+err.Error())
	}

	note := ""
	if len(out.Errors) > 0 {
		note = strings.Join(out.Errors, "; ")
		if len(note) > 4000 {
			note = note[:4000]
		}
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE runs SET finished_at = ?, seen = ?, added = ?, changed = ?, note = ? WHERE id = ?`,
		time.Now().Unix(), out.Seen, out.Added, out.Repriced, note, runID); err != nil {
		return out, err
	}
	return out, nil
}

// A new listing has to clear this to earn a notification. Below it, it is still
// in the grid with a NEW badge and it is not worth a buzz.
const newListingAlertScore = 60

func lastPriceMove(ctx context.Context, db *sql.DB, id int64) (from, to int, ok bool) {
	rows, err := db.QueryContext(ctx,
		`SELECT price FROM price_history WHERE listing_id = ? ORDER BY seen_at DESC LIMIT 2`, id)
	if err != nil {
		return 0, 0, false
	}
	defer rows.Close()

	var prices []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return 0, 0, false
		}
		prices = append(prices, p)
	}
	if len(prices) < 2 {
		return 0, 0, false
	}
	return prices[1], prices[0], true
}

// CheckAddress is the front door. A pasted address and an asking price, and
// everything else comes from public records: the county for the lot and what it
// is assessed at, the census for the area, and the rest from the same services a
// listing would have gone through.
//
// It returns the id so the caller can send the reader straight to the report.
func (r *Refresher) CheckAddress(ctx context.Context, addr string, price int, extra Listing) (int64, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 0, fmt.Errorf("an address is needed")
	}

	lat, lon, matched, err := r.geo.Geocode(ctx, addr)
	if err != nil {
		// What comes back from the geocoder is for a log, not for somebody who has
		// just typed their first address into the box.
		slog.Info("geocode failed", slog.String("address", addr), slog.Any("err", err))
		return 0, fmt.Errorf("we could not find %s. Check the street, town and state, then try again", addr)
	}

	l := extra
	l.Source = "checked"
	l.Address = firstNonEmpty(l.Address, addr)
	l.Lat, l.Lon = lat, lon
	l.Price = price
	if l.Status == "" {
		l.Status = "Checked"
	}
	// The geocoder returns the address it actually matched, which is the tidied
	// form and is worth keeping over whatever was pasted.
	if matched != "" {
		l.Address, l.City, l.State, l.Zip = splitMatched(matched, l)
	}

	id, _, _, err := Upsert(ctx, r.db, l, time.Now())
	if err != nil {
		return 0, err
	}

	// The geocode is quick and everything after it is a dozen paced calls to other
	// people's servers, which took a minute and a half and then died on the write
	// timeout with somebody watching a blank tab. So the row goes in now, the
	// reader goes straight to the report, and the rest fills in behind them.
	if !r.claim(id) {
		// Already being worked on, and the page polls, so the reader sees the same
		// run finish either way.
		return id, nil
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		defer r.release(id)

		if _, err := r.assess(bg, id, l); err != nil {
			slog.Error("assessing an address failed", slog.String("address", addr), slog.Any("err", err))
		}
	}()

	return id, nil
}

// mustJSON is for a struct that is built here and cannot fail to marshal. An
// error would mean a field type changed, and an empty object is the right thing
// to store either way.
func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// splitMatched pulls the parts out of what the geocoder matched, which comes back
// as "118 MARCHBANK RD, TAYLORSVILLE, NC, 28681". Anything it cannot parse is left
// as it was rather than blanked.
func splitMatched(matched string, l Listing) (addr, city, state, zip string) {
	addr, city, state, zip = l.Address, l.City, l.State, l.Zip
	parts := strings.Split(matched, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) >= 4 {
		return titleAddress(parts[0]), titleAddress(parts[1]), strings.ToUpper(parts[2]), parts[3]
	}
	return addr, city, state, zip
}

// Directions stay in capitals and everything else is title cased. Length is not
// the test: "Rd" is two letters and is not a direction, which is how the first
// version of this produced "118 Marchbank RD".
var directions = map[string]bool{
	"n": true, "s": true, "e": true, "w": true,
	"ne": true, "nw": true, "se": true, "sw": true,
	// Spelled out too. The tax roll writes "358 WEST MAIN AVENUE" against a site
	// address of "358 MAIN AVE", and reading WEST as the street name said the
	// owner lived somewhere else.
	"north": true, "south": true, "east": true, "west": true,
	"northeast": true, "northwest": true, "southeast": true, "southwest": true,
}

// The geocoder shouts, and an address in capitals on a page meant to be pleasant
// to read is not.
func titleAddress(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		trimmed := strings.Trim(w, ".")
		// Only the abbreviations are shouted. A spelled out direction is a word
		// and reads as one.
		if directions[trimmed] && len(trimmed) <= 2 {
			words[i] = strings.ToUpper(w)
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// assess computes every fact for one listing and writes the facts row. The
// expensive parts come out of the caches in guard.go and the lookups table, so a
// second run over the same listing costs one SQLite read per fact and no network.
func (r *Refresher) assess(ctx context.Context, id int64, l Listing) (*Assessment, error) {
	a := &Assessment{Listing: l, Drives: map[string]Leg{}}
	g := r.cfg.Geography

	if l.Lat == 0 || l.Lon == 0 {
		a.Flags = Flags(r.cfg, a)
		a.Excluded = Excluded(a.Flags)
		a.Flood = newFloodResult()
		a.Road = newRoadResult()
		a.Monthly = r.cfg.Money.Estimate(l.Price, l.TaxAnnual, l.HOAMonthly, l.County, l.City, false)
		return a, r.writeFacts(ctx, id, a, Score(r.cfg, *a))
	}

	home := point{l.Lat, l.Lon}
	if r.cfg.HasOrigin() {
		a.Bearing = bearingDeg(g.OriginLat, g.OriginLon, l.Lat, l.Lon)
	}

	// Five different services with five separate guards, so they run at the same
	// time. Each still paces itself against its own upstream, and doing them one
	// after another made checking a single address take the best part of two
	// minutes while somebody stood there waiting for it.
	var wg sync.WaitGroup
	logFail := func(what string, err error) {
		if err != nil {
			slog.Info(what+" lookup failed", slog.String("address", l.Address), slog.Any("err", err))
		}
	}

	wg.Add(7)
	go func() {
		defer wg.Done()
		var err error
		if a.Flood, err = r.flood.Lookup(ctx, l.Lat, l.Lon); err != nil {
			a.Flood = newFloodResult()
			a.Flood.Partial = true
			logFail("flood", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if a.Road, err = r.roads.Lookup(ctx, l.Lat, l.Lon); err != nil {
			a.Road = newRoadResult()
			a.Road.Partial = true
			logFail("road", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if a.Zones, err = r.schls.Lookup(ctx, l.Lat, l.Lon, l.County); err != nil {
			logFail("zone", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if a.Terrain, err = r.terra.Lookup(ctx, l.Lat, l.Lon); err != nil {
			logFail("terrain", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		// What the county already knows, which is most of what would otherwise
		// have to be typed. A lot size off the parcel beats one off a listing,
		// since it is the deeded figure rather than an agent's rounding.
		if a.Parcel, err = r.parcl.Lookup(ctx, l.Lat, l.Lon, l.County, l.Address); err != nil {
			logFail("parcel", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if a.Outings, err = r.outng.Lookup(ctx, l.Lat, l.Lon); err != nil {
			logFail("outings", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if a.Street, err = r.strt.Lookup(ctx, l.Lat, l.Lon); err != nil {
			logFail("street", err)
		}
	}()
	wg.Wait()

	a.Fence = FenceNote(l)
	// The deeded acreage beats anything typed in, and it has to land on the
	// listing row rather than only in memory: acres lives there, the land factor
	// reads it back off the card, and without this the county's answer was fetched
	// and then thrown away.
	if a.Listing.Acres == 0 && a.Parcel.Acres > 0 {
		a.Listing.Acres = a.Parcel.Acres
		if _, err := r.db.ExecContext(ctx,
			`UPDATE listings SET acres = ? WHERE id = ? AND COALESCE(acres,0) = 0`,
			a.Parcel.Acres, id); err != nil {
			slog.Info("could not store the parcel acreage", slog.Any("err", err))
		}
	}

	// The county the parcel sits in beats the one on the address, which is often
	// blank: the geocoder returns a street address and no county at all, so an
	// address typed into the box has nothing to key the area figures on until the
	// parcel lookup says where it is.
	county := firstNonEmpty(a.Parcel.County, l.County, a.Zones.County)
	if county != "" && l.County == "" {
		if _, err := r.db.ExecContext(ctx, `UPDATE listings SET county = ? WHERE id = ?`, county, id); err != nil {
			slog.Info("could not store the county", slog.Any("err", err))
		}
		a.Listing.County = county
	}
	if profile, err := r.acs.County(ctx, "North Carolina", county); err == nil {
		a.Area = profile.Merge(r.area.For(county))
	} else {
		slog.Info("census lookup failed", slog.String("county", county), slog.Any("err", err))
		a.Area = r.area.For(county)
	}
	// Crime last, so it can use the population the census just returned and so a
	// hand-entered figure in area.json is not overwritten by one from the API.
	if a.Area.CrimeSource == "" {
		if fbi, err := r.crime.County(ctx, county, a.Area.Population); err != nil {
			slog.Info("crime lookup failed", slog.String("county", county), slog.Any("err", err))
		} else if fbi.Found {
			a.Area.ViolentPer1k = fbi.ViolentPer1k
			a.Area.PropertyPer1k = fbi.PropertyPer1k
			a.Area.CrimeSource = fbi.CrimeSource
			a.Area.CrimeYear = fbi.CrimeYear
			a.Area.Found = true
		}
	}

	// What the assessment is worth now. It needs the county for the index and the
	// parcel for the figure, so it sits after both.
	if a.Parcel.MarketValue > 0 {
		year := r.cfg.Money.RevaluationYear(county)
		if est, err := r.hpi.Estimate(ctx, county, a.Parcel.MarketValue, year); err != nil {
			slog.Info("value estimate failed", slog.String("county", county), slog.Any("err", err))
		} else {
			a.Value = est
		}
	}

	if r.cfg.HasOrigin() {
		work := point{g.OriginLat, g.OriginLon}
		elem := point{a.Zones.ElementaryLat, a.Zones.ElementaryLon}
		middle := point{a.Zones.MiddleLat, a.Zones.MiddleLon}
		high := point{a.Zones.HighLat, a.Zones.HighLon}
		a.Morning = r.route.Morning(ctx, home, work, elem, middle, high)
	}

	// Every drive is an independent route, so they go out together. The router's
	// own guard still paces them against the demo server, which is the thing that
	// has to be respected, rather than the order they happen to be asked in.
	type dest struct {
		key  string
		kind string
		at   point
	}
	var dests []dest
	for _, place := range r.cfg.Destinations {
		if place.Lat == 0 {
			continue
		}
		dests = append(dests, dest{place.Key, place.Kind, point{place.Lat, place.Lon}})
	}
	// The three nearest licensed nursing homes, which answers where that kind of work is
	// rather than the drive to one named employer.
	if near, err := r.facs.Nearest(ctx, l.Lat, l.Lon, 3); err == nil {
		for _, fc := range near {
			dests = append(dests, dest{"snf:" + fc.ID, "employer", point{fc.Lat, fc.Lon}})
		}
	} else {
		slog.Info("nowhere to work found", slog.String("address", l.Address), slog.Any("err", err))
	}

	var legs sync.Mutex
	var dwg sync.WaitGroup
	for _, d := range dests {
		dwg.Add(1)
		go func(d dest) {
			defer dwg.Done()
			leg, err := r.route.Route(ctx, home, d.at)
			if err != nil {
				return
			}
			leg.Kind = d.kind
			legs.Lock()
			a.Drives[d.key] = leg
			legs.Unlock()
		}(d)
	}
	dwg.Wait()

	a.Monthly = r.cfg.Money.Estimate(l.Price, l.TaxAnnual, l.HOAMonthly, l.County, l.City, false)
	a.Stretch = IsStretch(r.cfg, a)
	a.Flags = Flags(r.cfg, a)
	a.Excluded = Excluded(a.Flags)
	a.Grades = loadGrades(ctx, r.db, a.Zones)

	return a, r.writeFacts(ctx, id, a, Score(r.cfg, *a))
}

// loadGrades reads the NC DPI performance grades, which have to be dropped in as
// a file because DPI publishes them as a spreadsheet on a web page and not as an
// API. Absent, the schools factor falls back to whether the zoning is real.
func loadGrades(ctx context.Context, db *sql.DB, z SchoolZones) SchoolGrades {
	var g SchoolGrades
	var payload string
	if err := db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'grades' AND key = 'all'`).Scan(&payload); err != nil {
		return g
	}
	var table map[string]string
	if json.Unmarshal([]byte(payload), &table) != nil {
		return g
	}
	// A zone name is tidied down to a word or two, so an exact match is tried
	// first and a prefix match only when it picks out exactly one school. Range
	// order over a map is random, so returning the first prefix hit gave
	// "Taylorsville" whichever of the elementary, middle and high school came up,
	// and a different one on the next run.
	lookup := func(name string) string {
		if name == "" {
			return ""
		}
		for school, grade := range table {
			if strings.EqualFold(school, name) {
				return grade
			}
		}
		want := strings.ToLower(name)
		var grade string
		var hits int
		for school, g := range table {
			if strings.HasPrefix(strings.ToLower(school), want) {
				grade = g
				hits++
			}
		}
		if hits == 1 {
			return grade
		}
		return ""
	}
	g.Elementary, g.Middle, g.High = lookup(z.Elementary), lookup(z.Middle), lookup(z.High)
	g.Have = g.Elementary != "" || g.Middle != "" || g.High != ""
	return g
}

func (r *Refresher) writeFacts(ctx context.Context, id int64, a *Assessment, bd Breakdown) error {
	reasons, err := json.Marshal(a.Excluded)
	if err != nil {
		return err
	}
	breakdown, err := json.Marshal(bd)
	if err != nil {
		return err
	}

	dpa := r.cfg.Money.Estimate(a.Listing.Price, a.Listing.TaxAnnual, a.Listing.HOAMonthly,
		a.Listing.County, a.Listing.City, true)

	// Column and value together, because a hand-maintained column list, a row of
	// question marks and a list of arguments drift apart silently: a count that was
	// three out wrote nothing at all and the only sign was an empty facts table.
	fields := []struct {
		col string
		val any
	}{
		{"listing_id", id},
		{"computed_at", time.Now().Unix()},

		{"flood_zone", a.Flood.Zone},
		{"flood_sfha", a.Flood.SFHA},
		{"flood_subtype", a.Flood.Subtype},
		{"water_feet", a.Flood.WaterFeet},
		{"water_name", a.Flood.WaterName},
		{"water_kind", a.Flood.WaterKind},
		{"perennial_feet", a.Flood.PerennialFeet},
		{"perennial_name", a.Flood.PerennialName},
		{"ditch_feet", a.Flood.DitchFeet},

		{"market_value", a.Parcel.MarketValue},
		{"land_value", a.Parcel.LandValue},
		{"acres_from", a.Parcel.AcresFrom},
		{"parcel_address", a.Parcel.Address},
		{"value_json", mustJSON(a.Value)},
		{"parcel_lat", a.Parcel.Lat},
		{"parcel_lon", a.Parcel.Lon},
		{"zoning", a.Parcel.Use},
		{"area_json", mustJSON(a.Area)},

		{"relief_feet", a.Terrain.ReliefFeet},
		{"mean_slope_pct", a.Terrain.MeanSlopePct},
		{"flat_share", a.Terrain.FlatShare},
		{"fence", a.Fence},
		{"outings_json", mustJSON(a.Outings)},
		{"street_json", mustJSON(a.Street)},

		{"aadt", a.Road.AADT},
		{"aadt_route", a.Road.AADTRoute},
		{"aadt_feet", a.Road.AADTFeet},
		{"road_class", a.Road.Class},
		{"road_name", a.Road.RoadName},
		{"road_class_feet", a.Road.ClassFeet},
		{"corner", a.Road.Corner()},
		{"corner_roads", strings.Join(a.Road.NearbyRoads, ", ")},
		{"ramp_feet", a.Road.RampFeet},
		{"ramp_name", a.Road.RampName},
		{"highway_feet", a.Road.HighwayFeet},
		{"highway_name", a.Road.HighwayName},

		{"elem_name", a.Zones.Elementary},
		{"middle_name", a.Zones.Middle},
		{"high_name", a.Zones.High},
		{"zoning_source", a.Zones.Source},
		// Verified means the drop-off is measured to the school the child actually
		// goes to. A borrowed coordinate is not that, whatever the boundary said.
		{"zoning_verified", a.Zones.Verified && !a.Zones.Borrowed},
		{"zoning_nearest", a.Zones.Nearest},
		{"elem_miles", a.Zones.ElemMiles},
		{"middle_miles", a.Zones.MiddleMiles},
		{"high_miles", a.Zones.HighMiles},
		{"elem_grade", a.Grades.Elementary},
		{"middle_grade", a.Grades.Middle},
		{"high_grade", a.Grades.High},

		{"commute_min", a.Morning.Commute.Minutes},
		{"commute_miles", a.Morning.Commute.Miles},
		{"dropoff_min", a.Morning.WithElementary.Minutes},
		{"dropoff_miles", a.Morning.WithElementary.Miles},
		{"detour_min", a.Morning.DetourMin},
		{"middle_detour_min", a.Morning.MiddleDetourMin},
		{"high_detour_min", a.Morning.HighDetourMin},
		{"opposite_ways", a.Morning.OppositeWays},
		{"bearing", a.Bearing},

		{"monthly_total", a.Monthly.Total},
		{"monthly_pi", a.Monthly.PrincipalInt},
		{"monthly_tax", a.Monthly.Tax},
		{"monthly_ins", a.Monthly.Insurance},
		{"monthly_pmi", a.Monthly.PMI},
		{"monthly_hoa", a.Monthly.HOA},
		{"monthly_util", a.Monthly.Utilities + a.Monthly.Internet},
		{"monthly_dpa", dpa.Total},

		{"score", bd.Score},
		{"breakdown", string(breakdown)},
		{"excluded", len(a.Excluded) > 0},
		{"exclude_reasons", string(reasons)},
		{"penalty_total", bd.Docked},
		{"stretch", a.Stretch},
	}

	cols := make([]string, 0, len(fields))
	holes := make([]string, 0, len(fields))
	sets := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields))
	for _, f := range fields {
		cols = append(cols, f.col)
		holes = append(holes, "?")
		if f.col != "listing_id" {
			sets = append(sets, f.col+" = excluded."+f.col)
		}
		args = append(args, f.val)
	}

	query := "INSERT INTO facts (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.Join(holes, ",") + ") ON CONFLICT(listing_id) DO UPDATE SET " +
		strings.Join(sets, ", ")

	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("write facts: %w", err)
	}

	if _, err := r.db.ExecContext(ctx, `DELETE FROM drives WHERE listing_id = ?`, id); err != nil {
		return err
	}
	for key, leg := range a.Drives {
		if _, err := r.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO drives (listing_id, place_key, minutes, miles) VALUES (?,?,?,?)`,
			id, key, leg.Minutes, leg.Miles); err != nil {
			return err
		}
	}
	return nil
}
