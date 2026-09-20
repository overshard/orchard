package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The read side. One row type feeds the grid, the detail view and the filtered
// out drawer, so a number cannot read one way on a card and another way behind it.
type Card struct {
	ID int64
	// What a link carries. The row id never leaves the process.
	PublicID string
	MLS      string
	Address  string
	City     string
	State    string
	Zip      string
	County   string
	Lat      float64
	Lon      float64

	Price      int
	Beds       float64
	Baths      float64
	SqFt       int
	Acres      float64
	YearBuilt  int
	Style      string
	Status     string
	TaxAnnual  float64
	HOAMonthly float64
	URL        string
	Remarks    string

	Photos []string

	Score     float64
	Breakdown Breakdown

	MonthlyTotal float64
	MonthlyDPA   float64
	MonthlyPI    float64
	MonthlyTax   float64
	MonthlyIns   float64
	MonthlyPMI   float64
	MonthlyHOA   float64
	MonthlyUtil  float64

	CommuteMin      float64
	DetourMin       float64
	MiddleDetourMin float64
	HighDetourMin   float64
	OppositeWays    bool

	Elementary     string
	Middle         string
	High           string
	ZoningSource   string
	ZoningVerified bool
	ZoningNearest  bool
	ElemMiles      float64
	MiddleMiles    float64
	HighMiles      float64
	ElemGrade      string
	MiddleGrade    string
	HighGrade      string

	FloodZone     string
	FloodSFHA     bool
	WaterFeet     float64
	WaterName     string
	WaterKind     string
	PerennialFeet float64
	PerennialName string
	DitchFeet     float64

	MarketValue   float64
	LandValue     float64
	AcresFrom     string
	ParcelAddress string
	Value         ValueEstimate
	ParcelLat     float64
	ParcelLon     float64
	Zoning        string
	Area          AreaProfile

	ReliefFeet   float64
	MeanSlopePct float64
	FlatShare    float64
	Fence        string
	Outings      OutingsResult
	Street       StreetResult

	AADT          int
	AADTRoute     string
	RoadClass     string
	RoadClassFeet float64
	Corner        bool
	CornerRoads   string
	RampFeet      float64
	RampName      string
	HighwayFeet   float64
	HighwayName   string

	Excluded bool
	Reasons  []string
	Stretch  bool

	ComputedAt int64
	FirstSeen  int64
	LastSeen   int64
	DaysListed int
	PriceFrom  int
	Rating     int
	Note       string
	Drives     []DriveRow
}

type DriveRow struct {
	Key     string
	Name    string
	Who     string
	Kind    string
	Minutes float64
	Miles   float64
}

// IsNew is what the NEW badge reads. Anything first seen inside this window is
// new, which is generous, since being a day late to a listing in this
// price band is how you lose it.
const newWindow = 7 * 24 * time.Hour

func (c Card) IsNew() bool {
	return time.Since(time.Unix(c.FirstSeen, 0)) < newWindow
}

func (c Card) PriceDropped() bool {
	return c.PriceFrom > 0 && c.Price < c.PriceFrom
}

func (c Card) DropPercent() float64 {
	if c.PriceFrom == 0 {
		return 0
	}
	return float64(c.PriceFrom-c.Price) / float64(c.PriceFrom) * 100
}

// Pending reports that the background assessment has not written a facts row yet,
// which is the state a freshly pasted address is in for the minute or so the
// paced lookups take.
func (c Card) Pending() bool { return c.ComputedAt == 0 }

// ValueGapNote compares the asking price with what the county has the place down
// as, which is the closest thing to a free appraisal. An assessment is from the
// last revaluation and counties here revalue every four years, so it lags a
// rising market badly.
// ParcelMismatch reports that the parcel we matched is not the address that was
// typed. The geocoder puts the point in the road and the nearest parcel is
// sometimes next door.
func (c Card) ParcelMismatch() bool {
	if c.ParcelAddress == "" {
		return false
	}
	return addressKey(c.ParcelAddress, "") != addressKey(c.Address, "")
}

