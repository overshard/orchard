package property

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

// One report is far more than a small model should be handed to answer "is it in
// a flood zone". The whole thing runs to several thousand tokens and a
// conversation about one house is a dozen questions, so the tool takes an aspect
// and this is what each one is.
//
// Every value here is already in plain words with its unit in the key, because a
// model handed `{"sfha_feet": -1}` writes that the house is minus one foot from a
// flood zone.

// Aspects is every slice a caller may ask for, which is also the enum the model
// is handed.
var Aspects = []string{
	"summary", "cost", "flood", "road", "schools", "commutes",
	"area", "land", "neighbours", "outings", "links",
}

// everything is accepted and not offered. A small model handed it in the enum
// reaches for it on the first question, and what it used to return was the whole
// struct: eleven thousand characters of Go field names and sentinel distances,
// which came back out as "Palmer Place, residential, 19 feet class". It returns
// the written sections now, and the enum leaves it out so summary stays the
// default for a first look.

// Aspect returns one part of the report. An unknown name comes back with the list
// rather than an error, since a model that invents a section has still said what
// it wants and the list is what gets it there.
func (r *Report) Aspect(name string) any {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "summary":
		return r.Summary()
	case "cost", "loans", "mortgage", "money", "rates", "payment":
		return r.Cost()
	case "flood", "water":
		return r.FloodPart()
	case "road", "roads", "traffic", "noise":
		return r.RoadPart()
	case "schools", "school":
		return r.SchoolsPart()
	// commutes rather than drives: asked for the drives section the model
	// answered about the driveway and how many cars fit on it.
	case "commutes", "commute", "drives", "drive", "traffic_times":
		return r.DrivesPart()
	case "area", "demographics", "crime", "census":
		return r.AreaPart()
	case "land", "lot", "terrain", "acreage", "value":
		return r.LandPart()
	case "neighbours", "neighbors", "street", "owners":
		return r.NeighboursPart()
	case "outings", "things_to_do", "recreation":
		return r.OutingsPart()
	case "links":
		return map[string]any{"links": r.Links()}
	case "everything", "all", "full":
		// Not in the enum, so a model asking for it is guessing, and what it got
		// buried the table eleven sections deep and filled the window. The
		// summary is already a line on every part of the house.
		out := r.Summary()
		out["note"] = "there is no everything section. This is the summary, which covers every part of " +
			"the house in a line. Ask for one of the other sections by name when a question needs the detail."
		return out
	default:
		return map[string]any{
			"error":    "no section called " + name,
			"sections": Aspects,
		}
	}
}

func (r *Report) head() map[string]any {
	m := map[string]any{"address": r.Address}
	if r.County != "" {
		m["county"] = r.County
	}
	if r.Price > 0 {
		m["asking"] = "$" + comma(float64(r.Price))
	}
	if len(r.Missing) > 0 {
		m["could_not_reach"] = r.Missing
	}
	if len(r.Resting) > 0 {
		m["why"] = r.Resting
	}
	return m
}

// Summary is the one line per thing answer, for the first question about a house.
// Every number here is repeated in a section, so a follow up is a cheap read
// rather than another lookup.
func (r *Report) Summary() map[string]any {
	m := r.head()
	m["flood"] = r.floodLine()
	m["road"] = r.roadLine()
	m["schools"] = r.schoolLine()
	m["morning_drive"] = r.morningLine()
	m["lot"] = r.lotLine()
	m["neighbourhood"] = r.neighbourLine()
	m["usda_area"] = r.usdaLine()
	if r.Area.CrimeSource != "" {
		m["crime"] = r.crimeLine()
	}
	if best := r.cheapest(); best != nil {
		m["cheapest_loan"] = fmt.Sprintf("%s at $%s a month all in", best.Name, comma(best.Total))
	}
	// The table belongs here and not only in the cost section, because a first
	// question about a house gets the summary, and that is the answer that was
	// coming back as four paragraphs of prose with a figure buried in each.
	if t := r.CostTable(); t != "" {
		m["answer_with_this_table_exactly"] = t
	}
	if r.Value.Found {
		worth := fmt.Sprintf("about $%s, from a $%s assessment carried forward from %d",
			comma(r.Value.Estimate), comma(r.Value.Assessed), r.Value.BaseYear)
		if why := r.parcelCaveat(); why != "" {
			worth += ", except " + why
		}
		m["what_it_is_worth"] = worth
	}
	if r.Price <= 0 {
		m["note"] = "no asking price was given, so nothing about the money was worked out"
	}
	// Asked about a house with a price on it, the answer came back as four
	// paragraphs about loans and said nothing about the house, which is the half
	// a listing page cannot tell you and the reason any of this exists.
	m["how_to_answer"] = "Say what the house and the area are like first, in a few lines, " +
		"then print the table. Every line above is already checked, so use them rather than " +
		"picking one."
	m["config"] = r.ConfigLabel
	return m
}

