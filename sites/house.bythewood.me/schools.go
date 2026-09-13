package main

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

// School zoning, from actual attendance boundaries and never from "nearest
// school". Getting this wrong invalidates the whole dashboard, because the
// drop-off detour is the number that ranks this dashboard and it is measured to
// whichever school the house is assigned to.
//
// Two of the four counties publish the boundaries as a live ArcGIS layer and two
// do not. A listing in a county with no layer is marked unverified rather than
// guessed, and the grid says so on the card, because a made-up zone is worse
// than a blank one.
type zoneLayer struct {
	url   string
	field string
	level string // elementary, middle, high
}

type countyZones struct {
	name     string
	district string
	layers   []zoneLayer
	// The point layer holding the schools themselves, so a zone name can be
	// turned into somewhere to drive to.
	schoolsURL   string
	schoolsField string
	levelField   string
}

// Verified by querying each service. Alexander's field is DESCR and carries a
// number in front of the name ("328 TAYLORSVILLE"), Caldwell's is DISTRICT_NAME.
var zoneSources = map[string]countyZones{
	"alexander": {
		name:     "Alexander",
		district: "Alexander County Schools",
		layers: []zoneLayer{
			{"https://maps.alexandercountync.gov/arcgis/rest/services/BoundaryLayers/MapServer/32/query", "DESCR", "elementary"},
			{"https://maps.alexandercountync.gov/arcgis/rest/services/BoundaryLayers/MapServer/33/query", "DESCR", "middle"},
			{"https://maps.alexandercountync.gov/arcgis/rest/services/BoundaryLayers/MapServer/34/query", "DESCR", "high"},
		},
		schoolsURL:   "https://maps.alexandercountync.gov/arcgis/rest/services/BoundaryLayers/MapServer/31/query",
		schoolsField: "NAME",
	},
	"caldwell": {
		name:     "Caldwell",
		district: "Caldwell County Schools",
		layers: []zoneLayer{
			// K-5 first and K-8 second: a K-8 zone answers the elementary and
			// the middle question at once, which the middle lookup below relies on.
			{"https://gis.caldwellcountync.org/arcgis/rest/services/Public_Access/MapServer/13/query", "DISTRICT_NAME", "elementary"},
			{"https://gis.caldwellcountync.org/arcgis/rest/services/Public_Access/MapServer/14/query", "DISTRICT_NAME", "elementary-k8"},
			{"https://gis.caldwellcountync.org/arcgis/rest/services/Public_Access/MapServer/15/query", "DISTRICT_NAME", "middle"},
			{"https://gis.caldwellcountync.org/arcgis/rest/services/Public_Access/MapServer/16/query", "DISTRICT_NAME", "high"},
		},
		schoolsURL:   "https://gis.caldwellcountync.org/arcgis/rest/services/Public_Access/MapServer/12/query",
		schoolsField: "SCHOOLNAME",
		levelField:   "LEVEL_",
	},
}

// The counties inside the drive-time band that publish nothing. Named rather
// than inferred from a missing map entry, so the card can say which district it
// is and that the zone was not checked.
var unzonedCounties = map[string]string{
	"catawba":  "Catawba County Schools, Hickory City Schools or Newton-Conover City Schools",
	"burke":    "Burke County Public Schools",
	"iredell":  "Iredell-Statesville Schools or Mooresville Graded",
	"lincoln":  "Lincoln County Schools",
	"wilkes":   "Wilkes County Schools",
	"mcdowell": "McDowell County Schools",
}

// Every public school in the state as a point, with its grade range and its DPI
// school code. This is the fallback for the counties that publish no attendance
// boundary, which is most of them: the nearest school of each level is not the
// same claim as the zoned one and it is a great deal more use than a blank.
const ncSchools = "https://services.nconemap.gov/secure/rest/services/NC1Map_Education/MapServer/3/query"

// How far to look for a school. Rural districts are wide and a child can be bused
// a long way, so this is generous and the distance is reported alongside.
const schoolSearchFeet = 12 * 5280

