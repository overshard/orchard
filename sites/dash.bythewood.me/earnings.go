package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Earnings for the hundred largest companies in the S&P 500, which is as far
// down the index as a name is still a reason the market moved. Nasdaq publishes
// a free calendar and a free screener, and the screener is what makes "the top
// hundred" a filter that maintains itself rather than a list to keep by hand.
const (
	earningsURL      = "https://api.nasdaq.com/api/calendar/earnings?date="
	earningsCapsURL  = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&limit=500&offset=0"
	earningsEvery    = 6 * time.Hour
	earningsDailyURL = "https://query1.finance.yahoo.com/v7/finance/spark"

	// How far down the index the panel goes. The hundredth name is worth about
	// $120B at the moment, and everything under it reports into a market that is
	// not watching.
	earningsRank = 100

	// Three either side, since that is what the panel has room for once every
	// row carries a result.
	earningsShown = 3

	// How far each walk goes for those three, counting today in both. Reporting
	// season is four bursts a year, so off season the forward walk runs its
	// whole length and finds nothing, which is the correct answer rather than a
	// failure.
	earningsBackDays = 21
	earningsNextDays = 28

	// A surprise inside this band is the estimate being met, not a beat. Penny
	// estimates make the percentage meaningless either way, which is why the
	// row shows the two figures next to the word.
	earningsMetBand = 2.0

	// A move this size is the market having an opinion. Under it a beat and a
	// red close is noise rather than a story about the outlook.
	earningsMoveBand = 1.5
)

// Earning is one row. A reported row and an upcoming row are the same shape
// with different halves filled in, so the template has one thing to render.
type Earning struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Day    string `json:"day"`
	When   string `json:"when"`

	// Upcoming: what the street is looking for, and how many analysts said so.
	Est  string `json:"est"`
	Ests string `json:"ests"`

	// Reported: the call, the two figures behind it, and what the stock did.
	Verdict  string  `json:"verdict"`
	Actual   string  `json:"actual"`
	Forecast string  `json:"forecast"`
	Move     string  `json:"move"`
	MovePct  float64 `json:"move_pct"`
	Dir      string  `json:"dir"`

	// Set when the verdict and the move disagree. No free source publishes
	// guidance, so this is as close as the panel gets to saying why a company
	// that beat still closed down.
	Note string `json:"note"`
}

// Earnings is the panel: what has printed and what is coming.
type Earnings struct {
	Reported []Earning `json:"reported"`
	Upcoming []Earning `json:"upcoming"`
}

func (e Earnings) empty() bool { return len(e.Reported) == 0 && len(e.Upcoming) == 0 }

type nasdaqEarnings struct {
	Data struct {
		Rows []struct {
			Symbol      string `json:"symbol"`
			Name        string `json:"name"`
			MarketCap   string `json:"marketCap"`
			Time        string `json:"time"`
			EPS         string `json:"eps"`
			EPSForecast string `json:"epsForecast"`
			Surprise    string `json:"surprise"`
			NoOfEsts    string `json:"noOfEsts"`
		} `json:"rows"`
	} `json:"data"`
}

type nasdaqScreener struct {
	Data struct {
		Table struct {
			Rows []struct {
				Symbol    string `json:"symbol"`
				MarketCap string `json:"marketCap"`
			} `json:"rows"`
		} `json:"table"`
	} `json:"data"`
}

func fetchEarnings(ctx context.Context, g *Guard, now time.Time) (Earnings, error) {
	caps, err := fetchIndexCaps(ctx, g)
	if err != nil {
		return Earnings{}, err
	}

	var out Earnings
	today := now.In(easternTime())

	// Both walks start on today. Nasdaq stops supplying the time of day once a
	// date is in the past and only fills in the actual EPS then too, so the
	// presence of that figure is what sorts a row into one half or the other,
	// and a company reporting tonight is upcoming until its number lands.
	day := func(d time.Time) ([]Earning, bool) { return earningsDay(ctx, g, d, today, caps) }
	out.Reported = walkEarnings(day, today, -1, earningsBackDays, reportedRow)
	out.Upcoming = walkEarnings(day, today, 1, earningsNextDays, upcomingRow)

	// Today's pre-market names land in Reported ahead of yesterday's, and the
	// walk visits days newest first, so the halves are already in the order they
	// read in. The reactions are the one thing that needs a second upstream.
	addReactions(ctx, g, out.Reported, today)

	if out.empty() {
		return Earnings{}, fmt.Errorf("nasdaq: no top %d name reports in the window", earningsRank)
	}
	return out, nil
}

func reportedRow(e Earning) bool { return e.Verdict != "" }
func upcomingRow(e Earning) bool { return e.Verdict == "" }

// walkEarnings takes rows a day at a time until it has earningsShown of them,
// stepping forward or back from today. A day that will not fetch costs its own
// rows and the walk carries on.
func walkEarnings(day func(time.Time) ([]Earning, bool), today time.Time, step, days int, want func(Earning) bool) []Earning {
	var out []Earning
	for i := 0; i < days && len(out) < earningsShown; i++ {
		rows, ok := day(today.AddDate(0, 0, i*step))
		if !ok {
			continue
		}
		for _, r := range rows {
			if !want(r) {
				continue
			}
			out = append(out, r)
			if len(out) == earningsShown {
				break
			}
		}
	}
	return out
}