// AskingGapPct is how far the asking price sits from the carried-forward
// estimate, which is a fairer comparison than the raw assessment because the
// assessment is as of a revaluation that can be years old.
func (c Card) AskingGapPct() float64 {
	if !c.Value.Found || c.Value.Estimate <= 0 || c.Price <= 0 {
		return 0
	}
	return (float64(c.Price) - c.Value.Estimate) / c.Value.Estimate * 100
}

// AskingVerdict is the one line that answers is this priced right.
func (c Card) AskingVerdict() string {
	if !c.Value.Found || c.Price <= 0 {
		return ""
	}
	pct := c.AskingGapPct()
	switch {
	case float64(c.Price) < c.Value.Low:
		return fmt.Sprintf("asking %.0f%% under the estimate, worth a look at why", -pct)
	case float64(c.Price) > c.Value.High:
		return fmt.Sprintf("asking %.0f%% over the estimate", pct)
	default:
		return "asking inside the estimate"
	}
}

func (c Card) ValueGapNote() string {
	if c.MarketValue <= 0 || c.Price <= 0 {
		return ""
	}
	if c.ParcelMismatch() {
		return "but that is the record for " + c.ParcelAddress
	}
	diff := float64(c.Price) - c.MarketValue
	pct := diff / c.MarketValue * 100
	switch {
	case pct > 12:
		return fmt.Sprintf("asking %.0f%% over, and an assessment lags the market", pct)
	case pct < -8:
		return fmt.Sprintf("asking %.0f%% under what it is assessed at", -pct)
	default:
		return "asking within a whisker of it"
	}
}

// Photo is the card's dominant element, so a listing with no photo gets a marker
// the template can style rather than a broken image.
func (c Card) HasPhotos() bool { return len(c.Photos) > 0 }

type Query struct {
	View     string // grid, new, drops, filtered, stretch
	Sort     string // score, price, monthly, detour, newest, oldest
	MinBeds  float64
	MinAcres float64
	MaxPrice int
	County   string
	Rated    string // any, liked, unrated
}

const cardColumns = `
	l.id, COALESCE(l.public_id,''), COALESCE(l.mls,''), l.address, COALESCE(l.city,''),
	COALESCE(l.state,''), COALESCE(l.zip,''), COALESCE(l.county,''),
	COALESCE(l.lat,0), COALESCE(l.lon,0),
	l.price, COALESCE(l.beds,0), COALESCE(l.baths,0), COALESCE(l.sqft,0), COALESCE(l.acres,0),
	COALESCE(l.year_built,0), COALESCE(l.style,''), COALESCE(l.status,''),
	COALESCE(l.tax_annual,0), COALESCE(l.hoa_monthly,0),
	COALESCE(l.listing_url,''), COALESCE(l.remarks,''),
	l.first_seen, l.last_seen,
	COALESCE(f.computed_at,0), COALESCE(f.score,0), COALESCE(f.breakdown,'{}'),
	COALESCE(f.monthly_total,0), COALESCE(f.monthly_dpa,0), COALESCE(f.monthly_pi,0),
	COALESCE(f.monthly_tax,0), COALESCE(f.monthly_ins,0), COALESCE(f.monthly_pmi,0),
	COALESCE(f.monthly_hoa,0), COALESCE(f.monthly_util,0),
	COALESCE(f.commute_min,0), COALESCE(f.detour_min,0), COALESCE(f.middle_detour_min,0), COALESCE(f.high_detour_min,0),
	COALESCE(f.opposite_ways,0),
	COALESCE(f.elem_name,''), COALESCE(f.middle_name,''), COALESCE(f.high_name,''),
	COALESCE(f.zoning_source,''), COALESCE(f.zoning_verified,0),
	COALESCE(f.zoning_nearest,0), COALESCE(f.elem_miles,0),
	COALESCE(f.middle_miles,0), COALESCE(f.high_miles,0),
	COALESCE(f.elem_grade,''), COALESCE(f.middle_grade,''), COALESCE(f.high_grade,''),
	COALESCE(f.flood_zone,''), COALESCE(f.flood_sfha,0), COALESCE(f.water_feet,-1),
	COALESCE(f.water_name,''), COALESCE(f.water_kind,''),
	COALESCE(f.perennial_feet,-1), COALESCE(f.perennial_name,''), COALESCE(f.ditch_feet,-1),
	COALESCE(f.market_value,0), COALESCE(f.land_value,0), COALESCE(f.acres_from,''),
	COALESCE(f.parcel_address,''), COALESCE(f.value_json,''), COALESCE(f.parcel_lat,0), COALESCE(f.parcel_lon,0),
	COALESCE(f.zoning,''), COALESCE(f.area_json,'{}'),
	COALESCE(f.relief_feet,0), COALESCE(f.mean_slope_pct,0), COALESCE(f.flat_share,0),
	COALESCE(f.fence,''), COALESCE(f.outings_json,'{}'), COALESCE(f.street_json,'{}'),
	COALESCE(f.aadt,0), COALESCE(f.aadt_route,''),
	COALESCE(f.road_class,''), COALESCE(f.road_class_feet,-1),
	COALESCE(f.corner,0), COALESCE(f.corner_roads,''),
	COALESCE(f.ramp_feet,-1), COALESCE(f.ramp_name,''),
	COALESCE(f.highway_feet,-1), COALESCE(f.highway_name,''),
	COALESCE(f.excluded,0), COALESCE(f.exclude_reasons,'[]'), COALESCE(f.stretch,0),
	COALESCE(v.rating,0), COALESCE(v.note,'')`

