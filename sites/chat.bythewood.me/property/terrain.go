package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

// Whether there is flat ground to put raised beds and a chicken run on. Acreage
// on its own does not answer it, since two acres on the side of a ridge is not
// two acres you can use.
//
// USGS 3DEP answers at one metre resolution and samples many points in one
// request, so a grid around the house costs one call.
const (
	elevationSamples = "https://elevation.nationalmap.gov/arcgis/rest/services/3DEPElevation/ImageServer/getSamples"

	// The box sampled around the house, and the grid across it. 300ft square at
	// 5 by 5 is a sample every 75ft, which is the scale a garden and a run sit at
	// and is coarse enough not to be reading the roof of the house.
	terrainBoxFeet = 300
	terrainGrid    = 5

	terrainCacheKind = "terrain"
)

type TerrainResult struct {
	// Feet between the highest and lowest sample, which is the number that says
	// whether the yard is flat.
	ReliefFeet float64

	// The steepest and the average rise across neighbouring samples, as a percent.
	MaxSlopePct  float64
	MeanSlopePct float64

	// The share of the sampled grid that is flat enough to build on, against the
	// threshold below.
	FlatShare float64

	Partial bool
}

// Ten percent is the line. A raised bed wants under about five percent and a
// chicken run will take more, so ten is the figure that separates usable yard from
// hillside without ruling out the gentle slope nearly every lot here has.
const flatEnoughPct = 10

type Terrain struct {
	db    *sql.DB
	guard *Guard
}

func NewTerrain(db *sql.DB) *Terrain {
	return &Terrain{
		db: db,
		guard: NewGuard(db, "usgs-3dep", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      60,
			Timeout:     40 * time.Second,
		}),
	}
}

type samplesResponse struct {
	Samples []struct {
		LocationID int    `json:"locationId"`
		Value      string `json:"value"`
	} `json:"samples"`
}

func (t *Terrain) Lookup(ctx context.Context, lat, lon float64) (TerrainResult, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	err := t.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = ? AND key = ?`, terrainCacheKind, key).Scan(&payload)
	if err == nil {
		var cached TerrainResult
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return TerrainResult{}, err
	}

	// The grid, in reading order, so locationId maps straight back to a row and
	// column and the slope between neighbours is an index away.
	half := terrainBoxFeet / 2.0
	step := terrainBoxFeet / float64(terrainGrid-1)
	dLat := step / feetPerDegLat
	dLon := step / feetPerDegLon(lat)

	var points []string
	for row := 0; row < terrainGrid; row++ {
		for col := 0; col < terrainGrid; col++ {
			y := lat - half/feetPerDegLat + float64(row)*dLat
			x := lon - half/feetPerDegLon(lat) + float64(col)*dLon
			points = append(points, fmt.Sprintf("[%.7f,%.7f]", x, y))
		}
	}

	geometry := `{"points":[` + strings.Join(points, ",") + `],"spatialReference":{"wkid":4326}}`
	u := elevationSamples + "?" + url.Values{
		"geometry":             {geometry},
		"geometryType":         {"esriGeometryMultipoint"},
		"returnFirstValueOnly": {"true"},
		"f":                    {"json"},
	}.Encode()

	var res samplesResponse
	if err := t.guard.Get(ctx, u, &res); err != nil {
		return TerrainResult{Partial: true}, err
	}

	// Values come back as strings in metres, and a point off the edge of the
	// coverage comes back as NoData rather than a number.
	const metresToFeet = 3.28084
	grid := make([]float64, terrainGrid*terrainGrid)
	for i := range grid {
		grid[i] = math.NaN()
	}
	for _, s := range res.Samples {
		if s.LocationID < 0 || s.LocationID >= len(grid) {
			continue
		}
		v := num(s.Value)
		if v == 0 {
			continue
		}
		grid[s.LocationID] = v * metresToFeet
	}

	out := computeTerrain(grid, step)
	if out.Partial {
		return out, fmt.Errorf("3dep returned too few samples")
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := t.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		terrainCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// computeTerrain is split out so the arithmetic is testable without a network
// call. grid is row major and step is the spacing between samples in feet.
func computeTerrain(grid []float64, step float64) TerrainResult {
	var out TerrainResult
	lo, hi := math.Inf(1), math.Inf(-1)
	valid := 0

	for _, v := range grid {
		if math.IsNaN(v) {
			continue
		}
		valid++
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	// Half the grid has to have come back, or the numbers describe a different
	// piece of ground than the one asked about.
	if valid < len(grid)/2 {
		out.Partial = true
		return out
	}
	out.ReliefFeet = hi - lo

	// Slope between each sample and the one to its right and the one below it.
	// Only cells with both neighbours count toward the flat share, since an edge
	// cell's only comparison can run along the contour where there is no fall at
	// all, which reads a uniform hillside as mostly flat.
	var sum float64
	var pairs, flat, cells int
	for row := 0; row < terrainGrid-1; row++ {
		for col := 0; col < terrainGrid-1; col++ {
			here := grid[row*terrainGrid+col]
			right := grid[row*terrainGrid+col+1]
			below := grid[(row+1)*terrainGrid+col]
			if math.IsNaN(here) || math.IsNaN(right) || math.IsNaN(below) {
				continue
			}

			cellOK := true
			for _, there := range []float64{right, below} {
				slope := math.Abs(there-here) / step * 100
				sum += slope
				pairs++
				out.MaxSlopePct = math.Max(out.MaxSlopePct, slope)
				if slope > flatEnoughPct {
					cellOK = false
				}
			}
			cells++
			if cellOK {
				flat++
			}
		}
	}
	if pairs > 0 {
		out.MeanSlopePct = sum / float64(pairs)
	}
	if cells > 0 {
		out.FlatShare = float64(flat) / float64(cells)
	}
	return out
}

// Fence comes off the listing's own words, which is the only source for it: no
// public dataset records whether a yard is fenced, and an agent who fenced a yard
// says so because it sells.
var fenceWords = []string{
	"fenced", "fencing", "privacy fence", "chain link", "chainlink",
	"board fence", "split rail", "invisible fence", "dog run", "kennel",
}

// FenceNote reads the listing's own words, since that is the only source: no
// public dataset records whether a yard is fenced, and an agent who fenced one
// says so because it sells.
func FenceNote(remarks string) string {
	hay := strings.ToLower(remarks)
	for _, w := range fenceWords {
		if !strings.Contains(hay, w) {
			continue
		}
		// Fenced in front of something it is not is the one false positive worth
		// catching: a fenced community pool is not a fenced yard.
		if strings.Contains(hay, w+" community") || strings.Contains(hay, w+" pool") {
			continue
		}
		return w
	}
	return ""
}