// earningsDay is one calendar day filtered to the index names, largest first.
// The false return is a day that could not be fetched, which costs its own rows
// rather than the panel.
func earningsDay(ctx context.Context, g *Guard, day, today time.Time, caps map[string]float64) ([]Earning, bool) {
	if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return nil, false
	}

	var payload nasdaqEarnings
	if err := getJSONWith(ctx, g, "nasdaq", earningsURL+day.Format("2006-01-02"), &payload); err != nil {
		return nil, false
	}

	label := dayLabel(day, today)
	var out []Earning
	for _, r := range payload.Data.Rows {
		symbol := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if _, ok := caps[symbol]; !ok {
			continue
		}

		e := Earning{
			Symbol: symbol,
			Name:   trimCompany(r.Name),
			Day:    label,
			When:   whenLabel(r.Time),
		}

		actual, hasActual := parseEPS(r.EPS)
		forecast, hasForecast := parseEPS(r.EPSForecast)
		switch {
		case hasActual && hasForecast:
			e.Verdict = epsVerdict(actual, forecast, r.Surprise)
			e.Actual = money(actual)
			e.Forecast = money(forecast)
		case hasForecast:
			e.Est = money(forecast)
			e.Ests = strings.TrimSpace(r.NoOfEsts)
		}

		out = append(out, e)
	}

	sort.SliceStable(out, func(i, j int) bool { return caps[out[i].Symbol] > caps[out[j].Symbol] })
	return out, true
}

// fetchIndexCaps is the top earningsRank of the S&P 500 by market cap. The
// screener answers every US listing sorted by cap in one call, so the index
// membership list is the only part of this that is written down.
func fetchIndexCaps(ctx context.Context, g *Guard) (map[string]float64, error) {
	var payload nasdaqScreener
	if err := getJSONWith(ctx, g, "nasdaq", earningsCapsURL, &payload); err != nil {
		return nil, err
	}

	caps := make(map[string]float64, earningsRank)
	for _, r := range payload.Data.Table.Rows {
		symbol := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if !inIndex(symbol) {
			continue
		}
		if cap := parseMoney(r.MarketCap); cap > 0 {
			caps[symbol] = cap
		}
		if len(caps) == earningsRank {
			break
		}
	}

	if len(caps) < earningsRank/2 {
		return nil, fmt.Errorf("nasdaq screener: %d index names, expected %d", len(caps), earningsRank)
	}
	return caps, nil
}

// addReactions fills in what each stock did around its print, in place. One
// batched request covers every reported row, and a failure leaves the rows
// without a move rather than dropping them.
func addReactions(ctx context.Context, g *Guard, rows []Earning, today time.Time) {
	if len(rows) == 0 {
		return
	}

	symbols := make([]string, 0, len(rows))
	for _, r := range rows {
		symbols = append(symbols, r.Symbol)
	}

	series, err := fetchDailyCloses(ctx, g, symbols)
	if err != nil {
		return
	}

	for i := range rows {
		day, err := time.ParseInLocation("2006-01-02", reportDate(rows[i].Day, today), easternTime())
		if err != nil {
			continue
		}
		pct, ok := reaction(series[rows[i].Symbol], day)
		if !ok {
			continue
		}
		rows[i].MovePct = pct
		rows[i].Move = signedPercent(pct)
		// Rounded first, so a move that prints as +0.0% is not painted green
		// for a rounding error.
		rows[i].Dir = direction(math.Round(pct*10) / 10)
		rows[i].Note = earningsNote(rows[i].Verdict, pct)
	}
}

// dailyClose is one session, keyed by its New York date.
type dailyClose struct {
	date  string
	close float64
}

func fetchDailyCloses(ctx context.Context, g *Guard, symbols []string) (map[string][]dailyClose, error) {
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	// A month of daily bars, which always spans the walk back plus the session
	// on either side of the oldest report in it.
	q.Set("range", "1mo")
	q.Set("interval", "1d")

	var payload sparkPayload
	if err := getJSON(ctx, g, "yahoo", earningsDailyURL+"?"+q.Encode(), &payload); err != nil {
		return nil, err
	}

	out := make(map[string][]dailyClose, len(symbols))
	for _, r := range payload.Spark.Result {
		if len(r.Response) == 0 || len(r.Response[0].Indicators.Quote) == 0 {
			continue
		}
		s := r.Response[0]
		var days []dailyClose
		for i, c := range s.Indicators.Quote[0].Close {
			if c == nil || math.IsNaN(*c) || i >= len(s.Timestamp) {
				continue
			}
			at := time.Unix(s.Timestamp[i], 0).In(easternTime())
			days = append(days, dailyClose{date: at.Format("2006-01-02"), close: *c})
		}
		out[strings.ToUpper(r.Symbol)] = days
	}
	return out, nil
}

