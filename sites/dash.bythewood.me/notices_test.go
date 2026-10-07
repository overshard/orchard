package main

import (
	"strings"
	"testing"
	"time"
)

func noticeIDs(ns []Notice) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

// A poller replaces its own kind and nothing else, and a snapshot taken before
// the write still reads what it read, since the hub may be marshalling it.
func TestSetNoticesReplacesOneKind(t *testing.T) {
	var st State
	st.setNotices("news", []Notice{{ID: "news:a", Kind: "news"}})
	st.setNotices("weather", []Notice{{ID: "weather:w", Kind: "weather"}})
	before := st.Notices

	st.setNotices("news", []Notice{{ID: "news:b", Kind: "news"}})
	got := strings.Join(noticeIDs(st.Notices), ",")
	if got != "weather:w,news:b" {
		t.Errorf("notices = %s", got)
	}
	if before[0].ID != "news:a" {
		t.Errorf("an earlier snapshot changed under its reader: %v", noticeIDs(before))
	}
}

func sessionQuote(symbol string, prev float64, closes []float64, start time.Time) Quote {
	times := make([]int64, len(closes))
	for i := range closes {
		times[i] = start.Add(time.Duration(i) * 30 * time.Minute).Unix()
	}
	return Quote{Symbol: symbol, Price: closes[len(closes)-1], Previous: prev,
		AsOf: time.Unix(times[len(times)-1], 0), Closes: closes, Times: times}
}

// The day ladder starts at 2% for the S&P and notifies once per rung, so the
// same fall read on the next poll is the same id and a deeper one is a new one.
func TestMarketNoticesClimbTheLadder(t *testing.T) {
	open := at(t, "2026-09-28 09:30")
	now := at(t, "2026-09-28 11:00")

	quiet := buildMarket(map[string]Quote{"^GSPC": sessionQuote("^GSPC", 100, []float64{98.5, 98.3, 98.2, 98.1}, open)}, now)
	if len(quiet.notices) != 0 {
		t.Errorf("a 1.9%% day raised %v", noticeIDs(quiet.notices))
	}

	two := buildMarket(map[string]Quote{"^GSPC": sessionQuote("^GSPC", 100, []float64{98.2, 98.0, 97.9, 97.9}, open)}, now)
	again := buildMarket(map[string]Quote{"^GSPC": sessionQuote("^GSPC", 100, []float64{98.2, 98.0, 97.8, 97.7}, open)}, now)
	three := buildMarket(map[string]Quote{"^GSPC": sessionQuote("^GSPC", 100, []float64{98.2, 98.0, 97.3, 96.9}, open)}, now)

	if len(two.notices) != 1 || !strings.Contains(two.notices[0].Title, "down 2.1% today") {
		t.Fatalf("a 2.1%% fall gave %+v", two.notices)
	}
	if two.notices[0].ID != again.notices[0].ID {
		t.Errorf("the same rung was given two ids, %s and %s", two.notices[0].ID, again.notices[0].ID)
	}
	if three.notices[0].ID == two.notices[0].ID {
		t.Error("a fall past 3% reused the 2% id")
	}
}

// A fall from a morning high is news even on a day that is still only down a
// little from the close.
func TestMarketNoticesCatchASwing(t *testing.T) {
	open := at(t, "2026-09-28 09:30")
	now := at(t, "2026-09-28 11:30")
	q := sessionQuote("^GSPC", 100, []float64{100.2, 100.8, 100.1, 99.5, 99.2}, open)

	m := buildMarket(map[string]Quote{"^GSPC": q}, now)
	if len(m.notices) != 1 || !strings.Contains(m.notices[0].ID, ":swing:down:") {
		t.Fatalf("a 1.6%% slide in two hours gave %+v", m.notices)
	}
	if !strings.Contains(m.notices[0].Title, "in two hours") {
		t.Errorf("title = %q", m.notices[0].Title)
	}
}

func TestSwingLooksOnlyInsideTheWindow(t *testing.T) {
	base := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC).Unix()
	closes := []float64{120, 100, 100.5, 101}
	times := []int64{base - 3*3600, base - 3600, base - 1800, base}
	pct, ok := swing(closes, times, 2*time.Hour)
	if !ok || pct < 0.99 || pct > 1.01 {
		t.Errorf("swing = %.2f, %v, want +1%% off the low inside the window", pct, ok)
	}
}