type SchoolZones struct {
	Elementary    string
	Middle        string
	High          string
	ElementaryLat float64
	ElementaryLon float64
	MiddleLat     float64
	MiddleLon     float64
	HighLat       float64
	HighLon       float64
	County        string
	Source        string
	// Set when the schools came from the statewide point layer rather than from a
	// real attendance boundary, so the page can say nearest rather than zoned.
	Nearest     bool
	ElemMiles   float64
	MiddleMiles float64
	HighMiles   float64
	// A coordinate had to be taken from the nearest school of that level because
	// the county's own point layer did not carry one, so the drive is to a school
	// that may not be the zoned one.
	Borrowed bool

	// Nothing answered, so this must not be cached: a one minute outage would
	// otherwise cost the address its schools factor forever.
	Partial bool

	Verified bool
	K8       bool // one school covers elementary and middle, so no second move
	Note     string
}

type Schools struct {
	db     *sql.DB
	guards map[string]*Guard
	// The statewide points layer, which is a state server rather than one county
	// machine and can take a brisker pace.
	statewide *Guard
}

func NewSchools(db *sql.DB) *Schools {
	s := &Schools{
		db:     db,
		guards: map[string]*Guard{},
		statewide: NewGuard(db, "nc-schools", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      200,
			Window:      time.Hour,
			Timeout:     40 * time.Second,
		}),
	}
	for key := range zoneSources {
		// A county GIS server is one machine in a county building, so it gets
		// the gentlest pace here and the smallest budget.
		s.guards[key] = NewGuard(db, "gis-"+key, GuardOpts{
			MinInterval: 3 * time.Second,
			Budget:      200,
			Window:      time.Hour,
			TripAfter:   3,
			Timeout:     30 * time.Second,
		})
	}
	return s
}

func countyKey(county string) string {
	c := strings.ToLower(strings.TrimSpace(county))
	c = strings.TrimSuffix(c, " county")
	return strings.TrimSpace(c)
}

