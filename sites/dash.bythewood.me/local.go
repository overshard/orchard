package main

import (
	"context"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Conditions where Isaac actually is, beyond the temperature. Every upstream
// here is free, keyless and guarded.

const (
	// The National Weather Service asks for a contact in the User-Agent and is
	// otherwise open. This is the one panel that matters at three in the
	// morning, so it polls faster than the rest of the weather.
	alertsURL   = "https://api.weather.gov/alerts/active?point=%.2f,%.2f"
	alertsEvery = 10 * time.Minute

	airURL   = "https://air-quality-api.open-meteo.com/v1/air-quality"
	airEvery = 30 * time.Minute

	// Forsyth County counts pollen off a sampler in downtown Winston-Salem and
	// posts it by 3:30pm each business day, with a three day forecast that
	// carries the weekend. It's an HTML page, so when it changes the row drops.
	pollenURL   = "https://www.co.forsyth.nc.us/eap/pollen.aspx"
	pollenEvery = 3 * time.Hour
)

// Alert is one active NWS warning.
type Alert struct {
	Event     string `json:"event"`
	Severity  string `json:"severity"`
	Urgency   string `json:"urgency"`
	Area      string `json:"area"`
	Office    string `json:"office"`
	Headline  string `json:"headline"`
	Starts    string `json:"starts"`
	Until     string `json:"until"`
	UntilUnix int64  `json:"until_unix"`

	// Which event this is across the updates the NWS issues for it, what it is
	// called in words instead of caps, and whether it is a cancellation.
	key    string
	name   string
	cancel bool
}

type alertPayload struct {
	Features []struct {
		Properties struct {
			ID          string `json:"id"`
			MessageType string `json:"messageType"`
			Parameters  struct {
				VTEC []string `json:"VTEC"`
			} `json:"parameters"`
			Event     string `json:"event"`
			Severity  string `json:"severity"`
			Urgency   string `json:"urgency"`
			Headline  string `json:"headline"`
			AreaDesc  string `json:"areaDesc"`
			Onset     string `json:"onset"`
			Effective string `json:"effective"`
			Ends      string `json:"ends"`
			Expires   string `json:"expires"`
		} `json:"properties"`
	} `json:"features"`
}

// The NWS writes every headline off one template, "<event> issued <time> until
// <time> by <office>", and the office is the only part a row doesn't already
// carry.
func alertOffice(headline string) string {
	i := strings.LastIndex(headline, " by ")
	if i < 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(headline[i+4:]), "NWS "))
}

// Empty for the generated template, which every watch and warning gets, and the
// text itself for the rare one where a forecaster wrote something.
func alertHeadline(event, headline string) string {
	h := strings.TrimSpace(headline)
	if h == "" || strings.HasPrefix(strings.ToUpper(h), strings.ToUpper(strings.TrimSpace(event))) {
		return ""
	}
	if i := strings.LastIndex(h, " by "); i > 0 {
		h = strings.TrimSpace(h[:i])
	}
	return h
}

