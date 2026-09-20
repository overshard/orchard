package main

import (
	"math"
	"strings"
)

// One score out of a hundred per listing, and the breakdown behind it is never
// hidden. Each factor scores 0 to 1 on its own terms, then the configured weight
// turns that into points, so editing a weight in config changes the ranking and
// not the ceiling.
type FactorScore struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Group  string  `json:"group"`
	Raw    float64 `json:"raw"` // 0 to 1
	Weight float64 `json:"weight"`
	Points float64 `json:"points"`
	Why    string  `json:"why"`

	// Nothing answered, so this is no score rather than a low one. A missing
	// lookup scored at half marks reads as an average place and scored at zero
	// reads as a bad one, and neither is a claim anybody made.
	Unknown bool `json:"unknown"`
}

// Measured is the weight of everything that actually answered, which is what the
// score is out of.
func (b Breakdown) Measured() float64 {
	var total float64
	for _, f := range b.Factors {
		if !f.Unknown {
			total += f.Weight
		}
	}
	return total
}

// MeasuredPct is how much of the house this score actually saw.
func (b Breakdown) MeasuredPct() float64 {
	if b.Total <= 0 {
		return 100
	}
	return b.Measured() / b.Total * 100
}

// Unmeasured is the weight of everything nothing answered for.
func (b Breakdown) Unmeasured() float64 {
	var total float64
	for _, f := range b.Factors {
		if f.Unknown {
			total += f.Weight
		}
	}
	return total
}

type Breakdown struct {
	Factors   []FactorScore `json:"factors"`
	Penalties []Flag        `json:"penalties"`
	Gross     float64       `json:"gross"`
	Docked    float64       `json:"docked"`
	Score     float64       `json:"score"`
	Out       bool          `json:"out"`

	// The sum of every weight, kept so the report can express an unmeasured factor
	// as points out of a hundred without reaching for the config again.
	Total float64 `json:"total"`
}