// Lookup resolves the zones for one point. The county on the listing is a hint
// only: the point query is what decides, so a listing whose county column is
// wrong still gets the right zone as long as some county claims the point.
func (s *Schools) Lookup(ctx context.Context, lat, lon float64, county string) (SchoolZones, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'zones2' AND key = ?`, key).Scan(&payload)
	if err == nil {
		var cached SchoolZones
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return SchoolZones{}, err
	}

	ck := countyKey(county)
	src, ok := zoneSources[ck]
	if !ok {
		// Try every county that does publish, since the hint may be wrong or a
		// listing may sit near a county line.
		for k, candidate := range zoneSources {
			if z, err := s.queryCounty(ctx, k, candidate, lat, lon); err == nil && z.Elementary != "" {
				return s.cache(ctx, key, z)
			}
		}
		// No boundary anywhere, so fall back to the nearest of each level. It is a
		// different claim and the page says so.
		district := unzonedCounties[ck]
		if district == "" {
			district = strings.TrimSpace(county) + " schools"
		}
		z, err := s.nearestSchools(ctx, lat, lon)
		if err != nil {
			// A bad minute at NC OneMap, not a county without schools. Caching this
			// would cost the address its heaviest factor permanently, since nothing
			// here expires and Progress would count the source as answered.
			return SchoolZones{
				Source:  district,
				Partial: true,
				Note:    "we could not look the schools up just now",
			}, err
		}
		if z.Elementary == "" {
			return s.cache(ctx, key, SchoolZones{
				Source:   district,
				Verified: false,
				Note:     "no attendance boundary published and no schools found nearby",
			})
		}
		z.Source = district
		return s.cache(ctx, key, z)
	}

	z, err := s.queryCounty(ctx, ck, src, lat, lon)
	if err != nil {
		return SchoolZones{Source: src.district, Note: "zone lookup failed: " + err.Error()}, nil
	}
	return s.cache(ctx, key, z)
}

// nearestSchools picks the closest elementary, middle and high school to a point
// from the statewide layer. Distance as the crow flies, which is only deciding
// which school to name and to measure a drive to.
func (s *Schools) nearestSchools(ctx context.Context, lat, lon float64) (SchoolZones, error) {
	var z SchoolZones

	xmin, ymin, xmax, ymax := envelopeAround(lat, lon, schoolSearchFeet)
	v := url.Values{
		"geometry":          {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", xmin, ymin, xmax, ymax)},
		"geometryType":      {"esriGeometryEnvelope"},
		"inSR":              {"4326"},
		"outSR":             {"4326"},
		"spatialRel":        {"esriSpatialRelIntersects"},
		"outFields":         {"school_nam,bgn_grade,end_grade,elem,middle,high,county"},
		"returnGeometry":    {"true"},
		"geometryPrecision": {"6"},
		"f":                 {"json"},
	}

	var res esriQueryResult
	if err := s.statewide.Get(ctx, ncSchools+"?"+v.Encode(), &res); err != nil {
		return z, err
	}

	at := point{lat, lon}
	type best struct {
		name  string
		miles float64
		lat   float64
		lon   float64
	}
	var elem, middle, high best
	elem.miles, middle.miles, high.miles = math.Inf(1), math.Inf(1), math.Inf(1)

	for _, f := range res.Features {
		if f.Geometry.X == nil || f.Geometry.Y == nil {
			continue
		}
		name := attrString(f.Attributes, "school_nam")
		if name == "" {
			continue
		}
		miles := haversineFeet(lat, lon, *f.Geometry.Y, *f.Geometry.X) / 5280
		put := func(b *best) {
			if miles < b.miles {
				*b = best{name, miles, *f.Geometry.Y, *f.Geometry.X}
			}
		}
		// A school can serve more than one level, and a K-12 serves all of them,
		// so these are not exclusive.
		if attrString(f.Attributes, "elem") != "" {
			put(&elem)
		}
		if attrString(f.Attributes, "middle") != "" {
			put(&middle)
		}
		if attrString(f.Attributes, "high") != "" {
			put(&high)
		}
		_ = at
	}

	z.Nearest = true
	if elem.name != "" {
		z.Elementary, z.ElemMiles = elem.name, elem.miles
		z.ElementaryLat, z.ElementaryLon = elem.lat, elem.lon
	}
	if middle.name != "" {
		z.Middle, z.MiddleMiles = middle.name, middle.miles
		z.MiddleLat, z.MiddleLon = middle.lat, middle.lon
	}
	if high.name != "" {
		z.High, z.HighMiles = high.name, high.miles
		z.HighLat, z.HighLon = high.lat, high.lon
	}
	z.Note = "nearest of each level, since this county publishes no attendance boundary"
	return z, nil
}

func (s *Schools) cache(ctx context.Context, key string, z SchoolZones) (SchoolZones, error) {
	if z.Partial {
		return z, nil
	}
	raw, err := json.Marshal(z)
	if err != nil {
		return z, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('zones2',?,?,?)`,
		key, string(raw), time.Now().Unix())
	return z, err
}

