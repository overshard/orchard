package property

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"
)

// OSRM's public demo server asks for light use, so every route is cached forever.
const (
	osrmBase = "https://router.project-osrm.org"

	// The demo profile has no traffic model, so it understates the morning run,
	// but the detour is a difference of two routes at the same hour and that
	// largely cancels out.
	osrmProfile = "/route/v1/driving/"
)

type Router struct {
	db    *sql.DB
	guard *Guard
}

func NewRouter(db *sql.DB) *Router {
	return &Router{
		db: db,
		// The demo server is a courtesy and asks for light use in so many words.
		// Self-hosting OSRM is the answer if this is ever too slow.
		guard: NewGuard(db, "osrm", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      150,
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

	Kind string

	// Without Who a question like "what about her commute" has nothing in the
	// answer to match on.
	Who  string `json:",omitempty"`
	Name string `json:",omitempty"`
}

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Duration float64 `json:"duration"`
		Distance float64 `json:"distance"`
		Geometry string  `json:"geometry"`
	} `json:"routes"`
}

// Route takes waypoints in order instead of adding legs, since a stop changes
// which way the route leaves the first point.
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
	if _, err := keep(ctx, r.db,
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

// Morning's detours are the commute through each school minus the commute alone,
// which is the cost of the drop-off.
type Morning struct {
	Commute        Leg
	WithElementary Leg
	WithMiddle     Leg
	WithHigh       Leg

	DetourMin       float64
	MiddleDetourMin float64
	HighDetourMin   float64

	// The elementary and middle school lie in opposite directions from the
	// house, which is a two-stop morning once both matter.
	OppositeWays bool

	Partial bool // a leg could not be routed, so the detour is not trustworthy
}

// Morning marks a missing school coordinate partial instead of zero, since a zero
// detour reads as the best possible school run.
func (r *Router) Morning(ctx context.Context, home, work point, elem, middle, high point) Morning {
	var m Morning

	// Asked for at once, and the guard still paces them one at a time.
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
		// Measured from the house, since two schools more than 120 degrees apart
		// from the driveway can't both fit in one morning trip.
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
