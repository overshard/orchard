package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"
)

// Drive times, from a real routing engine. The roads run around ridges and along
// the river, so two houses the same distance from the office can be fifteen
// minutes apart.
//
// OSRM's public demo server is the default and it asks for light use, so every
// route is cached forever and a refresh only routes listings it has not seen.
const (
	osrmBase = "https://router.project-osrm.org"

	// OSRM's demo profile is car with no traffic model, so a night shift drive is
	// close to right and the morning school run is the one this understates. The
	// detour is a difference of two routes over the same roads at the same hour,
	// so the understatement largely cancels out.
	osrmProfile = "/route/v1/driving/"
)

type Router struct {
	db    *sql.DB
	guard *Guard
}

func NewRouter(db *sql.DB) *Router {
	return &Router{
		db: db,
		// The demo server is a courtesy and asks for light use in so many words,
		// which is why this is the slowest pace here after Overpass. Self-hosting
		// OSRM is the answer if a run ever needs to be faster than this.
		guard: NewGuard(db, "osrm", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      300,
			Window:      time.Hour,
			Timeout:     30 * time.Second,
			UserAgent:   clientUA,
		}),
	}
}

type Leg struct {
	Minutes  float64
	Miles    float64
	Geometry string // encoded polyline, for drawing the morning route on the map

	// What the destination is, straight off the config entry. Scoring groups the
	// drives by this rather than guessing from the key, which put a hospital in
	// with the colleges the first time round.
	Kind string
}

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Duration float64 `json:"duration"`
		Distance float64 `json:"distance"`
		Geometry string  `json:"geometry"`
	} `json:"routes"`
}

// Route is one or more waypoints in order, so home to school to work is one call
// and one cache entry rather than two legs added together. Adding legs would be
// wrong anyway, since a stop changes which way the route leaves the first point.
func (r *Router) Route(ctx context.Context, pts ...point) (Leg, error) {
	if len(pts) < 2 {
		return Leg{}, fmt.Errorf("a route needs two points")
	}

	key := ""
	coords := ""
	for i, p := range pts {
		if p.lat == 0 && p.lon == 0 {
			return Leg{}, fmt.Errorf("waypoint %d has no coordinate", i)
		}
		if i > 0 {
			coords += ";"
			key += ";"
		}
		coords += fmt.Sprintf("%.6f,%.6f", p.lon, p.lat)
		key += fmt.Sprintf("%.5f,%.5f", p.lat, p.lon)
	}

	var leg Leg
	var seconds, meters float64
	var geom sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT seconds, meters, geometry FROM routes WHERE key = ?`, key).Scan(&seconds, &meters, &geom)
	if err == nil {
		return Leg{Minutes: seconds / 60, Miles: meters / 1609.344, Geometry: geom.String}, nil
	}
	if err != sql.ErrNoRows {
		return leg, err
	}

	u := osrmBase + osrmProfile + coords + "?overview=simplified&geometries=polyline"
	var res osrmResponse
	if err := r.guard.Get(ctx, u, &res); err != nil {
		return leg, err
	}
	if res.Code != "Ok" || len(res.Routes) == 0 {
		return leg, fmt.Errorf("osrm: %s", res.Code)
	}

	route := res.Routes[0]
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO routes (key, seconds, meters, geometry, fetched_at) VALUES (?,?,?,?,?)`,
		key, route.Duration, route.Distance, route.Geometry, time.Now().Unix()); err != nil {
		return leg, err
	}

	return Leg{
		Minutes:  route.Duration / 60,
		Miles:    route.Distance / 1609.344,
		Geometry: route.Geometry,
	}, nil
}

// Morning holds the three numbers the school run turns on, the commute with no
// child in the car, the commute actually driven, and the difference between
// them, which is the cost of the drop-off and what ranks this dashboard.
type Morning struct {
	Commute        Leg
	WithElementary Leg
	WithMiddle     Leg
	WithHigh       Leg

	DetourMin       float64
	MiddleDetourMin float64
	HighDetourMin   float64

	// True when the elementary and the middle school lie in opposite directions
	// from the house. That is a two-stop morning once both children's schools
	// matter, which is a dealbreaker wearing the costume of a small detour.
	OppositeWays bool

	Partial bool // a leg could not be routed, so the detour is not trustworthy
}

// Morning computes the three routes and the two detours. A missing school
// coordinate is a partial result rather than a zero, because a zero detour would
// rank an unknown house at the top of the grid.
func (r *Router) Morning(ctx context.Context, home, work point, elem, middle, high point) Morning {
	var m Morning

	// Three independent routes, asked for at once. The guard still paces them
	// against the demo server one at a time, which is the part that matters, and
	// asking in sequence only added the round trips together.
	var wg sync.WaitGroup
	var base, withElem, withMiddle, withHigh Leg
	var baseErr, elemErr, middleErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		base, baseErr = r.Route(ctx, home, work)
	}()
	if elem.lat != 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withElem, elemErr = r.Route(ctx, home, elem, work)
		}()
	}
	if middle.lat != 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withMiddle, middleErr = r.Route(ctx, home, middle, work)
		}()
	}
	if high.lat != 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withHigh, _ = r.Route(ctx, home, high, work)
		}()
	}
	wg.Wait()

	if baseErr != nil {
		m.Partial = true
		return m
	}
	m.Commute = base

	if elem.lat == 0 || elemErr != nil {
		m.Partial = true
	} else {
		m.WithElementary = withElem
		m.DetourMin = withElem.Minutes - base.Minutes
	}

	if middle.lat != 0 {
		if middleErr != nil {
			m.Partial = true
		} else {
			m.WithMiddle = withMiddle
			m.MiddleDetourMin = withMiddle.Minutes - base.Minutes
		}
	}

	if withHigh.Minutes > 0 {
		m.WithHigh = withHigh
		m.HighDetourMin = withHigh.Minutes - base.Minutes
	}

	if elem.lat != 0 && middle.lat != 0 {
		// Opposite is measured from the house, not from the office: the question
		// is whether one morning trip can take in both, and two schools more
		// than 120 degrees apart as seen from the driveway cannot be.
		a := bearingDeg(home.lat, home.lon, elem.lat, elem.lon)
		b := bearingDeg(home.lat, home.lon, middle.lat, middle.lon)
		diff := math.Abs(a - b)
		if diff > 180 {
			diff = 360 - diff
		}
		m.OppositeWays = diff > 120
	}

	return m
}