// A warning is continued and extended under new ids through its life, and the
// VTEC event number is what stays the same.
func TestAlertKeyFollowsTheEventNotTheMessage(t *testing.T) {
	first := alertKey([]string{"/O.NEW.KRAH.SV.W.0123.260928T2000Z-260928T2045Z/"}, "urn:1")
	later := alertKey([]string{"/O.CON.KRAH.SV.W.0123.000000T0000Z-260928T2045Z/"}, "urn:2")
	if first != "KRAH.SV.W.0123" || first != later {
		t.Errorf("keys %q and %q, want both KRAH.SV.W.0123", first, later)
	}
	if got := alertKey(nil, "urn:3"); got != "urn:3" {
		t.Errorf("no VTEC gave %q, want the id", got)
	}
}

func TestOnlySevereAlertsNotify(t *testing.T) {
	alerts := []Alert{
		{Event: "TORNADO WARNING", Severity: "extreme", key: "t", name: "Tornado Warning", Area: "ALEXANDER", Until: "MON 8:30PM"},
		{Event: "WINTER STORM WATCH", Severity: "moderate", key: "w", name: "Winter Storm Watch"},
		{Event: "HEAT ADVISORY", Severity: "moderate", key: "h", name: "Heat Advisory"},
		{Event: "SPECIAL WEATHER STATEMENT", Severity: "moderate", key: "s", name: "Special Weather Statement"},
		{Event: "SEVERE THUNDERSTORM WARNING", Severity: "severe", key: "c", name: "Severe Thunderstorm Warning", cancel: true},
	}
	got := strings.Join(noticeIDs(alertNotices(alerts)), ",")
	if got != "weather:t,weather:w" {
		t.Errorf("notified %s, want the warning and the watch", got)
	}
	if n := alertNotices(alerts[:1])[0]; n.Title != "Tornado Warning" || n.Body != "ALEXANDER, until MON 8:30PM" {
		t.Errorf("notice = %+v", n)
	}
}

func TestEarningsNoticeIDsUseTheDate(t *testing.T) {
	row := Earning{Symbol: "NVDA", Name: "NVIDIA", Day: "TODAY", Verdict: "BEAT", Actual: "$1.64", Forecast: "$1.60", date: "2026-09-28"}
	next := row
	next.Day = "YESTERDAY"

	a, b := earningsNotices([]Earning{row}), earningsNotices([]Earning{next})
	if len(a) != 1 || a[0].ID != b[0].ID || a[0].ID != "earnings:NVDA:2026-09-28" {
		t.Fatalf("ids %v and %v", noticeIDs(a), noticeIDs(b))
	}
	if a[0].Title != "NVDA beat" || a[0].Body != "NVIDIA, $1.64 a share against $1.60 expected" {
		t.Errorf("notice = %+v", a[0])
	}
	if got := earningsNotices([]Earning{{Symbol: "COST", date: "2026-09-28"}}); len(got) != 0 {
		t.Errorf("an unreported row notified: %+v", got)
	}
}

func TestNewReportsComparesByNameAndDate(t *testing.T) {
	known := []Earning{{Symbol: "ORCL", date: "2026-09-10"}}
	if newReports([]Earning{{Symbol: "ORCL", date: "2026-09-10"}}, known) {
		t.Error("a print the full poll already had counted as new")
	}
	if !newReports([]Earning{{Symbol: "ORCL", date: "2026-09-10"}, {Symbol: "ADBE", date: "2026-09-10"}}, known) {
		t.Error("a new print was missed")
	}
}

// Monday's weekday before is Friday, since an evening report on Friday is still
// arriving on Monday morning.
func TestRecentReportsReachBackOverTheWeekend(t *testing.T) {
	monday := time.Date(2026, 9, 28, 7, 0, 0, 0, easternTime())
	var asked []string
	day := func(d time.Time) ([]Earning, bool) {
		asked = append(asked, d.Format("Mon"))
		return []Earning{{Symbol: "X", Verdict: "BEAT"}, {Symbol: "Y"}}, true
	}
	got := recentReports(day, monday)
	if strings.Join(asked, ",") != "Mon,Fri" || len(got) != 2 {
		t.Errorf("asked %v and kept %d rows", asked, len(got))
	}
}

func TestReportingHours(t *testing.T) {
	for when, want := range map[string]bool{
		"2026-09-28 05:59": false,
		"2026-09-28 06:00": true,
		"2026-09-28 16:30": true,
		"2026-09-28 20:00": false,
		"2026-09-27 12:00": false,
	} {
		if got := reportingHours(at(t, when)); got != want {
			t.Errorf("%s: %v, want %v", when, got, want)
		}
	}
}