// budgetLine measures the cheapest payment against what he said he wanted to
// spend. It is written in the second person because the model reads it back
// almost verbatim, and in the third it told him what "he" was aiming for. Without it in the answer the model invented a range and reported being
// inside it, which is the worst kind of wrong: specific, plausible, and about his
// own money.
func (r *Report) budgetLine() string {
	best := r.cheapest()
	if best == nil || r.MonthlyTarget <= 0 {
		return ""
	}
	switch {
	case best.Total <= r.MonthlyTarget:
		return fmt.Sprintf("the cheapest is $%s, inside the $%s a month you are aiming for",
			comma(best.Total), comma(r.MonthlyTarget))
	case r.MonthlyCeiling > 0 && best.Total <= r.MonthlyCeiling:
		return fmt.Sprintf("the cheapest is $%s, which is $%s over the $%s you are aiming for and still under your $%s ceiling",
			comma(best.Total), comma(best.Total-r.MonthlyTarget), comma(r.MonthlyTarget), comma(r.MonthlyCeiling))
	case r.MonthlyCeiling > 0:
		return fmt.Sprintf("the cheapest is $%s, which is $%s over your $%s ceiling",
			comma(best.Total), comma(best.Total-r.MonthlyCeiling), comma(r.MonthlyCeiling))
	default:
		return fmt.Sprintf("the cheapest is $%s, which is $%s over the $%s a month you are aiming for",
			comma(best.Total), comma(best.Total-r.MonthlyTarget), comma(r.MonthlyTarget))
	}
}

// cheapest is the lowest all-in monthly among the loans he could actually get.
func (r *Report) cheapest() *Quote {
	var best *Quote
	for i := range r.Quotes {
		q := &r.Quotes[i]
		if !q.Eligible || q.Total <= 0 {
			continue
		}
		if best == nil || q.Total < best.Total {
			best = q
		}
	}
	return best
}

// Cost is the whole money answer: every programme, what the rate came from, and
// what was assumed rather than measured.
func (r *Report) Cost() map[string]any {
	m := r.head()
	if len(r.Quotes) == 0 {
		m["error"] = "no asking price, so there is nothing to price. Ask again with a price."
		return m
	}

	loans := make([]map[string]any, 0, len(r.Quotes))
	for _, q := range r.Quotes {
		loans = append(loans, quoteMap(q))
	}
	sort.SliceStable(loans, func(i, j int) bool {
		ai, _ := loans[i]["eligible"].(bool)
		aj, _ := loans[j]["eligible"].(bool)
		if ai != aj {
			return ai
		}
		return loans[i]["_total"].(float64) < loans[j]["_total"].(float64)
	})
	for i := range loans {
		delete(loans[i], "_total")
	}
	m["loans"] = loans
	// Handed a shape, the model reproduces the shape, and the all-in number means
	// nothing without the rows it is the sum of.
	if t := r.CostTable(); t != "" {
		m["answer_with_this_table_exactly"] = t
	}

	if r.Market.Found {
		m["rate_basis"] = fmt.Sprintf("Freddie Mac's weekly survey for %s, %.2f%% on a 30 year conventional and %.2f%% on a 15 year. Every programme rate below is that plus the spread it normally goes out at, so a lender's sheet on the day is the real number.",
			r.Market.Week, r.Market.Thirty, r.Market.Deuce)
	} else {
		m["rate_basis"] = "the rate survey could not be reached, so any rate below was passed in rather than looked up"
	}
	if b := r.budgetLine(); b != "" {
		m["against_his_budget"] = b
	}
	m["assumed"] = []string{
		fmt.Sprintf("insurance at $%s a year", comma(r.mustCfgInsurance())),
		"utilities and internet at the figures in the config",
		"closing costs at 3% of the price, which is a band and not a quote",
		"the county tax rate in the config, which moves at every revaluation",
	}
	return m
}