func (s *Schools) queryCounty(ctx context.Context, ck string, src countyZones, lat, lon float64) (SchoolZones, error) {
	z := SchoolZones{Source: src.district, County: src.name}
	guard := s.guards[ck]

	for _, layer := range src.layers {
		name, err := s.zoneName(ctx, guard, layer, lat, lon)
		if err != nil {
			return z, err
		}
		if name == "" {
			continue
		}
		switch layer.level {
		case "elementary":
			z.Elementary = name
		case "elementary-k8":
			// A K-8 zone wins over a K-5 one for the same point and answers the
			// middle school question too, which is the good case: no second
			// school run when the elementary years end.
			z.Elementary = name
			z.Middle = name
			z.K8 = true
		case "middle":
			if !z.K8 {
				z.Middle = name
			}
		case "high":
			z.High = name
		}
	}

	if z.Elementary != "" || z.High != "" {
		z.Verified = true
	}

	if err := s.locate(ctx, ck, src, &z); err != nil {
		z.Note = "zones are real, school locations are not: " + err.Error()
	}

	// A zone with a name and no coordinate cannot be routed to, and the county
	// point layers do not always carry every school. The statewide layer fills in
	// whichever is still missing, keeping the zoned name and borrowing only the
	// position of the nearest school of that level.
	if z.ElementaryLat == 0 || z.MiddleLat == 0 || z.HighLat == 0 {
		if near, err := s.nearestSchools(ctx, lat, lon); err == nil {
			// Borrowing a position means the detour is measured to whichever school
			// is closest, which is not necessarily the one the child is zoned to.
			// README says outright that guessing this is worse than leaving it
			// blank, so the page has to stop claiming the zoning is confirmed.
			z.Borrowed = true
			if z.ElementaryLat == 0 {
				z.ElementaryLat, z.ElementaryLon = near.ElementaryLat, near.ElementaryLon
				z.ElemMiles = near.ElemMiles
			}
			if z.MiddleLat == 0 {
				z.MiddleLat, z.MiddleLon = near.MiddleLat, near.MiddleLon
				z.MiddleMiles = near.MiddleMiles
			}
			if z.HighLat == 0 {
				z.HighLat, z.HighLon = near.HighLat, near.HighLon
				z.HighMiles = near.HighMiles
			}
		}
	}

	// A zoned school has a name and a boundary and nothing that says how far away
	// it is, so the distance is measured here once the coordinate is settled. It
	// is a straight line and it is only used to rank, never to promise a drive.
	z.fillMiles(lat, lon)
	return z, nil
}

func (z *SchoolZones) fillMiles(lat, lon float64) {
	for _, m := range []struct {
		miles *float64
		slat  float64
		slon  float64
	}{
		{&z.ElemMiles, z.ElementaryLat, z.ElementaryLon},
		{&z.MiddleMiles, z.MiddleLat, z.MiddleLon},
		{&z.HighMiles, z.HighLat, z.HighLon},
	} {
		if *m.miles == 0 && m.slat != 0 {
			*m.miles = haversineFeet(lat, lon, m.slat, m.slon) / 5280
		}
	}
}

