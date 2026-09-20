package main

import "math"

// Just enough geometry to answer "how far is this house from that" without a
// dependency. Every distance comes back in feet.
//
// The projection is a local equirectangular one: x scaled by cos(latitude), y
// left alone, both multiplied by feet per degree. Over the few thousand feet
// these questions cover the error is well under a foot.
const (
	feetPerDegLat = 364000.0 // 1 degree of latitude, near enough at this latitude
	sqFtPerAcre   = 43560.0
)

type point struct{ lat, lon float64 }

// esriGeometry is the subset of ArcGIS JSON geometry these services return.
type esriGeometry struct {
	Rings [][][]float64 `json:"rings"`
	Paths [][][]float64 `json:"paths"`
	X     *float64      `json:"x"`
	Y     *float64      `json:"y"`
}

func feetPerDegLon(lat float64) float64 {
	return feetPerDegLat * math.Cos(lat*math.Pi/180)
}

// distToSegmentFeet is the distance from p to the segment ab, projected flat
// around p's latitude.
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

// pointInRing is ray casting, counting crossings of the ring's edges. Holes are
// not distinguished: for "is this house in a flood zone" treating a hole as
// inside is the cautious answer and these layers rarely carry one.
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

// distanceFeet is the distance from p to a geometry, and zero when p is inside a
// polygon. Anything unrecognised comes back as +Inf so a caller treats a shape
// it cannot read as "not nearby" rather than as "right here".
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

// acres is the area of a polygon, which is how a lot size is known in a county
// that publishes parcel shapes and no attributes. The shoelace formula on the
// same flat projection everything else here uses, so it is exact enough at the
// size of a house lot and meaningless at the size of a state.
//
// Rings after the first are holes and are subtracted, since an ArcGIS polygon
// winds them the other way.
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

// envelopeAround is the lat/lon box a given number of feet out from a point,
// which is what every ArcGIS envelope query here asks for.
func envelopeAround(lat, lon, feet float64) (xmin, ymin, xmax, ymax float64) {
	dLat := feet / feetPerDegLat
	dLon := feet / feetPerDegLon(lat)
	return lon - dLon, lat - dLat, lon + dLon, lat + dLat
}

// centroid is the middle of the first ring, area weighted. The geocoder puts a
// point in the road, so a map link built from it drops its pin a few doors along,
// and the lot's own middle is where the house actually is.
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

// boxAround is the lat/lon bounding box a given number of feet out from a point,
// in the south,west,north,east order Overpass wants, which is the opposite order
// to the one ArcGIS wants.
func boxAround(lat, lon, feet float64) (south, west, north, east float64) {
	dLat := feet / feetPerDegLat
	dLon := feet / feetPerDegLon(lat)
	return lat - dLat, lon - dLon, lat + dLat, lon + dLon
}
