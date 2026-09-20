package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Finishing reports that came up short, without anybody pressing anything.
//
// A lookup fails here because somebody else's server had a bad minute or our own
// guard is resting after one, and both fix themselves given time. A button is
// also what gets a home user blocked, since the instinct when a page looks wrong
// is to press it again.
type Mender struct {
	db    *sql.DB
	ref   *Refresher
	every time.Duration

	// How long after a report was built we keep trying to fill its gaps. Past
	// this, a missing fact is one the county does not publish rather than one that
	// failed, and asking again forever is rude.
	window time.Duration
}

func NewMender(db *sql.DB, ref *Refresher) *Mender {
	return &Mender{db: db, ref: ref, every: 4 * time.Minute, window: 7 * 24 * time.Hour}
}

// Run ticks quietly for the life of the process. One listing per tick at most, so
// a database full of half-finished reports is worked through slowly rather than
// all at once, which would be the same burst of traffic the guards exist to avoid.
func (m *Mender) Run(ctx context.Context) {
	t := time.NewTicker(m.every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if id, ok := m.next(ctx); ok {
				m.mend(ctx, id)
			}
		}
	}
}

// next finds one listing with a gap worth another try. A gap whose guard is still
// resting is left alone: asking now would spend a request on a refusal and push
// the backoff out further.
func (m *Mender) next(ctx context.Context) (int64, bool) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT l.id, COALESCE(l.lat,0), COALESCE(l.lon,0)
		FROM listings l JOIN facts f ON f.listing_id = l.id
		WHERE l.gone_at IS NULL AND COALESCE(l.lat,0) != 0 AND f.computed_at > ?
		ORDER BY f.computed_at ASC LIMIT 40`,
		time.Now().Add(-m.window).Unix())
	if err != nil {
		slog.Info("mender could not list listings", slog.Any("err", err))
		return 0, false
	}
	defer rows.Close()

	type candidate struct {
		id       int64
		lat, lon float64
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.lat, &c.lon); err != nil {
			return 0, false
		}
		all = append(all, c)
	}
	if rows.Err() != nil {
		return 0, false
	}

	guards, err := guardStatuses(m.db)
	if err != nil {
		return 0, false
	}
	resting := map[string]bool{}
	for _, g := range guards {
		if g.Open {
			resting[g.Endpoint] = true
		}
	}

	for _, c := range all {
		if m.ref.Running(c.id) {
			continue
		}
		states, err := Progress(ctx, m.db, c.lat, c.lon, 0)
		if err != nil {
			continue
		}
		for _, s := range states {
			if s.Done {
				continue
			}
			stuck := false
			for endpoint := range resting {
				if guardServes(endpoint, s.Key) {
					stuck = true
					break
				}
			}
			if !stuck {
				return c.id, true
			}
		}
	}
	return 0, false
}

func (m *Mender) mend(ctx context.Context, id int64) {
	l, err := listingByID(ctx, m.db, id)
	if err != nil {
		slog.Info("mender could not load a listing", slog.Int64("id", id), slog.Any("err", err))
		return
	}

	if !m.ref.claim(id) {
		return
	}
	defer m.ref.release(id)

	// Its own budget. Everything already answered comes out of the cache, so this
	// is only as slow as whatever is still missing.
	run, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	if _, err := m.ref.assess(run, id, l); err != nil {
		slog.Info("mender could not finish a report", slog.Int64("id", id), slog.Any("err", err))
		return
	}
	slog.Info("filled in a gap on a report", slog.Int64("id", id), slog.String("address", l.Address))
}

func listingByID(ctx context.Context, db *sql.DB, id int64) (Listing, error) {
	var l Listing
	var county, city, state, zip, style, url, remarks sql.NullString
	// property_type and status have to come back too: without them a mended
	// re-assessment runs the manufactured-home test against an empty string and
	// quietly drops a twenty point penalty a full run applied.
	var propType, status sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT address, COALESCE(city,''), COALESCE(state,''), COALESCE(zip,''),
		       COALESCE(county,''), COALESCE(lat,0), COALESCE(lon,0), COALESCE(price,0),
		       COALESCE(beds,0), COALESCE(baths,0), COALESCE(sqft,0), COALESCE(acres,0),
		       COALESCE(year_built,0), COALESCE(style,''), COALESCE(listing_url,''),
		       COALESCE(remarks,''), COALESCE(tax_annual,0), COALESCE(hoa_monthly,0),
		       COALESCE(property_type,''), COALESCE(status,'')
		FROM listings WHERE id = ?`, id).
		Scan(&l.Address, &city, &state, &zip, &county, &l.Lat, &l.Lon, &l.Price,
			&l.Beds, &l.Baths, &l.SqFt, &l.Acres, &l.YearBuilt, &style, &url, &remarks,
			&l.TaxAnnual, &l.HOAMonthly, &propType, &status)
	if err != nil {
		return l, err
	}
	l.City, l.State, l.Zip, l.County = city.String, state.String, zip.String, county.String
	l.Style, l.URL, l.Remarks = style.String, url.String, remarks.String
	l.PropertyType, l.Status = propType.String, status.String
	return l, nil
}