// SchoolsRaw scores the schools on how close they are, which is real information
// whether or not the county publishes an attendance boundary. Three of the five
// counties here publish none, and scoring those houses at a quarter was docking
// them for their county's filing habits rather than for anything about the house.
func (z SchoolZones) SchoolsRaw() (float64, bool) {
	var sum, n float64
	for _, miles := range []float64{z.ElemMiles, z.MiddleMiles, z.HighMiles} {
		if miles <= 0 {
			continue
		}
		// A mile and a half is a school run you barely notice and twelve is one
		// that shapes the morning.
		sum += band(miles, 1.5, 12)
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / n, true
}

func (s *Schools) zoneName(ctx context.Context, guard *Guard, layer zoneLayer, lat, lon float64) (string, error) {
	v := url.Values{
		"geometry":       {fmt.Sprintf(`{"x":%.6f,"y":%.6f,"spatialReference":{"wkid":4326}}`, lon, lat)},
		"geometryType":   {"esriGeometryPoint"},
		"inSR":           {"4326"},
		"spatialRel":     {"esriSpatialRelIntersects"},
		"outFields":      {layer.field},
		"returnGeometry": {"false"},
		"f":              {"json"},
	}
	var res esriQueryResult
	if err := guard.Get(ctx, layer.url+"?"+v.Encode(), &res); err != nil {
		return "", err
	}
	if len(res.Features) == 0 {
		return "", nil
	}
	return tidyZoneName(attrString(res.Features[0].Attributes, layer.field)), nil
}

// tidyZoneName drops the leading district number Alexander puts in front of the
// name and title cases the shouting, so "328 TAYLORSVILLE" reads as Taylorsville.
func tidyZoneName(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) > 1 && num(fields[0]) != 0 && !strings.ContainsAny(fields[0], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		fields = fields[1:]
	}
	for i, f := range fields {
		lower := strings.ToLower(f)
		fields[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(fields, " ")
}

// locate fills in where the zoned elementary and middle schools actually are, by
// matching the zone name against the county's own school point layer. The zone
// layers carry a name and no coordinate, and a drop-off detour needs a
// coordinate.
func (s *Schools) locate(ctx context.Context, ck string, src countyZones, z *SchoolZones) error {
	if src.schoolsURL == "" {
		return nil
	}
	points, err := s.schoolPoints(ctx, ck, src)
	if err != nil {
		return err
	}
	if lat, lon, ok := bestSchoolMatch(points, z.Elementary); ok {
		z.ElementaryLat, z.ElementaryLon = lat, lon
	}
	if lat, lon, ok := bestSchoolMatch(points, z.Middle); ok {
		z.MiddleLat, z.MiddleLon = lat, lon
	}
	if lat, lon, ok := bestSchoolMatch(points, z.High); ok {
		z.HighLat, z.HighLon = lat, lon
	}
	return nil
}

type schoolPoint struct {
	Name  string  `json:"name"`
	Level string  `json:"level"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

// schoolPoints fetches a county's whole school layer once and caches it. It is a
// few dozen points and it does not move.
func (s *Schools) schoolPoints(ctx context.Context, ck string, src countyZones) ([]schoolPoint, error) {
	var payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'schoolpoints' AND key = ?`, ck).Scan(&payload)
	if err == nil {
		var pts []schoolPoint
		if json.Unmarshal([]byte(payload), &pts) == nil && len(pts) > 0 {
			return pts, nil
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	fields := src.schoolsField
	if src.levelField != "" {
		fields += "," + src.levelField
	}
	v := url.Values{
		"where":             {"1=1"},
		"outFields":         {fields},
		"returnGeometry":    {"true"},
		"outSR":             {"4326"},
		"geometryPrecision": {"6"},
		"f":                 {"json"},
	}
	var res esriQueryResult
	if err := s.guards[ck].Get(ctx, src.schoolsURL+"?"+v.Encode(), &res); err != nil {
		return nil, err
	}

	var pts []schoolPoint
	for _, f := range res.Features {
		if f.Geometry.X == nil || f.Geometry.Y == nil {
			continue
		}
		pts = append(pts, schoolPoint{
			Name:  attrString(f.Attributes, src.schoolsField),
			Level: attrString(f.Attributes, src.levelField),
			Lat:   *f.Geometry.Y,
			Lon:   *f.Geometry.X,
		})
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("school layer returned no points")
	}

	raw, err := json.Marshal(pts)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('schoolpoints',?,?,?)`,
		ck, string(raw), time.Now().Unix()); err != nil {
		return nil, err
	}
	return pts, nil
}

// bestSchoolMatch scores token overlap between a zone name and a school name,
// because the two layers spell the same school differently: a zone called
// "East Middle" is the school "East Alexander Middle School".
func bestSchoolMatch(points []schoolPoint, zone string) (lat, lon float64, ok bool) {
	want := nameTokens(zone)
	if len(want) == 0 {
		return 0, 0, false
	}
	best := 0.0
	for _, p := range points {
		have := nameTokens(p.Name)
		var hits int
		for _, w := range want {
			for _, h := range have {
				if w == h {
					hits++
					break
				}
			}
		}
		if hits == 0 {
			continue
		}
		// Normalised so a long school name does not win on length alone.
		score := float64(hits) / math.Max(float64(len(want)), 1)
		if score > best {
			best, lat, lon, ok = score, p.Lat, p.Lon, true
		}
	}
	// Half the zone's words have to appear, or it is not the same school.
	if best < 0.5 {
		return 0, 0, false
	}
	return lat, lon, ok
}

var nameNoise = map[string]bool{
	"school": true, "elementary": true, "middle": true, "high": true,
	"the": true, "of": true, "county": true, "district": true, "k8": true,
}

func nameTokens(s string) []string {
	var out []string
	for _, f := range strings.Fields(strings.ToLower(s)) {
		f = nonAlnum.ReplaceAllString(f, "")
		if f == "" || nameNoise[f] || num(f) != 0 {
			continue
		}
		out = append(out, f)
	}
	return out
}