// mustCfgInsurance reads the insurance figure back off a quote, since a report
// carries no config of its own once it has been cached.
func (r *Report) mustCfgInsurance() float64 {
	for _, q := range r.Quotes {
		if q.Insurance > 0 {
			return q.Insurance * 12
		}
	}
	return 0
}

func quoteMap(q Quote) map[string]any {
	m := map[string]any{
		"loan":     q.Name,
		"eligible": q.Eligible,
		"_total":   q.Total,
	}
	if q.RatePct > 0 {
		m["rate_pct"] = q.RatePct
	}
	if len(q.Blockers) > 0 {
		m["cannot_use_because"] = q.Blockers
	}
	if len(q.Checks) > 0 {
		m["worth_checking"] = q.Checks
	}
	if q.Total <= 0 {
		return m
	}

	m["down_payment"] = fmt.Sprintf("$%s, %.3g%%", comma(q.DownPayment), q.DownPct)
	m["loan_amount"] = "$" + comma(q.Loan)
	if q.UpfrontFee > 0 {
		m["upfront_fee"] = fmt.Sprintf("$%s, %s", comma(q.UpfrontFee), q.UpfrontWhat)
	}
	m["cash_to_close"] = "$" + comma(q.CashToClose)
	m["monthly_all_in"] = "$" + comma(q.Total)
	m["monthly_breakdown"] = map[string]string{
		"principal_and_interest": "$" + comma(q.PrincipalInt),
		"property_tax":           "$" + comma(q.Tax) + ", " + q.TaxSource,
		"insurance":              "$" + comma(q.Insurance),
		"mortgage_insurance":     "$" + comma(q.MI) + ", " + q.MINote,
		"hoa":                    "$" + comma(q.HOA),
		"utilities_and_internet": "$" + comma(q.Utilities+q.Internet),
	}
	if q.BackDTI > 0 {
		m["debt_to_income"] = fmt.Sprintf("%.0f front, %.0f back, %s", q.FrontDTI, q.BackDTI, q.DTINote)
	}
	return m
}

func (r *Report) FloodPart() map[string]any {
	m := r.head()
	if !r.Flood.Measured {
		m["flood"] = "not measured, FEMA could not be reached"
		return m
	}
	m["fema_zone"] = orUnnamed(r.Flood.Zone, "no zone mapped here")
	m["fema_wording"] = r.Flood.Subtype
	m["in_a_special_flood_hazard_area"] = r.Flood.SFHA
	m["insurance_consequence"] = floodInsuranceNote(r.Flood)
	if !r.Flood.SFHA {
		m["distance_to_the_nearest_flood_zone"] = feetOrNone(r.Flood.SFHAFeet)
	}
	m["nearest_water"] = waterLine(r.Flood)
	m["nearest_year_round_water"] = namedFeet(r.Flood.PerennialFeet, r.Flood.PerennialName)
	m["nearest_wet_weather_stream"] = namedFeet(r.Flood.IntermittentFeet, r.Flood.IntermittentName)
	m["nearest_ditch"] = feetOrNone(r.Flood.DitchFeet)
	m["note"] = "a ditch is not a flood risk and nearly every rural parcel here has one, which is why the three kinds of water are kept apart"
	if r.Flood.Partial {
		m["incomplete"] = true
	}
	return m
}

func floodInsuranceNote(f FloodResult) string {
	if f.SFHA {
		return "inside an SFHA, so a lender will require flood insurance on any of these loans"
	}
	return "outside an SFHA, so flood insurance is optional and cheap on a preferred risk policy"
}