// areaDesc lists every county an alert covers, too many for a phone. The home
// county leads when it's in the list, since the row is read to see whether the
// thing is overhead, and the rest becomes a count.
func alertArea(desc string) string {
	seen := make(map[string]bool)
	names := make([]string, 0, 8)
	for _, part := range strings.Split(desc, ";") {
		n := strings.TrimSpace(part)
		if i := strings.LastIndex(n, ","); i > 0 {
			n = strings.TrimSpace(n[:i])
		}
		n = strings.ToUpper(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	if len(names) == 0 {
		return ""
	}

	lead := names[0]
	for _, n := range names {
		if n == homeCounty {
			lead = n
			break
		}
	}
	if len(names) == 1 {
		return lead
	}
	return fmt.Sprintf("%s +%d", lead, len(names)-1)
}

// alertKey names one event across every update the NWS issues for it. A
// continued warning gets a new id but keeps its VTEC office, phenomenon and
// event number, so that's the key, else the alert's own id.
func alertKey(vtec []string, id string) string {
	for _, v := range vtec {
		f := strings.Split(strings.Trim(v, "/"), ".")
		if len(f) >= 6 {
			return strings.Join(f[2:6], ".")
		}
	}
	return id
}

func fetchAlerts(ctx context.Context, g *Guard) ([]Alert, error) {
	url := fmt.Sprintf(alertsURL, weatherLat, weatherLon)

	var payload alertPayload
	if err := getJSONHeaders(ctx, g, "nws", url, map[string]string{
		// The NWS asks that automated clients identify themselves and say how
		// to be contacted about a misbehaving one.
		"User-Agent": "dash.bythewood.me (isaac@bythewood.me)",
		"Accept":     "application/geo+json",
	}, &payload); err != nil {
		return nil, err
	}

	alerts := make([]Alert, 0, len(payload.Features))
	now := time.Now()
	for _, f := range payload.Features {
		p := f.Properties
		a := Alert{
			Event:    strings.ToUpper(p.Event),
			Severity: strings.ToLower(p.Severity),
			Urgency:  strings.ToLower(p.Urgency),
			Area:     alertArea(p.AreaDesc),
			Office:   alertOffice(p.Headline),
			Headline: alertHeadline(p.Event, p.Headline),
			key:      alertKey(p.Parameters.VTEC, p.ID),
			name:     strings.TrimSpace(p.Event),
			cancel:   p.MessageType == "Cancel",
		}

		ends := p.Ends
		if ends == "" {
			ends = p.Expires
		}
		if t, err := time.Parse(time.RFC3339, ends); err == nil {
			a.Until = strings.ToUpper(t.In(easternTime()).Format("Mon 3:04pm"))
			a.UntilUnix = t.Unix()
		}

		// A watch for this evening and one already overhead are different rows,
		// and the start time is the only thing that says which.
		onset := p.Onset
		if onset == "" {
			onset = p.Effective
		}
		if t, err := time.Parse(time.RFC3339, onset); err == nil && t.After(now) {
			a.Starts = strings.ToUpper(t.In(easternTime()).Format("Mon 3:04pm"))
		}

		alerts = append(alerts, a)
	}

	// Worst first, so a tornado warning is never below a frost advisory.
	rank := map[string]int{"extreme": 0, "severe": 1, "moderate": 2, "minor": 3}
	sort.SliceStable(alerts, func(i, j int) bool {
		ri, ok := rank[alerts[i].Severity]
		if !ok {
			ri = 4
		}
		rj, ok := rank[alerts[j].Severity]
		if !ok {
			rj = 4
		}
		return ri < rj
	})
	return alerts, nil
}

// Air is the air quality panel, and pollen when the county has a reading.
type Air struct {
	AQI      int    `json:"aqi"`
	AQIState string `json:"aqi_state"`
	PM25     string `json:"pm25"`
	Known    bool   `json:"known"`

	// Each reading placed on its own scale, so UV, air quality and pollen read
	// as three of the same instrument instead of three unrelated numbers. Level
	// is the severity step the colour comes from.
	AQIFill  float64 `json:"aqi_fill"`
	AQILevel int     `json:"aqi_level"`

	PollenReading
}

// PollenReading polls on its own slower clock, so it sits inside Air but is
// swapped as a whole.
type PollenReading struct {
	Pollen      string  `json:"pollen"`
	PollenState string  `json:"pollen_state"`
	PollenTop   string  `json:"pollen_top"`
	PollenKnown bool    `json:"pollen_known"`
	PollenFill  float64 `json:"pollen_fill"`
	PollenLevel int     `json:"pollen_level"`
}

// gauge places a reading on a scale that ends where the scale's own top band
// begins, and buckets it into four steps for colour.
func gauge(v, top float64, edges ...float64) (float64, int) {
	fill := math.Round(math.Min(100, math.Max(0, v/top*100))*10) / 10

	level := 0
	for _, e := range edges {
		if v >= e {
			level++
		}
	}
	return fill, level
}

type airPayload struct {
	Current struct {
		USAQI float64 `json:"us_aqi"`
		PM25  float64 `json:"pm2_5"`
	} `json:"current"`
}

func fetchAir(ctx context.Context, g *Guard) (Air, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.2f", weatherLat))
	q.Set("longitude", fmt.Sprintf("%.2f", weatherLon))
	q.Set("current", "us_aqi,pm2_5")
	q.Set("timezone", "America/New_York")

	var payload airPayload
	if err := getJSON(ctx, g, "openmeteo", airURL+"?"+q.Encode(), &payload); err != nil {
		return Air{}, err
	}

	aqi := int(payload.Current.USAQI)
	air := Air{
		AQI:      aqi,
		AQIState: aqiBand(aqi),
		PM25:     fmt.Sprintf("%.1f", payload.Current.PM25),
		Known:    true,
	}
	// Scaled to 200 instead of the 500 the index runs to, since anything over
	// 200 here is smoke from a fire two states away and the bar has to say
	// something on an ordinary day.
	air.AQIFill, air.AQILevel = gauge(float64(aqi), 200, 50, 100, 150)
	return air, nil
}

// aqiBand is the EPA's own scale, which is what the number means and what every
// other reading of it will say.
func aqiBand(aqi int) string {
	switch {
	case aqi <= 50:
		return "GOOD"
	case aqi <= 100:
		return "MODERATE"
	case aqi <= 150:
		return "SENSITIVE GROUPS"
	case aqi <= 200:
		return "UNHEALTHY"
	case aqi <= 300:
		return "VERY UNHEALTHY"
	default:
		return "HAZARDOUS"
	}
}

var (
	pollenCell    = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	pollenCaption = regexp.MustCompile(`(?s)<caption>(.*?)</caption>`)
	pollenDate    = regexp.MustCompile(`\d{2}/\d{2}/\d{4}`)
	pollenTag     = regexp.MustCompile(`<[^>]+>`)
)

var pollenTypes = []string{"Trees", "Grasses", "Weeds"}

// pollenTable is the table with this id, cut out of the page.
func pollenTable(page, id string) string {
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	page = page[i:]
	if j := strings.Index(page, "</table>"); j >= 0 {
		page = page[:j]
	}
	return page
}

func pollenText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(pollenTag.ReplaceAllString(s, " "))), " ")
}