// reaction is what the stock did on the announcement, as a percent.
//
// Nasdaq drops the pre-market or after-hours flag once a date is in the past, so
// which of the two sessions around the report carried it is not knowable from
// the calendar. The one that moved is the one that heard the news, and when
// neither did it does not matter which gets picked.
func reaction(days []dailyClose, report time.Time) (float64, bool) {
	stamp := report.Format("2006-01-02")

	var before, on, after float64
	for _, d := range days {
		switch {
		case d.date < stamp:
			before = d.close
		case d.date == stamp:
			on = d.close
		case after == 0:
			after = d.close
		}
	}

	// Reported this morning before the bell, so the session it moved is still
	// open and today's close is the last one there is.
	if after == 0 {
		if before == 0 || on == 0 {
			return 0, false
		}
		return (on/before - 1) * 100, true
	}
	if before == 0 || on == 0 {
		return 0, false
	}

	pre := (on/before - 1) * 100
	post := (after/on - 1) * 100
	if math.Abs(pre) > math.Abs(post) {
		return pre, true
	}
	return post, true
}

// earningsNote names the disagreement between the result and the reaction, which is the
// only honest thing this panel can say about guidance without a paid feed.
func earningsNote(verdict string, move float64) string {
	switch {
	case verdict == "BEAT" && move <= -earningsMoveBand:
		return "SOLD THE BEAT"
	case verdict == "MISS" && move >= earningsMoveBand:
		return "BOUGHT THE MISS"
	}
	return ""
}

func epsVerdict(actual, forecast float64, surprise string) string {
	pct, err := strconv.ParseFloat(strings.TrimSpace(surprise), 64)
	if err != nil {
		// No surprise figure, which happens when last year had no estimate. The
		// two numbers are still there to compare.
		if forecast == 0 {
			return "MET"
		}
		pct = (actual - forecast) / math.Abs(forecast) * 100
	}
	switch {
	case pct > earningsMetBand:
		return "BEAT"
	case pct < -earningsMetBand:
		return "MISS"
	}
	return "MET"
}

// reportDate turns a row's display label back into the day it happened, since
// the label is what the walk kept. Anything it cannot read is a day too far back
// for a reaction to be interesting anyway.
func reportDate(label string, today time.Time) string {
	switch label {
	case "TODAY":
		return today.Format("2006-01-02")
	case "YESTERDAY":
		return today.AddDate(0, 0, -1).Format("2006-01-02")
	case "TOMORROW":
		return today.AddDate(0, 0, 1).Format("2006-01-02")
	}
	for i := -earningsBackDays; i <= earningsNextDays; i++ {
		day := today.AddDate(0, 0, i)
		if dayLabel(day, today) == label {
			return day.Format("2006-01-02")
		}
	}
	return ""
}

// getJSONWith is getJSON with the one header Nasdaq's API insists on. Without
// an Accept of application/json it answers with an HTML challenge page.
func getJSONWith(ctx context.Context, g *Guard, endpoint, url string, out any) error {
	return getJSONHeaders(ctx, g, endpoint, url, map[string]string{
		"Accept": "application/json",
	}, out)
}

// parseEPS reads Nasdaq's EPS strings, which wrap a loss in parentheses the way
// an accountant would. An empty value is a quarter that has not been reported.
func parseEPS(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0, false
	}
	negative := strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")
	if negative {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
	}
	v, err := strconv.ParseFloat(strings.NewReplacer("$", "", ",", "", " ", "").Replace(s), 64)
	if err != nil {
		return 0, false
	}
	if negative {
		v = -v
	}
	return v, true
}

func money(v float64) string {
	if v < 0 {
		return fmt.Sprintf("-$%.2f", -v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func signedPercent(v float64) string {
	if v > 0 {
		return fmt.Sprintf("+%.1f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

func parseMoney(s string) float64 {
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(strings.TrimSpace(s))
	if s == "" || s == "N/A" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// trimCompany drops the suffixes that make every row the same width and say
// nothing, since the ticker is already there.
func trimCompany(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{
		", Inc.", " Inc.", " Inc", ", Ltd.", " Ltd.", " Ltd",
		" Corporation", " Corp.", " Corp", " Company", " Co.",
		" Holdings", " plc", " PLC", " S.A.", " N.V.",
	} {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(strings.TrimSuffix(name, ","))
}

func whenLabel(t string) string {
	switch {
	case strings.Contains(t, "pre-market"):
		return "PRE"
	case strings.Contains(t, "after-hours"):
		return "POST"
	default:
		return ""
	}
}

func dayLabel(day, today time.Time) string {
	switch days := int(day.Truncate(24*time.Hour).Sub(today.Truncate(24*time.Hour)).Hours() / 24); days {
	case 0:
		return "TODAY"
	case -1:
		return "YESTERDAY"
	case 1:
		return "TOMORROW"
	default:
		return strings.ToUpper(day.Format("Mon 2 Jan"))
	}
}
