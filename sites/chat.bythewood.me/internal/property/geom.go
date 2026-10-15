package property

import "math"

// Distances are in feet on a local equirectangular projection, x scaled by
// cos(latitude), and over a few thousand feet the error is well under a foot.
const (
	feetPerDegLat = 364000.0 // 1 degree of latitude, near enough at this latitude
	sqFtPerAcre   = 43560.0
)

type point struct{ lat, lon float64 }

type esriGeometry struct {
	Rings [][][]float64 `json:"rings"`
	Paths [][][]float64 `json:"paths"`
	X     *float64      `json:"x"`
	Y     *float64      `json:"y"`
}

func feetPerDegLon(lat float64) float64 {
	return feetPerDegLat * math.Cos(lat*math.Pi/180)
}

func distToSegmentFeet(p, a, b point) float64 {
	fx := feetPerDegLon(p.lat)
	px, py := 0.0, 0.0
	ax, ay := (a.lon-p.lon)*fx, (a.lat-p.lat)*feetPerDegLat
	bx, by := (b.lon-p.lon)*fx, (b.lat-p.lat)*feetPerDegLat

	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(ax, ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	cx, cy := ax+t*dx, ay+t*dy
	return math.Hypot(cx, cy)
}

// pointInRing doesn't distinguish holes, since inside is the cautious answer for
// a flood zone and these layers rarely carry one.
func pointInRing(p point, ring [][]float64) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		if (yi > p.lat) != (yj > p.lat) &&
			p.lon < (xj-xi)*(p.lat-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// distanceFeet is zero inside a polygon and +Inf for a shape it can't read, so
// that reads as not nearby instead of right here.
func (g esriGeometry) distanceFeet(p point) float64 {
	best := math.Inf(1)

	if g.X != nil && g.Y != nil {
		return distToSegmentFeet(p, point{*g.Y, *g.X}, point{*g.Y, *g.X})
	}

	for _, ring := range g.Rings {
		if len(ring) < 3 {
			continue
		}
		if pointInRing(p, ring) {
			return 0
		}
		best = math.Min(best, ringDistance(p, ring))
	}
	for _, path := range g.Paths {
		best = math.Min(best, ringDistance(p, path))
	}
	return best
}

func ringDistance(p point, ring [][]float64) float64 {
	best := math.Inf(1)
	for i := 0; i+1 < len(ring); i++ {
		if len(ring[i]) < 2 || len(ring[i+1]) < 2 {
			continue
		}
		a := point{ring[i][1], ring[i][0]}
		b := point{ring[i+1][1], ring[i+1][0]}
		best = math.Min(best, distToSegmentFeet(p, a, b))
	}
	return best
}

// acres is for a county that publishes parcel shapes and no attributes. Holes
// subtract on their own, since ArcGIS winds them the other way.
func (g esriGeometry) acres(around point) float64 {
	fx, fy := feetPerDegLon(around.lat), feetPerDegLat

	var total float64
	for _, ring := range g.Rings {
		if len(ring) < 4 {
			continue
		}
		var sum float64
		for i := 0; i+1 < len(ring); i++ {
			if len(ring[i]) < 2 || len(ring[i+1]) < 2 {
				continue
			}
			x1, y1 := ring[i][0]*fx, ring[i][1]*fy
			x2, y2 := ring[i+1][0]*fx, ring[i+1][1]*fy
			sum += x1*y2 - x2*y1
		}
		total += sum / 2
	}
	return math.Abs(total) / sqFtPerAcre
}

func envelopeAround(lat, lon, feet float64) (xmin, ymin, xmax, ymax float64) {
	dLat := feet / feetPerDegLat
	dLon := feet / feetPerDegLon(lat)
	return lon - dLon, lat - dLat, lon + dLon, lat + dLat
}

// centroid is for the map pin, since the geocoder puts the point in the road a
// few doors along.
func (g esriGeometry) centroid() (point, bool) {
	for _, ring := range g.Rings {
		if len(ring) < 4 {
			continue
		}
		var area, cx, cy float64
		for i := 0; i+1 < len(ring); i++ {
			if len(ring[i]) < 2 || len(ring[i+1]) < 2 {
				continue
			}
			x1, y1 := ring[i][0], ring[i][1]
			x2, y2 := ring[i+1][0], ring[i+1][1]
			cross := x1*y2 - x2*y1
			area += cross
			cx += (x1 + x2) * cross
			cy += (y1 + y2) * cross
		}
		if area == 0 {
			continue
		}
		area /= 2
		return point{lat: cy / (6 * area), lon: cx / (6 * area)}, true
	}
	return point{}, false
}

// boxAround is in the south,west,north,east order Overpass wants, unlike ArcGIS.
func boxAround(lat, lon, feet float64) (south, west, north, east float64) {
	dLat := feet / feetPerDegLat
	dLon := feet / feetPerDegLon(lat)
	return lat - dLat, lon - dLon, lat + dLat, lon + dLon
}

// haversineFeet is never for a commute, since a straight line through a ridge
// isn't a drive.
func haversineFeet(lat1, lon1, lat2, lon2 float64) float64 {
	const earthFeet = 20902231.0 // mean Earth radius, 6371 km in feet
	p := math.Pi / 180
	dLat := (lat2 - lat1) * p
	dLon := (lon2 - lon1) * p
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthFeet * math.Asin(math.Min(1, math.Sqrt(a)))
}