func (r *Report) RoadPart() map[string]any {
	m := r.head()
	if !r.Road.Measured {
		m["road"] = "not measured, the road data could not be reached"
		return m
	}
	m["fronts_on"] = orUnnamed(r.Road.RoadName, "an unnamed road")
	m["road_class"] = orUnnamed(r.Road.Class, "not classified, so a small road")
	m["how_far_that_road_is"] = feetOrNone(r.Road.ClassFeet)
	if r.Road.AADT > 0 {
		m["traffic_count"] = fmt.Sprintf("%s vehicles a day on %s, counted %s away",
			comma(float64(r.Road.AADT)), orUnnamed(r.Road.AADTRoute, "the nearest counted segment"), feetOrNone(r.Road.AADTFeet))
	} else {
		m["traffic_count"] = "no NCDOT counted segment near enough to speak for the frontage, which on a rural road means it is not counted"
	}
	m["corner_lot"] = r.Road.Corner()
	if r.Road.Corner() {
		m["roads_on_more_than_one_side"] = r.Road.NearbyRoads
	}
	m["nearest_interstate_pavement"] = namedFeet(r.Road.HighwayFeet, r.Road.HighwayName)
	m["nearest_on_ramp"] = namedFeet(r.Road.RampFeet, r.Road.RampName)
	if r.Road.Partial {
		m["incomplete"] = true
	}
	return m
}

func (r *Report) SchoolsPart() map[string]any {
	m := r.head()
	z := r.Zones
	if z.Elementary == "" && z.Middle == "" && z.High == "" {
		m["schools"] = "nothing found, the school layers could not be reached"
		return m
	}
	m["elementary"] = schoolLine(z.Elementary, z.ElemMiles, r.Morning.DetourMin)
	m["middle"] = schoolLine(z.Middle, z.MiddleMiles, r.Morning.MiddleDetourMin)
	m["high"] = schoolLine(z.High, z.HighMiles, r.Morning.HighDetourMin)
	m["zoned_or_nearest"] = zoningNote(z)
	m["source"] = z.Source
	if z.K8 {
		m["note"] = "one school covers elementary and middle here, so there is no second move"
	}
	if r.Morning.OppositeWays {
		m["warning"] = "the elementary and the middle school lie in opposite directions, which is a two stop morning once both matter"
	}
	if z.Partial {
		m["incomplete"] = true
	}
	return m
}

func zoningNote(z SchoolZones) string {
	if z.Verified {
		return "real attendance boundaries, so these are the zoned schools"
	}
	if z.Nearest {
		return "this county publishes no attendance boundary, so these are the nearest schools of each level and not confirmed as the zoned ones"
	}
	return "zoning unverified"
}

func schoolLine(name string, miles, detour float64) string {
	if name == "" {
		return "not found"
	}
	out := name
	if miles > 0 {
		out += fmt.Sprintf(", %.1f miles", miles)
	}
	if detour > 0 {
		out += ", " + minutes(detour) + " on top of the commute to drop off on the way"
	}
	return out
}

func (r *Report) DrivesPart() map[string]any {
	m := r.head()
	if r.Morning.Commute.Minutes > 0 {
		m["commute_to_work"] = fmt.Sprintf("%s, %.1f miles", minutes(r.Morning.Commute.Minutes), r.Morning.Commute.Miles)
	} else {
		m["commute_to_work"] = "no work address in the config, so nothing is measured from one"
	}
	if r.Morning.DetourMin > 0 {
		m["school_run_detour"] = fmt.Sprintf("%s on top of the commute, measured as one trip through the school rather than two legs added up", minutes(r.Morning.DetourMin))
		m["commute_with_the_school_run"] = minutes(r.Morning.WithElementary.Minutes)
	}

	// Grouped by whose drive it is, because a question here is almost always
	// about one person. Asked about a household member's work against a flat list
	// of place names, the model had nothing to match the name on and looked the
	// name up in the encyclopedia as a stranger instead.
	if len(r.Drives) > 0 {
		byWho := map[string][]string{}
		for _, leg := range r.Drives {
			who := leg.Who
			if who == "" {
				// The only drives with nobody against them are the hospitals and
				// nursing homes, which are nobody's commute yet. Saying what they
				// are beats filing them under a person who does not drive there.
				who = "nursing work within reach, for whoever in the house wants it"
			}
			byWho[who] = append(byWho[who],
				fmt.Sprintf("%s: %s, %.1f miles", orUnnamed(leg.Name, "somewhere"), minutes(leg.Minutes), leg.Miles))
		}
		for who := range byWho {
			sort.Strings(byWho[who])
		}
		m["drives_from_this_house"] = byWho
	}
	if len(r.Household) > 0 {
		who := map[string]string{}
		for _, p := range r.Household {
			who[p.Name] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.Role), ".") + ". " + p.Note)
		}
		// So a question naming somebody is answered about them rather than looked
		// up as a stranger.
		m["who_lives_here"] = who
	}
	if r.Morning.Partial {
		m["incomplete"] = "a leg would not route, so the detour is not trustworthy"
	}
	return m
}