// band maps a value onto 0..1 where best is the value scoring 1 and worst the
// value scoring 0, clamped at both ends. It reads the same whichever direction
// is better, so every factor below uses it rather than its own arithmetic.
// gradesRaw averages whatever DPI letters are published for the three schools.
func gradesRaw(g SchoolGrades) float64 {
	var sum, n float64
	for _, letter := range []string{g.Elementary, g.Middle, g.High} {
		if p, ok := gradePoints(letter); ok {
			sum += p
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// schoolList names the schools in the order a child meets them.
func schoolList(z SchoolZones) string {
	var names []string
	for _, n := range []string{z.Elementary, z.Middle, z.High} {
		if strings.TrimSpace(n) != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "no school found"
	}
	return strings.Join(names, ", then ")
}

// schoolMiles is the one that matters most in the morning.
func schoolMiles(z SchoolZones) string {
	if z.ElemMiles > 0 {
		return "the primary about " + trimFloat(math.Round(z.ElemMiles*10)/10) + " miles away"
	}
	return "distances unmeasured"
}

// haveKind reports whether any drive of a kind actually came back with a time.
func haveKind(drives map[string]Leg, kind string) bool {
	for _, leg := range drives {
		if leg.Kind == kind && leg.Minutes > 0 {
			return true
		}
	}
	return false
}

func monthlyWhy(total, target float64) string {
	if total <= target {
		return money(total) + " a month all in, inside " + money(target)
	}
	return money(total) + " a month all in, " + money(total-target) + " over " + money(target)
}

func band(v, best, worst float64) float64 {
	if best == worst {
		return 0.5
	}
	t := (v - worst) / (best - worst)
	return math.Max(0, math.Min(1, t))
}

// comfortBand scores a cost that is fine up to a figure and hurts more and more
// past it. A straight line from target to ceiling treats ten dollars over and a
// thousand over as points on the same slope. Anything at or under the target is
// full marks and the score halves for every (ceiling - target) over.
func comfortBand(v, target, ceiling float64) float64 {
	if v <= target {
		return 1
	}
	half := ceiling - target
	if half <= 0 {
		return 0
	}
	return math.Pow(0.5, (v-target)/half)
}

// Assessment is everything known about one listing by the time it is scored.
type Assessment struct {
	Listing  Listing
	Flood    FloodResult
	Road     RoadResult
	Zones    SchoolZones
	Morning  Morning
	Monthly  Monthly
	Value    ValueEstimate
	Drives   map[string]Leg // the partner's destinations, by place key
	Bearing  float64
	Terrain  TerrainResult
	Outings  OutingsResult
	Street   StreetResult
	Parcel   ParcelResult
	Area     AreaProfile
	Fence    string
	Flags    []Flag
	Excluded []string
	Stretch  bool
	Grades   SchoolGrades
}

// SchoolGrades is the K-12 quality composite. NC DPI publishes a performance
// grade per school, which has to be dropped in as a file: there is no API for it.
type SchoolGrades struct {
	Elementary string
	Middle     string
	High       string
	Have       bool
}

// gradePoints turns a DPI letter into 0..1. The scale is the state's own, A
// through F on a 15 point band.
func gradePoints(g string) (float64, bool) {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A", "A+NG":
		return 1, true
	case "B":
		return 0.78, true
	case "C":
		return 0.55, true
	case "D":
		return 0.3, true
	case "F":
		return 0, true
	}
	return 0, false
}

func Score(cfg Config, a Assessment) Breakdown {
	var b Breakdown
	h := cfg.Household

	add := func(key string, raw float64, why string) {
		w := cfg.Weights[key]
		f := FactorScore{
			Key:    key,
			Label:  factorLabels[key],
			Group:  factorFor[key],
			Raw:    math.Max(0, math.Min(1, raw)),
			Weight: w,
			Why:    why,
		}
		f.Points = f.Raw * w
		b.Factors = append(b.Factors, f)
		b.Score += f.Points
	}

	// Same, for a factor nothing could be measured for. It scores nothing and its
	// weight comes out of the total rather than counting against the house.
	unknown := func(key, why string) {
		b.Factors = append(b.Factors, FactorScore{
			Key:     key,
			Label:   factorLabels[key],
			Group:   factorFor[key],
			Weight:  cfg.Weights[key],
			Why:     why,
			Unknown: true,
		})
	}

	// ---- Little Bear ----

	// Schools, the heaviest factor, scored on whatever is actually known rather
	// than on how forthcoming the county was. State grades are the best signal
	// where they exist, and failing that it is how close the schools are. Nothing
	// at all is unknown rather than bad.
	nearRaw, haveNear := a.Zones.SchoolsRaw()
	switch {
	case a.Grades.Have && gradesRaw(a.Grades) > 0:
		add("schools", gradesRaw(a.Grades), "state grades "+strings.Trim(strings.Join([]string{
			a.Grades.Elementary, a.Grades.Middle, a.Grades.High}, "/"), "/"))

	case haveNear && a.Zones.Verified && !a.Zones.Borrowed:
		// Confirmed zoning is worth a little on top, because the school it names is
		// the school the child actually goes to. A borrowed coordinate does not
		// earn that, since the distance is to whichever school was nearest.
		why := "zoned to " + schoolList(a.Zones) + ", " + schoolMiles(a.Zones)
		if a.Zones.K8 {
			why += ", and one K-8 school means no second move"
		}
		add("schools", math.Min(1, nearRaw+0.08), why)

	case haveNear && a.Zones.Borrowed:
		add("schools", nearRaw, "zoned to "+schoolList(a.Zones)+
			", but the county publishes no address for one of them, so "+schoolMiles(a.Zones)+
			" is to the nearest school of that level rather than the zoned one")

	case haveNear:
		add("schools", nearRaw, "nearest are "+schoolList(a.Zones)+", "+schoolMiles(a.Zones)+
			". This county does not publish which schools an address goes to, so ring them to be sure")

	case a.Zones.Verified:
		add("schools", 0.55, "we know which schools it goes to, the state has not published their report cards")

	default:
		unknown("schools", "we could not find the schools for this address yet")
	}

	// Somewhere to be taken on a Saturday. Not whether the house is sound, and very
	// much whether a ten year old is bored.
	if a.Outings.Partial && len(a.Outings.Nearest) == 0 {
		unknown("outings", "the map service did not answer, we will try again")
	} else {
		add("outings", a.Outings.Raw(), a.Outings.Why())
	}

	// ---- safe and sound ----

	// The heaviest of the safety factors. With no figures, scoring half marks
	// reads as an average place, which is a claim nobody made.
	if a.Area.Found && a.Area.CrimeSource != "" {
		add("crime", a.Area.CrimeRaw(), a.Area.CrimeWhy())
	} else {
		unknown("crime", "no crime figures for this county yet")
	}

	// Flood, measured against the SFHA and the year-round water rather than the
	// nearest ditch, which is normal here and says nothing.
	// A distance of notFound reads as nothing nearby, which is the right answer
	// when FEMA replied and the wrong one when it did not.
	if !a.Flood.Measured || a.Flood.Partial {
		unknown("flood", "we could not check the flood map, we will try again")
	} else {
		sfha := distanceRaw(a.Flood.SFHAFeet, 2640)
		water := distanceRaw(a.Flood.PerennialFeet, 1320)
		add("flood", math.Min(sfha, water), floodWhy(a.Flood))
	}

	// A quiet road, which on a house with a child in it is a safety factor rather
	// than a comfort one. The filter already removed anything fronting a main
	// road, so this ranks what is left on how much quieter than the line it is,
	// and a corner lot loses ground here as well as taking its penalty.
	if a.Road.Partial {
		unknown("quiet", "we could not check the road out front, we will try again")
	} else {
		add("quiet", quietRaw(cfg, a.Road), quietWhy(cfg, a.Road))
	}

	// How many neighbours there are, which is a different question from who they
	// are and pulls the other way: a street can be almost entirely owner occupied
	// and still have forty houses on it. Fewer is better.
	if a.Street.Found {
		add("elbow", a.Street.SpaceRaw(), a.Street.SpaceWhy())
	} else {
		unknown("elbow", "the county has too few records around here to count the neighbours")
	}

	// Distance from the interstate, which is a noise and a safety question rather
	// than an access one. Being able to reach a slip road is worth a few minutes
	// either way and living beside six lanes is not.
	if a.Road.Partial {
		unknown("highway", "we could not check for an interstate nearby, we will try again")
	} else {
		add("highway", highwayRaw(cfg.Filters, a.Road), highwayWhy(cfg.Filters, a.Road))
	}

	// A settled neighbourhood. Owner occupancy counted off the tax roll for the
	// houses actually within sight of this one, which is what people mean by the
	// phrase, and the county wide census figures only when the parcels could not
	// be read. Nothing here is about who the neighbours are.
	switch {
	case a.Street.Found:
		add("community", a.Street.Raw(), a.Street.Why())
	case a.Area.Found && a.Area.OwnerOccupiedPct > 0:
		add("community", a.Area.CommunityRaw(), a.Area.CommunityWhy())
	default:
		unknown("community", "no figures for who lives around here yet")
	}

	// ---- Mama Bear ----

	// No drive at all means the routing did not answer, not that there is nowhere
	// to work. Scoring that zero was an outage reading as the worst house on the
	// list, on eighteen points between the two.
	if haveKind(a.Drives, "work") {
		add("partnerwork", nearestRaw(a.Drives, "work", 10, 40),
			nearestWhy(a.Drives, "work", "licensed nursing and long term care"))
	} else {
		unknown("partnerwork", "we could not work out the drive to the nursing homes")
	}
	if haveKind(a.Drives, "school") {
		add("partnerstudy", nearestRaw(a.Drives, "school", 15, 50),
			nearestWhy(a.Drives, "school", "nursing school"))
	} else {
		unknown("partnerstudy", "we could not work out the drive to the nursing schools")
	}

	// Land, which is three questions. Acreage is what is reported, flat ground is
	// what a raised bed and a chicken run actually need, and a fence is what the
	// dog needs. It sits with her rather than with the child, because the garden
	// and the animals are hers.
	add("land", landRaw(h, a), landWhy(h, a))

	// ---- the whole den ----

	// All-in monthly against the target rather than the ceiling, so the gap
	// between the two is where the ranking happens.
	if a.Monthly.Total > 0 {
		add("monthly", comfortBand(a.Monthly.Total, cfg.Money.MonthlyTarget, cfg.Money.MonthlyCeiling),
			monthlyWhy(a.Monthly.Total, cfg.Money.MonthlyTarget))
	} else {
		unknown("monthly", "no asking price yet, so we cannot work the payment out")
	}

	// ---- Papa Bear, last ----

	switch {
	case a.Morning.Partial:
		unknown("dropoff", "we could not work out the school run for this one")
	default:
		add("dropoff", band(a.Morning.DetourMin, 0, h.DropoffBadMin),
			fmtMinutes(a.Morning.DetourMin)+" on top of the commute")
	}

	if a.Morning.Commute.Minutes > 0 {
		add("commute", band(a.Morning.Commute.Minutes, cfg.Geography.IdealMinutes*0.5, cfg.Geography.MaxMinutes),
			fmtMinutes(a.Morning.Commute.Minutes)+" to "+orUnnamed(cfg.Geography.OriginName, "work"))
	} else {
		unknown("commute", "we could not work out the drive to work")
	}

	// A factor nothing would answer for is left out of the denominator entirely,
	// so a county server having a bad morning cannot lower a house's score.
	//
	// A house with gaps is scored on less, so the page says how much it was
	// measured on and the gaps get retried on their own until they fill in.
	b.Total = cfg.WeightTotal()
	if measured := b.Measured(); measured > 0 {
		b.Gross = b.Score / measured * 100
	}

	// Then the wants come off the top. A corner lot or an old doublewide does not
	// sink an otherwise perfect house, so it is points and a chip on the card
	// rather than a row in the drawer.
	b.Penalties = Penalties(a.Flags)
	b.Docked = PenaltyTotal(a.Flags)
	b.Score = math.Max(0, b.Gross-b.Docked)

	// An excluded house keeps its score. Zeroing it drew an empty dial reading 0
	// over a breakdown that said sixty-something, and scored 62 but ruled out on
	// one rule is a different house from scored 12.
	b.Out = len(a.Excluded) > 0
	return b
}

func quietRaw(cfg Config, r RoadResult) float64 {
	var raw float64
	switch {
	case r.AADT == 0 && r.Class != "":
		raw = 0.9
	case r.AADT == 0:
		raw = 0.6
	default:
		raw = band(float64(r.AADT), 300, float64(cfg.Filters.AADTCutoff))
	}
	// Road on more than one side is more traffic past the house whatever the
	// count on the front says.
	if r.Corner() {
		raw *= 0.6
	}
	return raw
}

func quietWhy(cfg Config, r RoadResult) string {
	var base string
	switch {
	case r.AADT == 0 && r.Class != "":
		base = "no traffic count nearby, and the frontage is " + r.Class
	case r.AADT == 0:
		base = "no traffic count and no road class found"
	default:
		base = commaInt(r.AADT) + " a day on " + orUnnamed(r.AADTRoute, r.RoadName)
	}
	if r.Corner() {
		base += ", and road on more than one side"
	}
	return base
}

// nearestRaw scores the nearest few destinations of one kind. The shifts behind
// these are nights and every other weekend, so this is distance and route
// simplicity rather than rush hour time.
func nearestRaw(drives map[string]Leg, kind string, best, worst float64) float64 {
	mins := legsOfKind(drives, kind)
	if len(mins) == 0 {
		return 0
	}
	sortFloats(mins)
	if len(mins) > 3 {
		mins = mins[:3]
	}
	var sum float64
	for _, m := range mins {
		sum += m
	}
	return band(sum/float64(len(mins)), best, worst)
}

func nearestWhy(drives map[string]Leg, kind, what string) string {
	mins := legsOfKind(drives, kind)
	if len(mins) == 0 {
		return "nothing of this kind configured or reachable"
	}
	sortFloats(mins)
	return "nearest " + what + " " + fmtMinutes(mins[0]) + " away"
}

// Each leg carries what its destination is, so a hospital counts as somewhere to
// work and a community college counts as somewhere to study. Guessing from the
// key put the two together and scored her work access off a college campus.
func legsOfKind(drives map[string]Leg, want string) []float64 {
	var out []float64
	for _, leg := range drives {
		if leg.Minutes <= 0 {
			continue
		}
		switch want {
		case "work":
			if leg.Kind == "employer" {
				out = append(out, leg.Minutes)
			}
		case "school":
			if leg.Kind == "school" {
				out = append(out, leg.Minutes)
			}
		}
	}
	return out
}

// The split inside the land factor. Acreage is most of it, flat ground is most of
// the rest, and the fence is a bonus: a listing that does not mention one may
// still have one, so its absence must not cost much.
const (
	acreShare  = 0.55
	flatShare  = 0.30
	fenceShare = 0.15
)

func landRaw(h Household, a Assessment) float64 {
	acres := a.Listing.Acres
	var acreRaw float64
	if acres > 0 {
		// No bonus past two acres: the chickens and the beds do not need more, and
		// more land past that is mowing.
		acreRaw = band(math.Min(acres, 2), h.LotIdealAcres, 0.1)
	} else {
		acreRaw = 0.2
	}

	// Unknown terrain scores mid rather than zero, since a lot whose slope could
	// not be sampled is not thereby a hillside.
	flat := 0.5
	if !a.Terrain.Partial && a.Terrain.FlatShare > 0 {
		flat = a.Terrain.FlatShare
	}

	fence := 0.0
	if a.Fence != "" {
		fence = 1
	}

	return acreRaw*acreShare + flat*flatShare + fence*fenceShare
}

func landWhy(h Household, a Assessment) string {
	var parts []string
	if a.Listing.Acres > 0 {
		parts = append(parts, trimFloat(a.Listing.Acres)+" acres")
	} else {
		parts = append(parts, "lot size not reported")
	}
	if a.Terrain.Partial || a.Terrain.FlatShare == 0 {
		parts = append(parts, "slope not sampled")
	} else {
		parts = append(parts, trimFloat(math.Round(a.Terrain.FlatShare*100))+"% of the yard flat enough to build on")
	}
	if a.Fence != "" {
		parts = append(parts, "a fence is mentioned")
	} else {
		parts = append(parts, "no fence mentioned")
	}
	return strings.Join(parts, ", ")
}

// highwayRaw scores the distance from the pavement, not from the slip road.
// Under the close figure is near enough to hear it, past the comfortable one it
// stops mattering, and there is no extra credit for being further still.
func highwayRaw(f Filters, r RoadResult) float64 {
	if !r.Measured {
		return 0.5
	}
	if r.HighwayFeet == notFound {
		// Nothing within two miles, which is the quiet answer.
		return 1
	}
	return band(r.HighwayFeet, f.HighwayComfyFeet, f.HighwayTooCloseFeet*0.35)
}

func highwayWhy(f Filters, r RoadResult) string {
	name := orUnnamed(r.HighwayName, "the nearest interstate")
	switch {
	case !r.Measured:
		return "not measured"
	case r.HighwayFeet == notFound:
		return "no interstate within two miles"
	case r.HighwayFeet < f.HighwayTooCloseFeet:
		return feet(r.HighwayFeet) + " from " + name + ", close enough to hear it"
	default:
		return feet(r.HighwayFeet) + " from " + name +
			", and " + rampNote(r)
	}
}

// The ramp is worth one clause and no more, since being able to get on it is the
// small half of this.
func rampNote(r RoadResult) string {
	if r.RampFeet == notFound {
		return "no slip road within two miles"
	}
	return feet(r.RampFeet) + " to a slip road"
}

// distanceRaw maps a distance to 0..1 where notFound, meaning nothing of that
// kind inside the search radius, is the best possible answer.
func distanceRaw(feet, ideal float64) float64 {
	if feet == notFound {
		return 1
	}
	return band(feet, ideal, 0)
}

func floodWhy(f FloodResult) string {
	switch {
	case f.SFHA:
		return "inside a FEMA special flood hazard area"
	case f.SFHAFeet == notFound && f.PerennialFeet == notFound:
		return "no mapped flood zone or year-round water within a mile"
	case f.SFHAFeet == notFound:
		return "no flood zone within a mile, " + orUnnamed(f.PerennialName, "year-round water") +
			" " + feet(f.PerennialFeet) + " away"
	default:
		return "flood zone " + feet(f.SFHAFeet) + " away, year-round water " + feet(f.PerennialFeet)
	}
}

func sortFloats(v []float64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