func scanCard(rows *sql.Rows) (Card, error) {
	var c Card
	var breakdown, reasons, areaJSON, valueJSON, outingsJSON, streetJSON string
	err := rows.Scan(
		&c.ID, &c.PublicID, &c.MLS, &c.Address, &c.City, &c.State, &c.Zip, &c.County, &c.Lat, &c.Lon,
		&c.Price, &c.Beds, &c.Baths, &c.SqFt, &c.Acres, &c.YearBuilt, &c.Style, &c.Status, &c.TaxAnnual, &c.HOAMonthly,
		&c.URL, &c.Remarks, &c.FirstSeen, &c.LastSeen,
		&c.ComputedAt, &c.Score, &breakdown,
		&c.MonthlyTotal, &c.MonthlyDPA, &c.MonthlyPI, &c.MonthlyTax, &c.MonthlyIns,
		&c.MonthlyPMI, &c.MonthlyHOA, &c.MonthlyUtil,
		&c.CommuteMin, &c.DetourMin, &c.MiddleDetourMin, &c.HighDetourMin, &c.OppositeWays,
		&c.Elementary, &c.Middle, &c.High, &c.ZoningSource, &c.ZoningVerified,
		&c.ZoningNearest, &c.ElemMiles, &c.MiddleMiles, &c.HighMiles,
		&c.ElemGrade, &c.MiddleGrade, &c.HighGrade,
		&c.FloodZone, &c.FloodSFHA, &c.WaterFeet, &c.WaterName, &c.WaterKind,
		&c.PerennialFeet, &c.PerennialName, &c.DitchFeet,
		&c.MarketValue, &c.LandValue, &c.AcresFrom, &c.ParcelAddress, &valueJSON, &c.ParcelLat, &c.ParcelLon, &c.Zoning, &areaJSON,
		&c.ReliefFeet, &c.MeanSlopePct, &c.FlatShare, &c.Fence, &outingsJSON, &streetJSON,
		&c.AADT, &c.AADTRoute,
		&c.RoadClass, &c.RoadClassFeet, &c.Corner, &c.CornerRoads, &c.RampFeet, &c.RampName, &c.HighwayFeet, &c.HighwayName,
		&c.Excluded, &reasons, &c.Stretch, &c.Rating, &c.Note)
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal([]byte(areaJSON), &c.Area)
	_ = json.Unmarshal([]byte(valueJSON), &c.Value)
	_ = json.Unmarshal([]byte(outingsJSON), &c.Outings)
	_ = json.Unmarshal([]byte(streetJSON), &c.Street)
	_ = json.Unmarshal([]byte(breakdown), &c.Breakdown)
	_ = json.Unmarshal([]byte(reasons), &c.Reasons)
	c.DaysListed = int(time.Since(time.Unix(c.FirstSeen, 0)).Hours() / 24)
	return c, nil
}