func (r *Report) AreaPart() map[string]any {
	m := r.head()
	a := r.Area
	if !a.Found {
		m["area"] = "no figures, the census lookup did not answer"
		return m
	}
	m["describes"] = fmt.Sprintf("%s, %s level", a.Name, a.Level)
	m["source"] = a.Source
	m["population"] = comma(float64(a.Population))
	m["median_age"] = a.MedianAge
	m["median_household_income"] = "$" + comma(a.MedianIncome)
	m["owner_occupied_pct"] = a.OwnerOccupiedPct
	m["vacancy_pct"] = a.VacancyPct
	if a.MedianHomeValue > 0 {
		m["median_home_value"] = "$" + comma(a.MedianHomeValue)
		if r.Price > 0 {
			m["asking_against_the_county_median"] = fmt.Sprintf("%+.0f%%", (float64(r.Price)-a.MedianHomeValue)/a.MedianHomeValue*100)
		}
	}
	if a.CrimeSource != "" {
		m["reported_crime"] = a.CrimeWhy()
		m["compared_with_the_state"] = a.CrimeWhyShort()
	} else {
		m["reported_crime"] = "no figures. The FBI's API needs a key and the state publishes a PDF once a year, so there is none rather than a guess."
	}
	if len(a.Composition) > 0 {
		m["published_composition"] = shareMap(a.Composition)
	}
	if len(a.Religion) > 0 {
		m["published_religion"] = shareMap(a.Religion)
	}
	m["note"] = "county level figures over several hundred square miles, so they describe the county and not the street. What the street is like is in the neighbours section."
	return m
}

func shareMap(s []AreaShare) map[string]float64 {
	out := map[string]float64{}
	for _, v := range s {
		out[v.Label] = v.Percent
	}
	return out
}

func (r *Report) LandPart() map[string]any {
	m := r.head()
	p := r.Parcel
	if p.Found {
		if p.Acres > 0 {
			m["lot_size"] = fmt.Sprintf("%s acres, %s", trimFloat(p.Acres), p.AcresFrom)
		}
		m["assessed_value"] = fmt.Sprintf("$%s total, $%s land and $%s buildings",
			comma(p.MarketValue), comma(p.LandValue), comma(p.BuildValue))
		m["parcel_address_on_the_tax_roll"] = p.Address
		m["parcel_use"] = p.Use
		m["last_sold"] = p.LastSold
		m["parcel_source"] = p.Source
	} else {
		m["parcel"] = "no parcel matched, so the lot size and the assessment are unknown"
	}

	if r.Value.Found {
		m["what_the_assessment_is_worth_now"] = fmt.Sprintf(
			"about $%s, between $%s and $%s, carrying the %d assessment forward %.1f%% on %s",
			comma(r.Value.Estimate), comma(r.Value.Low), comma(r.Value.High),
			r.Value.BaseYear, r.Value.MovedPct, r.Value.Basis)
		// A percentage off a record that is not this house is a wrong number
		// wearing a precise one's clothes, and a model will repeat it as a
		// finding. Say what is wrong with the record instead.
		if why := r.parcelCaveat(); why != "" {
			m["do_not_compare_the_price_to_that"] = why
		} else if r.Price > 0 && r.Value.Estimate > 0 {
			gap := (float64(r.Price) - r.Value.Estimate) / r.Value.Estimate * 100
			m["asking_against_that"] = fmt.Sprintf("%+.0f%%", gap)
		}
		m["value_note"] = "a county index is not an appraisal of one house, which is what the range is for"
	}

	t := r.Terrain
	if t.MeanSlopePct > 0 || t.ReliefFeet > 0 {
		m["slope"] = fmt.Sprintf("%.0f feet between the highest and lowest point sampled, averaging %.1f%%", t.ReliefFeet, t.MeanSlopePct)
		m["share_flat_enough_to_build_or_garden"] = fmt.Sprintf("%.0f%%, against a %d%% slope threshold", t.FlatShare*100, flatEnoughPct)
	} else {
		m["slope"] = "not measured"
	}
	return m
}

