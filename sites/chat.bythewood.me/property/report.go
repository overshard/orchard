package property

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

// A whole house, answered. Everything below comes from a public record and every
// piece of it is cached, so asking a second question about the same address costs
// one read and no requests to anybody.

// Report is what one address is worth knowing. Every field is a measurement or a
// stated absence, never a guess, because the thing this replaced learned twice
// that a zero reads as a claim.
type Report struct {
	Address  string  `json:"address"`
	Asked    string  `json:"asked_as,omitempty"`
	City     string  `json:"city,omitempty"`
	State    string  `json:"state,omitempty"`
	Zip      string  `json:"zip,omitempty"`
	County   string  `json:"county,omitempty"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Price    int     `json:"price,omitempty"`
	BuiltAt  string  `json:"built_at"`
	Complete bool    `json:"complete"`

	// A cached report that gains a field decodes with that field blank, which is
	// the same trap the lookup kinds carry a number for. Bump this whenever the
	// report gains meaning, so an older row is rebuilt rather than answered from.
	Version int `json:"v"`

	Flood   FloodResult   `json:"flood"`
	Road    RoadResult    `json:"road"`
	Zones   SchoolZones   `json:"schools"`
	Terrain TerrainResult `json:"terrain"`
	Parcel  ParcelResult  `json:"parcel"`
	Street  StreetResult  `json:"street"`
	Area    AreaProfile   `json:"area"`
	Value   ValueEstimate `json:"value"`
	Outings OutingsResult `json:"outings"`
	USDA    USDAArea      `json:"usda_area"`

	Morning   Morning        `json:"morning"`
	Drives    map[string]Leg `json:"drives"`
	Work      []Facility     `json:"nearby_work,omitempty"`
	Household []Person       `json:"household,omitempty"`

	Market Market  `json:"rates"`
	Quotes []Quote `json:"loans"`

	// What he said he wanted to spend. Without it in the answer the model
	// invented a range and reported being inside it, which is the worst kind of
	// wrong: specific, plausible and about his own money.
	MonthlyTarget  float64 `json:"monthly_target,omitempty"`
	MonthlyCeiling float64 `json:"monthly_ceiling,omitempty"`

	// What could not be reached this time, named rather than left as a blank a
	// reader would take for a measurement of nothing, and which upstreams have
	// asked us to slow down, which is usually the reason.
	Missing []string `json:"still_missing,omitempty"`
	Resting []string `json:"upstreams_resting,omitempty"`

	// Which config the figures came from, so a report built on defaults says so
	// rather than passing placeholder numbers off as his.
	ConfigLabel string `json:"config"`
}

// Engine holds every lookup and the database they cache into. One per process.
type Engine struct {
	db  *sql.DB
	cfg Config

	geo   *Geocoder
	flood *Flood
	roads *Roads
	schls *Schools
	terra *Terrain
	parcl *Parcels
	strt  *Street
	acs   *Census
	crime *Crime
	hpi   *HPI
	route *Router
	facs  *Facilities
	outng *Outings
	usda  *USDA
	rates *Rates

	// One assessment per address at a time. Two questions about the same house in
	// the same turn would otherwise double every external request, which is the
	// thing the whole cache exists to avoid.
	mu       sync.Mutex
	inflight map[string]chan struct{}

	// Held across the read and the write of the cold ceiling, so two turns asking
	// about two new addresses at once cannot both see the last slot.
	spendMu sync.Mutex
}

func NewEngine(db *sql.DB, cfg Config) (*Engine, error) {
	if err := Migrate(db); err != nil {
		return nil, err
	}
	roads := NewRoads(db, cfg.Thresholds.RoadClasses, cfg.Thresholds.CornerFeet)
	parcels := NewParcels(db)
	geo := NewGeocoder(db)
	return &Engine{
		db: db, cfg: cfg,
		geo:   geo,
		flood: NewFlood(db, cfg.Thresholds.FloodZones),
		roads: roads,
		schls: NewSchools(db),
		terra: NewTerrain(db),
		parcl: parcels,
		strt:  NewStreet(db, parcels),
		acs:   NewCensus(db),
		crime: NewCrime(db),
		hpi:   NewHPI(db),
		route: NewRouter(db),
		facs:  NewFacilities(db, geo),
		outng: NewOutings(db, roads),
		usda:  NewUSDA(db),
		rates: NewRates(db),

		inflight: map[string]chan struct{}{},
	}, nil
}

func (e *Engine) Config() Config { return e.cfg }

// Full is the address as somebody would write it down, which is what the
// geocoder needs and what anything quoting a report back has to use.
func (r *Report) Full() string {
	if r.Address == "" {
		return ""
	}
	out := r.Address
	for _, part := range []string{r.City, r.State, r.Zip} {
		if strings.TrimSpace(part) != "" {
			out += ", " + part
		}
	}
	return out
}

// A cached report is good for a month. Nothing in it moves faster than that
// except the rate, which is looked up separately and is not cached with the
// house, and the price, which comes from whoever is asking.
const reportTTL = 30 * 24 * time.Hour

// 2 when the drives learned whose they are and the household roster arrived. A
// report written before that answers a question naming somebody with a flat list
// of place names, which is what sent the model to Wikipedia looking for her.
const reportVersion = 2

// Options are what the caller knew that the public record does not.
type Options struct {
	Price      int
	HOAMonthly float64
	TaxAnnual  float64
	Remarks    string

	// How long to wait for a cold report before answering with what is done. A
	// full assessment is a dozen paced calls to other people's servers and runs
	// past a minute, and somebody in a conversation will not sit through that, so
	// the rest finishes in the background and lands in the cache for the next
	// question.
	Wait time.Duration

	// Refresh throws away a cached report and builds it again. It is not offered
	// to the model: pressing a thing again when it looks wrong is how a home
	// address gets blocked by somebody's free service.
	Refresh bool
}

// Lookup is the front door. An address and whatever else is known, and a report
// back, cached or fresh or half built.
func (e *Engine) Lookup(ctx context.Context, address string, opt Options) (*Report, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("an address is needed, street and town at least")
	}

	// Before the geocoder, because a house already looked up is the same house
	// whether or not the Census is answering and whether or not the street line
	// arrived with its town on it. A follow up went out with the street alone,
	// failed to place, and the model started bolting invented towns onto it.
	if !opt.Refresh {
		if r, ok := e.cachedLike(ctx, address); ok {
			e.priceReport(ctx, r, opt)
			return r, nil
		}
	}

	lat, lon, matched, err := e.geo.Geocode(ctx, address)
	if err != nil {
		// What the geocoder says is for a log. Whoever asked gets something they
		// can act on.
		slog.Info("property geocode failed", "address", address, "err", err)
		return nil, fmt.Errorf(
			"could not place %s. Give the street, the town and the state, and do not guess at a "+
				"different town: a wrong one is a different house and costs another lookup", address)
	}

	key := reportKey(address, matched)
	if !opt.Refresh {
		if r, ok := e.cached(ctx, key); ok {
			// The price and the loans are the caller's, not the cache's, so they
			// are worked out fresh over the cached facts every time.
			e.priceReport(ctx, r, opt)
			return r, nil
		}
	}

	// Nothing above this line leaves the machine twice for the same address, and
	// everything below it is about thirty eight requests to eleven other people's
	// servers, so the ceiling goes here: after the cache, before the work.
	if err := e.spend(ctx, key); err != nil {
		return nil, err
	}

	done := e.start(ctx, key, address, matched, lat, lon, opt)

	wait := opt.Wait
	if wait <= 0 {
		wait = 75 * time.Second
	}
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(wait):
	}

	r, ok := e.cached(ctx, key)
	if !ok {
		// Still building and nothing written yet, so answer with the half that
		// needs no lookups rather than with an error.
		r = &Report{Address: titleAddress(matched), Asked: address, Lat: lat, Lon: lon,
			BuiltAt: time.Now().UTC().Format(time.RFC3339), ConfigLabel: e.cfg.Label,
			Version: reportVersion}
		r.Missing = append(r.Missing, "the area work is still running, ask again in a minute and it will be ready")
	}
	e.priceReport(ctx, r, opt)
	return r, nil
}

// start runs one assessment in the background and hands back a channel that
// closes when it is written. Backgrounded rather than run inline so a caller that
// gives up waiting still leaves a finished report in the cache for the next
// question, which is what makes a follow up free.
func (e *Engine) start(ctx context.Context, key, address, matched string, lat, lon float64, opt Options) chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ch, running := e.inflight[key]; running {
		return ch
	}
	ch := make(chan struct{})
	e.inflight[key] = ch

	go func() {
		// Its own context, since the whole point is that it outlives the turn
		// that asked. Twenty minutes is the worst case with every guard resting.
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Minute)
		defer cancel()
		defer func() {
			e.mu.Lock()
			delete(e.inflight, key)
			e.mu.Unlock()
			close(ch)
		}()

		r := e.assess(bg, address, matched, lat, lon, opt)
		if err := e.save(bg, key, r); err != nil {
			slog.Error("could not cache a property report", "address", address, "err", err)
		}
	}()
	return ch
}

// assess is every lookup for one point. The independent ones run at once, each
// still paced against its own upstream by its own guard, because one after
// another took the best part of two minutes for one address.
func (e *Engine) assess(ctx context.Context, address, matched string, lat, lon float64, opt Options) *Report {
	r := &Report{
		Address:     titleAddress(matched),
		Asked:       address,
		Lat:         lat,
		Lon:         lon,
		Drives:      map[string]Leg{},
		BuiltAt:     time.Now().UTC().Format(time.RFC3339),
		ConfigLabel: e.cfg.Label,
		Version:     reportVersion,
	}
	r.Address, r.City, r.State, r.Zip = splitMatched(matched, r.Address)

	var missing sync.Mutex
	note := func(what string, err error) {
		if err == nil {
			return
		}
		slog.Info("property lookup failed", "what", what, "address", address, "err", err)
		missing.Lock()
		r.Missing = append(r.Missing, what)
		missing.Unlock()
	}

	var wg sync.WaitGroup
	run := func(what string, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			note(what, f())
		}()
	}

	run("flood zone and water", func() error {
		v, err := e.flood.Lookup(ctx, lat, lon)
		if err != nil {
			v = newFloodResult()
			v.Partial = true
		}
		r.Flood = v
		return err
	})
	run("the road it fronts", func() error {
		v, err := e.roads.Lookup(ctx, lat, lon)
		if err != nil {
			v = newRoadResult()
			v.Partial = true
		}
		r.Road = v
		return err
	})
	run("school zoning", func() error {
		v, err := e.schls.Lookup(ctx, lat, lon, "")
		r.Zones = v
		return err
	})
	run("how flat the lot is", func() error {
		v, err := e.terra.Lookup(ctx, lat, lon)
		r.Terrain = v
		return err
	})
	run("the county parcel record", func() error {
		v, err := e.parcl.Lookup(ctx, lat, lon, "", address)
		r.Parcel = v
		return err
	})
	run("who owns the street", func() error {
		v, err := e.strt.Lookup(ctx, lat, lon)
		r.Street = v
		return err
	})
	run("places to go nearby", func() error {
		v, err := e.outng.Lookup(ctx, lat, lon)
		r.Outings = v
		return err
	})
	run("the USDA rural area map", func() error {
		v, err := e.usda.Lookup(ctx, lat, lon)
		r.USDA = v
		return err
	})
	wg.Wait()

	// The county the parcel sits in beats the one on the address, which the
	// geocoder does not return at all, and the area figures have nothing to key on
	// until this lands.
	r.County = firstNonEmpty(r.Parcel.County, r.Zones.County)

	if profile, err := e.acs.County(ctx, "North Carolina", r.County); err == nil {
		r.Area = profile
	} else {
		note("the census figures for the county", err)
	}
	// Crime after the census, so it can use the population that just came back.
	if r.Area.CrimeSource == "" {
		if fbi, err := e.crime.County(ctx, r.County, r.Area.Population); err != nil {
			note("reported crime", err)
		} else if fbi.Found {
			r.Area.ViolentPer1k = fbi.ViolentPer1k
			r.Area.PropertyPer1k = fbi.PropertyPer1k
			r.Area.CrimeSource = fbi.CrimeSource
			r.Area.CrimeYear = fbi.CrimeYear
			r.Area.Found = true
		}
	}

	if r.Parcel.MarketValue > 0 {
		year := e.cfg.Money.RevaluationYear(r.County)
		if est, err := e.hpi.Estimate(ctx, r.County, r.Parcel.MarketValue, year); err != nil {
			note("what the assessment is worth now", err)
		} else {
			r.Value = est
		}
	}

	e.drives(ctx, r, note)

	// A gap is worth nothing without the reason for it. "no schools found" and
	// "the schools server is refusing us this hour" read the same in a report and
	// are not the same fact.
	r.Resting = Resting(e.db)
	r.Complete = len(r.Missing) == 0
	return r
}

// drives is the morning run and every configured destination. The detour is one
// route through the school rather than two legs added up, because a stop changes
// which way the trip leaves the house.
func (e *Engine) drives(ctx context.Context, r *Report, note func(string, error)) {
	home := point{r.Lat, r.Lon}

	if e.cfg.HasOrigin() {
		g := e.cfg.Geography
		work := point{g.OriginLat, g.OriginLon}
		r.Morning = e.route.Morning(ctx,
			home, work,
			point{r.Zones.ElementaryLat, r.Zones.ElementaryLon},
			point{r.Zones.MiddleLat, r.Zones.MiddleLon},
			point{r.Zones.HighLat, r.Zones.HighLon})
		if r.Morning.Partial {
			note("part of the morning drive", fmt.Errorf("a leg would not route"))
		}
	}

	r.Household = e.cfg.People

	type dest struct {
		key  string
		kind string
		who  string
		name string
		at   point
	}
	var dests []dest
	for _, place := range e.cfg.Destinations {
		if place.Lat == 0 {
			continue
		}
		name := firstNonEmpty(place.Name, place.Key)
		dests = append(dests, dest{name, place.Kind, place.Who, name, point{place.Lat, place.Lon}})
	}
	// The three nearest licensed nursing homes, which answers where that kind of
	// work is rather than the drive to one named employer.
	if near, err := e.facs.Nearest(ctx, r.Lat, r.Lon, 3); err == nil {
		r.Work = near
		for _, fc := range near {
			dests = append(dests, dest{fc.Name + ", " + fc.Label, "employer", "", fc.Name + ", " + fc.Label, point{fc.Lat, fc.Lon}})
		}
	} else {
		note("where the nursing work is", err)
	}

	var legs sync.Mutex
	var wg sync.WaitGroup
	for _, d := range dests {
		wg.Add(1)
		go func(d dest) {
			defer wg.Done()
			leg, err := e.route.Route(ctx, home, d.at)
			if err != nil {
				return
			}
			leg.Kind, leg.Who, leg.Name = d.kind, d.who, d.name
			legs.Lock()
			r.Drives[d.key] = leg
			legs.Unlock()
		}(d)
	}
	wg.Wait()
}

// priceReport works the money out over whatever facts the report has. It is
// separate from the assessment because the price is the caller's and changes
// between two questions about the same house, and none of it costs a request
// beyond the weekly rate survey.
func (e *Engine) priceReport(ctx context.Context, r *Report, opt Options) {
	if opt.Price > 0 {
		r.Price = opt.Price
	}
	if r.Price <= 0 {
		return
	}

	market, err := e.rates.Current(ctx)
	if err != nil {
		slog.Info("no mortgage rate survey", "err", err)
	}
	r.Market = market
	r.MonthlyTarget = e.cfg.Money.MonthlyTarget
	r.MonthlyCeiling = e.cfg.Money.MonthlyCeiling

	r.Quotes = e.cfg.Quotes(LoanInput{
		Price:           r.Price,
		County:          r.County,
		City:            r.City,
		TaxAnnual:       opt.TaxAnnual,
		HOAMonthly:      opt.HOAMonthly,
		USDAArea:        r.USDA.Eligible,
		USDAAreaChecked: r.USDA.Measured,
	}, market)
}

// Quote prices one house under one programme without touching a lookup, for the
// question that is only about money.
func (e *Engine) Quote(ctx context.Context, in LoanInput) ([]Quote, Market, error) {
	market, err := e.rates.Current(ctx)
	if err != nil && !market.Found {
		return nil, market, err
	}
	return e.cfg.Quotes(in, market), market, nil
}

func reportKey(asked, matched string) string {
	if strings.TrimSpace(matched) != "" {
		return addressKey(matched, "")
	}
	return addressKey(asked, "")
}

// cachedLike finds a report for an address written any of the ways somebody
// might write it. The stored key is built from the full matched address, so a
// street line on its own does not equal it and has to be matched as a prefix.
func (e *Engine) cachedLike(ctx context.Context, address string) (*Report, bool) {
	key := addressKey(address, "")
	if r, ok := e.cached(ctx, key); ok {
		return r, true
	}
	// The street line alone, against the front of a stored key. Anchored, so
	// "1 Oak" cannot match "21 Oak".
	bare := strings.TrimSuffix(key, "|")
	if len(bare) < 8 {
		return nil, false
	}
	var stored string
	err := e.db.QueryRowContext(ctx,
		`SELECT key FROM reports WHERE key LIKE ? ORDER BY built_at DESC LIMIT 1`, bare+"%").Scan(&stored)
	if err != nil {
		return nil, false
	}
	return e.cached(ctx, stored)
}

func (e *Engine) cached(ctx context.Context, key string) (*Report, bool) {
	var payload string
	var built int64
	err := e.db.QueryRowContext(ctx,
		`SELECT payload, built_at FROM reports WHERE key = ?`, key).Scan(&payload, &built)
	if err != nil {
		return nil, false
	}
	if time.Since(time.Unix(built, 0)) > reportTTL {
		return nil, false
	}
	var r Report
	if json.Unmarshal([]byte(payload), &r) != nil {
		return nil, false
	}
	if r.Version != reportVersion {
		return nil, false
	}
	return &r, true
}

func (e *Engine) save(ctx context.Context, key string, r *Report) error {
	// Stamped here rather than wherever a report is built, so a new construction
	// site cannot forget it and quietly write a row nothing will ever read back.
	r.Version = reportVersion
	_, err := e.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO reports (key, address, lat, lon, payload, built_at) VALUES (?,?,?,?,?,?)`,
		key, r.Address, r.Lat, r.Lon, mustJSON(r), time.Now().Unix())
	return err
}

// splitMatched pulls the parts out of what the geocoder matched, which comes back
// as "402 SAMPLE RD, ANYTOWN, NC, 27055". Anything it cannot parse is
// left as it was rather than blanked.
func splitMatched(matched, fallback string) (addr, city, state, zip string) {
	parts := strings.Split(matched, ",")
	if len(parts) < 4 {
		return fallback, "", "", ""
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	n := len(parts)
	return titleAddress(strings.Join(parts[:n-3], ", ")),
		titleAddress(parts[n-3]),
		strings.ToUpper(parts[n-2]),
		parts[n-1]
}