var sortClauses = map[string]string{
	"score":   "COALESCE(f.score,0) DESC, l.price ASC",
	"price":   "l.price ASC",
	"monthly": "CASE WHEN COALESCE(f.monthly_total,0) = 0 THEN 1 ELSE 0 END, f.monthly_total ASC",
	"detour":  "CASE WHEN COALESCE(f.detour_min,0) = 0 THEN 1 ELSE 0 END, f.detour_min ASC",
	"newest":  "l.first_seen DESC",
	"oldest":  "l.first_seen ASC",
	"acres":   "COALESCE(l.acres,0) DESC",
}

// Cards runs one query for whichever view is asked for. The views are different
// WHERE clauses over the same rows rather than different pages, so a card looks
// the same wherever it appears.
func Cards(ctx context.Context, db *sql.DB, q Query) ([]Card, error) {
	where := []string{"l.gone_at IS NULL"}
	var args []any

	switch q.View {
	case "filtered":
		where = append(where, "COALESCE(f.excluded,0) = 1")
	case "stretch":
		where = append(where, "COALESCE(f.excluded,0) = 0", "COALESCE(f.stretch,0) = 1")
	case "new":
		where = append(where, "COALESCE(f.excluded,0) = 0",
			fmt.Sprintf("l.first_seen > %d", time.Now().Add(-newWindow).Unix()))
	case "drops":
		where = append(where, "COALESCE(f.excluded,0) = 0", `l.id IN (
			SELECT listing_id FROM price_history GROUP BY listing_id
			HAVING MAX(price) > MIN(price))`)
	default:
		where = append(where, "COALESCE(f.excluded,0) = 0", "COALESCE(f.stretch,0) = 0")
	}

	if q.MinBeds > 0 {
		where = append(where, "COALESCE(l.beds,0) >= ?")
		args = append(args, q.MinBeds)
	}
	if q.MinAcres > 0 {
		where = append(where, "COALESCE(l.acres,0) >= ?")
		args = append(args, q.MinAcres)
	}
	if q.MaxPrice > 0 {
		where = append(where, "l.price <= ?")
		args = append(args, q.MaxPrice)
	}
	if q.County != "" {
		where = append(where, "LOWER(COALESCE(l.county,'')) = LOWER(?)")
		args = append(args, q.County)
	}
	switch q.Rated {
	case "liked":
		where = append(where, "COALESCE(v.rating,0) > 0")
	case "unrated":
		where = append(where, "COALESCE(v.rating,0) = 0")
	case "hidden":
		where = append(where, "COALESCE(v.rating,0) < 0")
	}
	// A thumbs down is a decision, so it stays out of every view but its own.
	if q.Rated != "hidden" {
		where = append(where, "COALESCE(v.rating,0) >= 0")
	}

	order, ok := sortClauses[q.Sort]
	if !ok {
		order = sortClauses["score"]
		if q.View == "new" || q.View == "drops" {
			order = sortClauses["newest"]
		}
	}

	sqlText := `SELECT ` + cardColumns + `
		FROM listings l
		LEFT JOIN facts f ON f.listing_id = l.id
		LEFT JOIN verdicts v ON v.listing_id = l.id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + ` LIMIT 500`

	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return withExtras(ctx, db, out)
}

// OneCard is the detail view, which needs the same row plus the photo set and
// every drive.
func OneCard(ctx context.Context, db *sql.DB, id int64) (Card, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+cardColumns+`
		FROM listings l
		LEFT JOIN facts f ON f.listing_id = l.id
		LEFT JOIN verdicts v ON v.listing_id = l.id
		WHERE l.id = ?`, id)
	if err != nil {
		return Card{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Card{}, sql.ErrNoRows
	}
	c, err := scanCard(rows)
	if err != nil {
		return Card{}, err
	}
	rows.Close()

	out, err := withExtras(ctx, db, []Card{c})
	if err != nil || len(out) == 0 {
		return c, err
	}
	return out[0], nil
}

// withExtras fills the photo list, the original price and the drives in bulk,
// three queries for the whole page rather than three per card.
func withExtras(ctx context.Context, db *sql.DB, cards []Card) ([]Card, error) {
	if len(cards) == 0 {
		return cards, nil
	}
	index := map[int64]int{}
	for i, c := range cards {
		index[c.ID] = i
	}

	photos, err := db.QueryContext(ctx, `SELECT listing_id, url FROM photos ORDER BY listing_id, idx`)
	if err != nil {
		return nil, err
	}
	for photos.Next() {
		var id int64
		var u string
		if err := photos.Scan(&id, &u); err != nil {
			photos.Close()
			return nil, err
		}
		if i, ok := index[id]; ok {
			cards[i].Photos = append(cards[i].Photos, u)
		}
	}
	photos.Close()

	// The first price ever seen, which is what a drop is measured from.
	prices, err := db.QueryContext(ctx,
		`SELECT listing_id, price FROM price_history ph WHERE seen_at = (
			SELECT MIN(seen_at) FROM price_history WHERE listing_id = ph.listing_id)`)
	if err != nil {
		return nil, err
	}
	for prices.Next() {
		var id int64
		var p int
		if err := prices.Scan(&id, &p); err != nil {
			prices.Close()
			return nil, err
		}
		if i, ok := index[id]; ok {
			cards[i].PriceFrom = p
		}
	}
	prices.Close()

	drives, err := db.QueryContext(ctx,
		`SELECT listing_id, place_key, minutes, miles FROM drives ORDER BY minutes`)
	if err != nil {
		return nil, err
	}
	defer drives.Close()
	for drives.Next() {
		var id int64
		var row DriveRow
		if err := drives.Scan(&id, &row.Key, &row.Minutes, &row.Miles); err != nil {
			return nil, err
		}
		// A facility's key is "snf:<kind>:<name>", which carries everything the
		// row needs to render without a join onto a table of its own.
		if rest, ok := strings.CutPrefix(row.Key, "snf:"); ok {
			kind, name, found := strings.Cut(rest, ":")
			if found {
				row.Kind, row.Name = kind, name
			} else {
				row.Name = rest
			}
			row.Who = "Mama Bear"
		}
		if i, ok := index[id]; ok {
			cards[i].Drives = append(cards[i].Drives, row)
		}
	}
	return cards, drives.Err()
}

// Counts is the number beside each view's tab, so the drawer says how many were
// filtered out without opening it.
type Counts struct {
	Grid     int
	New      int
	Drops    int
	Stretch  int
	Filtered int
	Total    int
}

func ViewCounts(ctx context.Context, db *sql.DB) (Counts, error) {
	var c Counts
	since := time.Now().Add(-newWindow).Unix()
	err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN COALESCE(f.excluded,0)=0 AND COALESCE(f.stretch,0)=0 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN COALESCE(f.excluded,0)=0 AND l.first_seen > ? THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN COALESCE(f.excluded,0)=0 AND COALESCE(f.stretch,0)=1 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN COALESCE(f.excluded,0)=1 THEN 1 ELSE 0 END),0)
		FROM listings l
		LEFT JOIN facts f ON f.listing_id = l.id
		LEFT JOIN verdicts v ON v.listing_id = l.id
		WHERE l.gone_at IS NULL AND COALESCE(v.rating,0) >= 0`, since).
		Scan(&c.Total, &c.Grid, &c.New, &c.Stretch, &c.Filtered)
	if err != nil {
		return c, err
	}

	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM listings l
		LEFT JOIN facts f ON f.listing_id = l.id
		LEFT JOIN verdicts v ON v.listing_id = l.id
		WHERE l.gone_at IS NULL AND COALESCE(v.rating,0) >= 0
		  AND COALESCE(f.excluded,0) = 0
		  AND l.id IN (SELECT listing_id FROM price_history GROUP BY listing_id
		               HAVING MAX(price) > MIN(price))`).Scan(&c.Drops)
	return c, err
}

// Counties is the filter dropdown, built from what is actually in the database
// rather than from the config list, so it never offers an empty option.
func Counties(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT county FROM listings WHERE county IS NOT NULL AND county != ''
		 AND gone_at IS NULL ORDER BY county`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LastRun is what the header shows, so a stale dashboard says so rather than