func (r *Report) NeighboursPart() map[string]any {
	m := r.head()
	s := r.Street
	if !s.Found {
		m["street"] = "the parcel layer did not answer, so nothing about the street is known"
		return m
	}
	m["parcels_on_the_street"] = s.Parcels
	m["owner_occupied"] = fmt.Sprintf("%d of %d, %.0f%%", s.OwnerOccupied, s.Parcels, s.Percent)
	m["no_mailing_address_on_the_tax_roll"] = s.Unknown
	m["how_this_is_worked_out"] = "each parcel's site address compared with where the owner's tax post goes, which is the thing people mean by whether a street is settled"
	m["elbow_room"] = s.SpaceWhy()
	return m
}

func (r *Report) OutingsPart() map[string]any {
	m := r.head()
	if len(r.Outings.Nearest) == 0 {
		m["outings"] = "nothing found within an hour, or Overpass did not answer"
		return m
	}
	out := make([]string, 0, len(r.Outings.Nearest))
	for _, o := range r.Outings.Nearest {
		// Overpass carries plenty of unnamed features, and the loader falls back
		// to the label, so printing both reads as "tubing or paddling: tubing or
		// paddling".
		if strings.EqualFold(o.Name, o.Label) {
			out = append(out, fmt.Sprintf("%s, %.1f miles", o.Label, o.Miles))
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s, %.1f miles", o.Label, o.Name, o.Miles))
	}
	m["within_about_an_hour"] = out
	m["in_short"] = r.Outings.Why()
	if r.Outings.Partial {
		m["incomplete"] = true
	}
	return m
}

// Links are built from the address and nothing is requested until somebody clicks
// one, so this costs no traffic to anybody and there is nothing to be blocked by.
func (r *Report) Links() map[string]string {
	lat, lon := r.Lat, r.Lon
	if r.Parcel.Lat != 0 {
		// The middle of the lot, not the geocoded point, which sits in the road
		// and lands a pin at the neighbour's.
		lat, lon = r.Parcel.Lat, r.Parcel.Lon
	}
	q := url.QueryEscape(strings.Join([]string{r.Address, r.City, r.State, r.Zip}, " "))
	return map[string]string{
		"map":         fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=17/%.6f/%.6f", lat, lon, lat, lon),
		"satellite":   fmt.Sprintf("https://www.google.com/maps/@%.6f,%.6f,300m/data=!3m1!1e3", lat, lon),
		"street_view": fmt.Sprintf("https://www.google.com/maps?q&layer=c&cbll=%.6f,%.6f", lat, lon),
		"zillow":      "https://www.zillow.com/homes/" + q + "_rb/",
		"realtor":     "https://www.realtor.com/realestateandhomes-search/" + q,
		"redfin":      "https://www.redfin.com/city/search?query=" + q,
		"flood_map":   fmt.Sprintf("https://msc.fema.gov/portal/search#searchresultsanchor?AddressQuery=%s", q),
		"usda_map":    "https://eligibility.sc.egov.usda.gov/eligibility/welcomeAction.do",
	}
}

// parcelCaveat is why the assessed value must not be held up against the asking
// price. Both cases are common and both produce a number that looks precise: the
// geocoder puts the point in the road so the nearest parcel is sometimes next
// door, and a tax roll carrying land and no buildings is a vacant lot record
// against a house that is standing on it.
func (r *Report) parcelCaveat() string {
	p := r.Parcel
	if !p.Found || r.Price <= 0 {
		return ""
	}
	if p.Address != "" && addressKey(p.Address, "") != addressKey(r.Address, "") {
		return fmt.Sprintf("the parcel that matched is %s, not the address asked about, so the assessment is somebody else's", p.Address)
	}
	if p.MarketValue > 0 && p.BuildValue == 0 && p.LandValue > 0 {
		return "the tax roll has land and no buildings on this parcel, so the assessment is for a vacant lot and says nothing about what the house is worth"
	}
	return ""
}

// The one line forms, which the summary is made of.

func (r *Report) floodLine() string {
	if !r.Flood.Measured {
		return "not measured"
	}
	if r.Flood.SFHA {
		return fmt.Sprintf("in FEMA zone %s, a special flood hazard area, so flood insurance is required", r.Flood.Zone)
	}
	// An unmapped point is not zone X. FEMA leaves plenty of rural parcels off the
	// panel entirely, and calling that X says somebody surveyed it and found
	// minimal hazard.
	if r.Flood.Zone == "" {
		return fmt.Sprintf("FEMA maps no zone at this point, and the nearest special flood hazard area is %s",
			feetOrNone(r.Flood.SFHAFeet))
	}
	return fmt.Sprintf("zone %s, outside any special flood hazard area, nearest one %s",
		r.Flood.Zone, feetOrNone(r.Flood.SFHAFeet))
}

func (r *Report) roadLine() string {
	if !r.Road.Measured {
		return "not measured"
	}
	out := fmt.Sprintf("fronts %s", orUnnamed(r.Road.RoadName, "an unnamed road"))
	if r.Road.Class != "" {
		out += " (" + r.Road.Class + ")"
	}
	if r.Road.AADT > 0 {
		out += fmt.Sprintf(", %s a day", comma(float64(r.Road.AADT)))
	}
	if r.Road.Corner() {
		out += ", corner lot"
	}
	return out
}

func (r *Report) schoolLine() string {
	z := r.Zones
	if z.Elementary == "" {
		return "not found"
	}
	out := z.Elementary
	if z.Middle != "" {
		out += ", " + z.Middle
	}
	if z.High != "" {
		out += ", " + z.High
	}
	if !z.Verified {
		out += " (nearest, not confirmed as zoned)"
	}
	return out
}

func (r *Report) morningLine() string {
	if r.Morning.Commute.Minutes <= 0 {
		return "no work address configured"
	}
	out := minutes(r.Morning.Commute.Minutes) + " to work"
	if r.Morning.DetourMin > 0 {
		out += fmt.Sprintf(", %.0f more with the school drop off", r.Morning.DetourMin)
	}
	return out
}

func (r *Report) lotLine() string {
	if !r.Parcel.Found {
		return "no parcel matched"
	}
	out := fmt.Sprintf("%s acres", trimFloat(r.Parcel.Acres))
	if r.Terrain.MeanSlopePct > 0 {
		out += fmt.Sprintf(", %.0f%% of it flat enough to build or garden on", r.Terrain.FlatShare*100)
	}
	return out
}

func (r *Report) neighbourLine() string {
	if !r.Street.Found {
		return "unknown"
	}
	return fmt.Sprintf("%.0f%% of the %d parcels on the street are owner occupied", r.Street.Percent, r.Street.Parcels)
}

func (r *Report) usdaLine() string {
	if !r.USDA.Measured {
		return "not checked"
	}
	if r.USDA.Eligible {
		return "eligible, so a no money down USDA loan is on the table"
	}
	return "inside a USDA ineligible area, so no USDA loan here"
}

func (r *Report) crimeLine() string {
	return r.Area.CrimeWhyShort() + ", " + r.Area.CrimeWhy()
}

// minutes writes a drive time, singular when it is one, since "1 minutes" in the
// middle of an answer is the sort of thing that makes the rest look careless.
func minutes(m float64) string {
	if math.Round(m) == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%.0f minutes", m)
}

func feetOrNone(f float64) string {
	if f < 0 {
		return "none found within the search radius"
	}
	if f >= 5280 {
		return fmt.Sprintf("%.1f miles", f/5280)
	}
	return fmt.Sprintf("%.0f feet", f)
}

func namedFeet(f float64, name string) string {
	s := feetOrNone(f)
	if f >= 0 && name != "" {
		return s + ", " + name
	}
	return s
}

func waterLine(f FloodResult) string {
	if f.WaterFeet < 0 {
		return "no mapped water within the search radius"
	}
	return fmt.Sprintf("%s, %s, %s", feetOrNone(f.WaterFeet), orUnnamed(f.WaterName, "unnamed"), orUnnamed(f.WaterKind, "kind not stated"))
}