func pollenCells(table string) []string {
	var cells []string
	for _, m := range pollenCell.FindAllStringSubmatch(table, -1) {
		cells = append(cells, pollenText(m[1]))
	}
	return cells
}

// parsePollen reads the Pollen Rating Scale for each type on the given day. The
// count is used once it's posted for that day, and the forecast until then.
func parsePollen(page string, day time.Time) (map[string]int, error) {
	want := day.Format("01/02/2006")

	if m := pollenCaption.FindStringSubmatch(pollenTable(page, "Graph")); m != nil {
		posted, err := time.Parse("Monday, January 2, 2006", pollenText(m[1]))
		if err == nil && posted.Format("01/02/2006") == want {
			cells := pollenCells(pollenTable(page, "Statistics"))
			if counts, ok := pollenRow(cells, 1, 0); ok {
				return counts, nil
			}
		}
	}

	cells := pollenCells(pollenTable(page, "Forecast"))
	col := -1
	for i, c := range cells {
		if pollenDate.FindString(c) == want {
			// Less one for the empty corner cell.
			col = i - 1
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("pollen: no reading for %s", want)
	}
	if counts, ok := pollenRow(cells, 0, col); ok {
		return counts, nil
	}
	return nil, fmt.Errorf("pollen: forecast table has no %s column", want)
}

// pollenRow finds each type's row label and reads the PRS that sits skip cells
// and then col columns after it.
func pollenRow(cells []string, skip, col int) (map[string]int, bool) {
	counts := map[string]int{}
	for i, c := range cells {
		for _, name := range pollenTypes {
			if c != name || i+1+skip+col >= len(cells) {
				continue
			}
			n, err := strconv.Atoi(cells[i+1+skip+col])
			if err != nil {
				return nil, false
			}
			counts[name] = n
		}
	}
	return counts, len(counts) == len(pollenTypes)
}

func fetchPollen(ctx context.Context, g *Guard, now time.Time) (PollenReading, error) {
	page, err := getPage(ctx, g, "forsyth", pollenURL)
	if err != nil {
		return PollenReading{}, err
	}
	counts, err := parsePollen(page, now.In(easternTime()))
	if err != nil {
		return PollenReading{}, err
	}
	return pollenSummary(counts), nil
}

// pollenSummary reads the worst type as the reading, and names up to two types
// that have any pollen at all, since a third wraps the cell onto a second line.
func pollenSummary(counts map[string]int) PollenReading {
	names := make([]string, 0, len(counts))
	worst := 0
	for name, n := range counts {
		worst = max(worst, n)
		if n > 0 {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 2 {
		names = names[:2]
	}
	for i := range names {
		names[i] = strings.ToUpper(names[i])
	}

	p := PollenReading{
		Pollen:      strconv.Itoa(worst),
		PollenState: pollenBand(worst),
		PollenTop:   strings.Join(names, " · "),
		PollenKnown: true,
	}
	p.PollenFill, p.PollenLevel = gauge(float64(worst), 76, 26, 51, 76)
	return p
}

// pollenBand is Forsyth County's own Pollen Rating Scale.
func pollenBand(prs int) string {
	switch {
	case prs <= 0:
		return "ABSENT"
	case prs <= 25:
		return "LOW"
	case prs <= 50:
		return "MODERATE"
	case prs <= 75:
		return "HIGH"
	default:
		return "VERY HIGH"
	}
}