// looking current.
type Run struct {
	StartedAt  int64
	FinishedAt int64
	Seen       int
	Added      int
	Changed    int
	Note       string
}

func LastRun(ctx context.Context, db *sql.DB) (Run, bool) {
	var r Run
	var finished sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT started_at, finished_at, seen, added, changed, note FROM runs
		 ORDER BY id DESC LIMIT 1`).Scan(&r.StartedAt, &finished, &r.Seen, &r.Added, &r.Changed, &r.Note)
	if err != nil {
		return r, false
	}
	r.FinishedAt = finished.Int64
	return r, true
}

// SetVerdict records a thumbs up or down and a note. A refresh never touches this
// table, which is the point of it being its own table.
func SetVerdict(ctx context.Context, db *sql.DB, id int64, rating int) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO verdicts (listing_id, rating, updated_at) VALUES (?,?,?)
		ON CONFLICT(listing_id) DO UPDATE SET
			rating = excluded.rating, updated_at = excluded.updated_at`,
		id, rating, time.Now().Unix())
	return err
}

// Verdict is the sentence the whole report exists to produce. Everything else on
// the page is the working, and somebody standing in a driveway with a phone wants
// the answer first and the working only if they doubt it.
type Call struct {
	Say     string
	Because string
}

func (c Card) Verdict() Call {
	if c.Excluded {
		// The reasons are listed under this, so repeating the first one here said
		// the same sentence twice.
		if len(c.Reasons) > 0 {
			return Call{Say: "Give it a miss"}
		}
		return Call{"Give it a miss", "it breaks one of the rules we set"}
	}

	best, worst := c.bestAndWorst()
	switch {
	case c.Score >= 75:
		return Call{"Go and see it", best}
	case c.Score >= 60:
		return Call{"Worth a look", best}
	case c.Score >= 45:
		return Call{"Only if nothing better turns up", worst}
	default:
		return Call{"Probably not", worst}
	}
}

// The strongest and the weakest thing the report found, in plain words, so the
// verdict carries a reason rather than only a mood.
func (c Card) bestAndWorst() (string, string) {
	var best, worst *FactorScore
	for i := range c.Breakdown.Factors {
		f := &c.Breakdown.Factors[i]
		if f.Weight <= 0 || f.Unknown {
			continue
		}
		if best == nil || f.Points/f.Weight > best.Points/best.Weight {
			best = f
		}
		if worst == nil || f.Points/f.Weight < worst.Points/worst.Weight {
			worst = f
		}
	}
	b, w := "it does well on most of what we care about", "it is thin in a few places"
	if best != nil {
		b = strings.ToLower(best.Label) + ": " + best.Why
	}
	if worst != nil {
		w = strings.ToLower(worst.Label) + ": " + worst.Why
	}
	return b, w
}

// listingIDFor turns the token a link carries into the row id everything here
// actually keys on. Nothing outside this function should ever see the integer.
func listingIDFor(ctx context.Context, db *sql.DB, publicID string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM listings WHERE public_id = ?`, publicID).Scan(&id)
	return id, err
}
